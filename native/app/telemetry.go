package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const appVersion = "0.5.4"

type metric struct {
	Schema       int      `json:"schema"`
	Version      string   `json:"version"`
	Session      string   `json:"session"`
	Device       bool     `json:"device"`
	Kind         string   `json:"kind"`
	At           string   `json:"at"`
	Elapsed      float64  `json:"elapsed_seconds"`
	StartElapsed *float64 `json:"start_elapsed_seconds,omitempty"`
	Battery      *int     `json:"battery_percent,omitempty"`
	Charging     *bool    `json:"charging,omitempty"`
	USB          *bool    `json:"usb_power,omitempty"`
	PowerFormat  string   `json:"power_info_format,omitempty"`
	PowerError   string   `json:"power_info_error,omitempty"`
	Frontlight   *int     `json:"frontlight_level,omitempty"`
	ProcessTicks uint64   `json:"process_cpu_ticks,omitempty"`
	SystemTicks  uint64   `json:"system_cpu_ticks,omitempty"`
	Goroutines   int      `json:"goroutines,omitempty"`
	GCCycles     uint32   `json:"gc_cycles,omitempty"`
	GCPauseNS    uint64   `json:"gc_pause_ns,omitempty"`
	RSS          int      `json:"rss_kib,omitempty"`
	HWM          int      `json:"hwm_kib,omitempty"`
	Heap         uint64   `json:"heap_bytes,omitempty"`
	UpdateOK     *bool    `json:"update_ok,omitempty"`
	ResearchOK   *bool    `json:"research_ok,omitempty"`
	WeatherOK    *bool    `json:"weather_ok,omitempty"`
	InterestsOK  *bool    `json:"interests_ok,omitempty"`
	Outcome      string   `json:"outcome,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type telemetry struct {
	dir, session string
	started      time.Time
	device       bool
	mu           sync.Mutex
}

func newTelemetry(dir string, realSources bool) *telemetry {
	now := time.Now()
	return &telemetry{dir: dir, session: fmt.Sprintf("%s-%d", now.UTC().Format("20060102T150405.000000000"), os.Getpid()), started: now, device: runtime.GOOS == "linux" && realSources}
}

func (t *telemetry) emit(m metric) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !mounted() {
		return
	}
	m.Schema, m.Version, m.Session, m.Device = 1, appVersion, t.session, t.device
	m.At = time.Now().UTC().Format(time.RFC3339Nano)
	m.Elapsed = time.Since(t.started).Seconds()
	if len(m.Error) > 400 {
		m.Error = m.Error[:400]
	}
	b, e := json.Marshal(m)
	if e != nil {
		return
	}
	b = append(b, '\n')
	path := filepath.Join(t.dir, "state", "metrics.jsonl")
	if st, e := os.Stat(path); e == nil && st.Size() > 1024*1024 {
		if e = os.Rename(path, path+".previous"); e != nil {
			log.Printf("METRICS_FAILED: %v", e)
			return
		}
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if e != nil {
		log.Printf("METRICS_FAILED: %v", e)
		return
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if m.Kind == "sample" && m.Battery != nil && mounted() {
		if err := atomic(filepath.Join(t.dir, "state", "power.json"), b); err != nil {
			log.Printf("POWER_STATUS_FAILED: %v", err)
		}
	}
	if e != nil {
		log.Printf("METRICS_FAILED: %v", e)
	}
}

func prop(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	b, e := exec.CommandContext(ctx, "/usr/bin/lipc-get-prop", "com.lab126.powerd", name).Output()
	return strings.TrimSpace(string(b)), e
}

func sampleBattery(m *metric, read func(string) (string, error)) {
	if value, e := read("flIntensity"); e == nil {
		if n, e := strconv.Atoi(value); e == nil && n >= 0 && n <= 25 {
			m.Frontlight = &n
		}
	}
	if value, e := read("battLevel"); e == nil {
		if n, e := strconv.Atoi(value); e == nil && n >= 0 && n <= 100 {
			m.Battery = &n
		}
	}
	if value, e := read("isCharging"); e == nil && (value == "0" || value == "1") {
		b := value == "1"
		m.Charging = &b
	}
	// isCharging can be false on external power at 100%. The device's reported
	// USB power flag is also required before estimating discharge consumption.
	if value, e := read("battStateInfo"); e == nil {
		m.USB, m.PowerFormat = parsePowerInfo(value)
		if m.USB == nil {
			m.PowerError = "battStateInfo has no valid USB power field"
		}
	} else {
		m.PowerError = "battStateInfo read failed: " + e.Error()
	}
}

func parsePowerInfo(value string) (*bool, string) {
	// lipc's property listing wraps displayed values in square brackets. A
	// direct string-property read returns the JSON object inside those brackets.
	type powerInfo struct {
		USB *bool `json:"usb"`
	}
	var single powerInfo
	if json.Unmarshal([]byte(value), &single) == nil && single.USB != nil {
		return single.USB, "object"
	}
	var list []powerInfo
	if json.Unmarshal([]byte(value), &list) == nil && len(list) == 1 && list[0].USB != nil {
		return list[0].USB, "array"
	}
	return nil, ""
}

func procMemory(text string) (rss, hwm int) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, e := strconv.Atoi(f[1])
		if e != nil || n < 0 {
			continue
		}
		switch f[0] {
		case "VmRSS:":
			rss = n
		case "VmHWM:":
			hwm = n
		}
	}
	return
}

func (t *telemetry) sample(ctx context.Context) {
	m := metric{Kind: "sample"}
	if runtime.GOOS == "linux" {
		sampleBattery(&m, func(name string) (string, error) { return prop(ctx, name) })
		if b, e := os.ReadFile("/proc/self/status"); e == nil {
			m.RSS, m.HWM = procMemory(string(b))
		}
		process, _ := os.ReadFile("/proc/self/stat")
		system, _ := os.ReadFile("/proc/stat")
		m.ProcessTicks, m.SystemTicks = cpuTicks(string(process), string(system))
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	m.Heap = memory.HeapAlloc
	m.Goroutines, m.GCCycles, m.GCPauseNS = runtime.NumGoroutine(), memory.NumGC, memory.PauseTotalNs
	if ctx.Err() == nil {
		t.emit(m)
	}
}

func cpuTicks(process, system string) (own, total uint64) {
	if end := strings.LastIndex(process, ")"); end >= 0 {
		fields := strings.Fields(process[end+1:])
		if len(fields) > 12 {
			a, _ := strconv.ParseUint(fields[11], 10, 64)
			b, _ := strconv.ParseUint(fields[12], 10, 64)
			own = a + b
		}
	}
	for _, line := range strings.Split(system, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 9 && fields[0] == "cpu" {
			for _, v := range fields[1:9] {
				n, _ := strconv.ParseUint(v, 10, 64)
				total += n
			}
			break
		}
	}
	return
}

func (t *telemetry) run(ctx context.Context) {
	t.sample(ctx)
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.sample(ctx)
		}
	}
}

func (t *telemetry) refresh(ctx context.Context, start time.Time, err error) {
	from := start.Sub(t.started).Seconds()
	m := metric{Kind: "refresh", StartElapsed: &from, Outcome: "completed"}
	if ctx.Err() != nil {
		m.Outcome = "cancelled"
		t.emit(m)
		return
	}
	ok := false
	m.UpdateOK = &ok
	var receipt struct {
		Attempt   string `json:"attempt_started_at"`
		Stale     bool   `json:"research_stale"`
		Weather   bool   `json:"weather_ok"`
		Interests bool   `json:"interests_ok"`
	}
	if b, e := os.ReadFile(filepath.Join(t.dir, "state", "receipt.json")); e == nil && json.Unmarshal(b, &receipt) == nil {
		if at, e := time.Parse(time.RFC3339Nano, receipt.Attempt); e == nil && !at.Before(start) {
			research := !receipt.Stale
			m.ResearchOK = &research
			m.WeatherOK = &receipt.Weather
			m.InterestsOK = &receipt.Interests
			ok = err == nil && research && receipt.Weather && receipt.Interests
		}
	}
	if err != nil {
		m.Error = err.Error()
	} else if !ok {
		var failed []string
		if m.ResearchOK != nil && !*m.ResearchOK {
			failed = append(failed, "research")
		}
		if m.WeatherOK != nil && !*m.WeatherOK {
			failed = append(failed, "weather")
		}
		if m.InterestsOK != nil && !*m.InterestsOK {
			failed = append(failed, "interests")
		}
		if len(failed) > 0 {
			m.Error = "sources incomplete: " + strings.Join(failed, ", ")
		} else {
			m.Error = "source or frame validation incomplete"
		}
	}
	t.emit(m)
}
