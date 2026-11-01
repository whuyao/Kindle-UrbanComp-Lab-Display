package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"
)

func mounted() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	// The gadget "connected" attribute can stay at 1 after USB eject/unplug.
	// The supervisor checks volumd; this independent guard checks the mount.
	b, e := os.ReadFile("/proc/mounts")
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) > 3 && f[1] == "/mnt/us" {
			return strings.Contains(","+f[3]+",", ",rw,")
		}
	}
	return false
}
func atomic(path string, b []byte) error {
	if e := os.WriteFile(path+".tmp", b, 0600); e != nil {
		return e
	}
	f, e := os.OpenFile(path+".tmp", os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	e = f.Sync()
	f.Close()
	if e != nil {
		return e
	}
	return os.Rename(path+".tmp", path)
}
func load(dir string) (Model, error) {
	var m Model
	b, e := os.ReadFile(filepath.Join(dir, "state", "content.json"))
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(b, &m)
	if len(m.Papers) == 0 {
		return m, errors.New("empty cached content")
	}
	return m, e
}
func paint(parent context.Context, path, driver string) error {
	if runtime.GOOS != "linux" {
		return errors.New("display requires Linux device")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch driver {
	case "fbink":
		cmd = exec.CommandContext(ctx, "/var/local/kmc/bin/fbink", "-q", "-g", "file="+path, "-W", "GC16", "-f")
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH=/var/local/kmc/lib")
	case "eips":
		cmd = exec.CommandContext(ctx, "/usr/bin/eips", "-g", path)
	default:
		return errors.New("unvalidated display driver")
	}
	b, e := cmd.CombinedOutput()
	if e != nil {
		return fmt.Errorf("%s: %w %s", driver, e, string(b))
	}
	return nil
}

func showCached(ctx context.Context, dir, driver string, display func(context.Context, string, string) error) error {
	m, e := load(dir)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp("", "urbancomp-ink-start-*.png")
	if e != nil {
		return e
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	// Apply layout and controls immediately, without waiting for the network.
	if e = render(m, dir, path); e != nil {
		return e
	}
	if e = display(ctx, path, driver); e != nil {
		return e
	}
	if !mounted() || ctx.Err() != nil {
		return errors.New("storage unavailable after cached display")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if e = atomic(filepath.Join(dir, "state", "frame.png"), b); e != nil {
		return e
	}
	receiptPath := filepath.Join(dir, "state", "receipt.json")
	var receipt map[string]any
	if prior, err := os.ReadFile(receiptPath); err == nil && json.Unmarshal(prior, &receipt) == nil && receipt != nil {
		hash := sha256.Sum256(b)
		receipt["sha256"] = hex.EncodeToString(hash[:])
		receipt["layout_redrawn_at"] = time.Now().UTC().Format(time.RFC3339)
		encoded, _ := json.MarshalIndent(receipt, "", "  ")
		if e = atomic(receiptPath, encoded); e != nil {
			return e
		}
	}
	return nil
}

func cycle(ctx context.Context, dir, fixture, driver string, noDisplay bool) error {
	attemptStarted := time.Now().UTC().Format(time.RFC3339Nano)
	if !mounted() {
		return errors.New("user storage unavailable; refusing writes")
	}
	state := filepath.Join(dir, "state")
	if e := os.MkdirAll(state, 0700); e != nil {
		return e
	}
	old, oldErr := load(dir)
	var m Model
	var data []byte
	var e, errorFetch error
	var network *http.Client
	var networkErr error
	if fixture == "" {
		network, networkErr = client(filepath.Join(dir, "cacert.pem"))
		if network != nil {
			defer network.CloseIdleConnections()
		}
	}
	if fixture != "" {
		data, e = os.ReadFile(fixture)
	} else {
		e = networkErr
		if e == nil {
			data, e = researchWithInterests(func(u string) ([]byte, error) {
				if u == researchURL || u == versionURL {
					return get(ctx, network, u)
				}
				limited, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()
				return get(limited, network, u)
			})
		}
	}
	if e == nil {
		m, e = adapt(data)
	}
	if e != nil {
		errorFetch = e
		log.Printf("RESEARCH_FAILED %v", e)
		if oldErr != nil {
			return e
		}
		m = old
		m.ResearchStale = true
		m.SelectionStale = true
	} else {
		log.Printf("RESEARCH_OK papers=%d source_updated=%s", len(m.Papers), m.Updated)
	}
	if fixture == "" {
		err := networkErr
		if err == nil {
			var b []byte
			b, err = get(ctx, network, weatherURL)
			if err == nil {
				m.Weather, err = parseWeather(b, time.Now())
			}
		}
		if err != nil {
			m.Weather = old.Weather
			if m.Weather != nil {
				m.Weather.Stale = true
			}
			log.Printf("WEATHER_FAILED %v", err)
		} else {
			log.Printf("WEATHER_OK %s %dC", m.Weather.Condition, m.Weather.Temp)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !mounted() {
		return errors.New("storage disconnected during fetch")
	}
	m = curate(m, old, time.Now())
	tmp, e := os.CreateTemp("", "urbancomp-ink-frame-*.png")
	if e != nil {
		return e
	}
	candidate := tmp.Name()
	tmp.Close()
	defer os.Remove(candidate)
	if e = render(m, dir, candidate); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !mounted() {
		return errors.New("storage disconnected during render")
	}
	b, e := os.ReadFile(candidate)
	if e != nil {
		return e
	}
	hash := sha256.Sum256(b)
	current := filepath.Join(state, "frame.png")
	same := false
	if prior, err := os.ReadFile(current); err == nil {
		same = sha256.Sum256(prior) == hash
	}
	if !noDisplay && !same {
		if e = paint(ctx, candidate, driver); e != nil {
			return e
		}
	}
	if !same {
		if !mounted() || ctx.Err() != nil {
			return errors.New("storage or context unavailable before commit")
		}
		if e = atomic(current, b); e != nil {
			return e
		}
	}
	content, _ := json.Marshal(m)
	if e = atomic(filepath.Join(state, "content.json"), content); e != nil {
		return e
	}
	receipt := map[string]any{"checked_at": time.Now().UTC().Format(time.RFC3339), "sha256": hex.EncodeToString(hash[:]), "changed": !same, "display_enabled": !noDisplay, "painted_this_cycle": !noDisplay && !same, "driver": driver, "source": researchURL, "independent_of_mac": fixture == "", "interval_seconds": 1800, "research_stale": m.ResearchStale}
	receipt["weather_ok"] = m.Weather != nil && !m.Weather.Stale
	receipt["interests_ok"] = !m.SelectionStale
	receipt["attempt_started_at"] = attemptStarted
	receipt["featured"] = m.Rotation.Featured
	receipt["paper_count"] = len(m.Papers)
	receiptData, _ := json.MarshalIndent(receipt, "", "  ")
	if e = atomic(filepath.Join(state, "receipt.json"), receiptData); e != nil {
		return e
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	log.Printf("FRAME_OK changed=%t display=%t heap_bytes=%d", !same, !noDisplay, stats.HeapAlloc)
	if b, e = os.ReadFile("/proc/self/status"); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "VmHWM:") {
				log.Print(line)
			}
		}
	}
	return errorFetch
}

func main() {
	dir := flag.String("dir", "/mnt/us/urbancomp-ink", "data directory")
	once := flag.Bool("once", false, "one fetch/render cycle")
	fixture := flag.String("fixture", "", "offline research snapshot for tests")
	noDisplay := flag.Bool("no-display", false, "render only, no screen writes")
	driver := flag.String("driver", "", "fbink or eips, only after device test")
	preview := flag.String("preview", "", "render cached content to this PNG and exit; no network or display")
	previewDetail := flag.String("preview-detail", "", "offline detail: focus, radar:0, paper:0, papers, weather, startup")
	previewPage := flag.Int("preview-page", 1, "one-based detail preview page")
	flag.Parse()
	debug.SetMemoryLimit(64 << 20)
	debug.SetGCPercent(35)
	log.Printf("UrbanComp Ink native %s: %s %s/%s", appVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if *preview != "" {
		if *previewDetail == "startup" {
			if e := renderStartup(*dir, *preview); e != nil {
				log.Fatal(e)
			}
			return
		}
		m, e := load(*dir)
		if e != nil {
			log.Fatal(e)
		}
		if *previewDetail != "" {
			var count int
			count, e = renderReading(previewDocument(m, *previewDetail), *previewPage-1, *dir, *preview)
			log.Printf("PREVIEW_PAGE %d/%d", *previewPage, count)
		} else {
			e = render(m, *dir, *preview)
		}
		if e != nil {
			log.Fatal(e)
		}
		return
	}
	if !*noDisplay && *driver == "" {
		log.Fatal("select a verified driver")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		// Writes retain their mount checks; Upstart also stops on USB takeover.
		// This idle fallback does not need to parse /proc/mounts twice a second.
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !mounted() {
					log.Print("USB/storage guard: stopping")
					stop()
					return
				}
				if _, e := os.Stat(filepath.Join(*dir, "DISABLED")); e == nil {
					stop()
					return
				}
			}
		}
	}()
	if *once {
		if e := cycle(ctx, *dir, *fixture, *driver, *noDisplay); e != nil {
			log.Fatal(e)
		}
		return
	}
	if !mounted() {
		log.Fatal("storage unavailable")
	}
	if !*noDisplay {
		if e := runUI(ctx, stop, *dir, *fixture, *driver); e != nil {
			log.Fatal(e)
		}
		return
	}
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		if _, e := os.Stat(filepath.Join(*dir, "DISABLED")); e == nil {
			return
		}
		if !mounted() {
			log.Print("USB or unavailable storage; exiting safely")
			return
		}
		if e := cycle(ctx, *dir, *fixture, *driver, *noDisplay); e != nil {
			log.Printf("Cycle kept previous frame: %v", e)
		}
		debug.FreeOSMemory()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
