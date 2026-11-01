package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

type refreshResult struct {
	Model Model
	Frame []byte
	Err   error
}

type uiServices struct {
	touch    func(context.Context, func(), chan<- Tap) error
	display  func(context.Context, string, string) error
	fetch    func(context.Context) refreshResult
	ticks    <-chan time.Time
	fault    func(error)
	activity func()
}

func runUI(ctx context.Context, stop func(), dir, fixture, driver string) error {
	reporter := newTelemetry(dir, fixture == "")
	light := newIdleLight(ctx, reporter)
	defer light.close()
	go reporter.run(ctx)
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	return runUIWith(ctx, stop, dir, driver, uiServices{
		touch: startTouch, display: paint, ticks: ticker.C,
		activity: light.touch,
		fault:    func(err error) { reporter.emit(metric{Kind: "display_failure", Error: err.Error()}) },
		fetch: func(ctx context.Context) refreshResult {
			start := time.Now()
			err := cycle(ctx, dir, fixture, driver, true)
			reporter.refresh(ctx, start, err)
			m, loadErr := load(dir)
			frame, frameErr := os.ReadFile(filepath.Join(dir, "state", "frame.png"))
			if loadErr != nil || frameErr != nil {
				frame = nil
			}
			debug.FreeOSMemory()
			return refreshResult{m, frame, err}
		},
	})
}

func runUIWith(ctx context.Context, stop func(), dir, driver string, svc uiServices) error {
	taps := make(chan Tap, 4)
	if e := svc.touch(ctx, stop, taps); e != nil {
		return e
	}
	tmp, e := os.CreateTemp("", "urbancomp-ink-reader-*.png")
	if e != nil {
		return e
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)
	session := readerSession{Memory: loadReading(dir)}
	var board []byte
	readyAt := time.Now()
	if m, err := load(dir); err == nil {
		if e = showCached(ctx, dir, driver, svc.display); e != nil {
			return e
		}
		board, e = os.ReadFile(filepath.Join(dir, "state", "frame.png"))
		if e != nil {
			return e
		}
		session.Model = m
		readyAt = time.Now()
		log.Print("CACHED_FRAME_DISPLAYED")
	} else {
		if e = renderStartup(dir, path); e != nil {
			return e
		}
		if e = svc.display(ctx, path, driver); e != nil {
			return e
		}
		log.Print("BRAND_STARTUP_DISPLAYED")
	}
	results := make(chan refreshResult, 1)
	busy := false
	refresh := func() {
		if busy {
			return
		}
		busy = true
		go func() {
			// Network and dashboard-cache generation never own the physical screen.
			// Only this event loop may paint it, preserving open reading pages.
			result := svc.fetch(ctx)
			select {
			case results <- result:
			case <-ctx.Done():
			}
		}()
	}
	refresh()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-svc.ticks:
			refresh()
		case r := <-results:
			busy = false
			if ctx.Err() != nil || !mounted() {
				return nil
			}
			if r.Err != nil {
				log.Printf("REFRESH_WARNING: %v", r.Err)
			}
			if len(r.Frame) == 0 {
				continue
			}
			painted := false
			if session.Doc == nil && !bytes.Equal(board, r.Frame) {
				if e = atomic(path, r.Frame); e == nil {
					e = svc.display(ctx, path, driver)
				}
				if e != nil {
					if svc.fault != nil {
						svc.fault(e)
					}
					log.Printf("DISPLAY_FAILED retained prior view: %v", e)
					continue
				}
				painted = true
				readyAt = time.Now()
			}
			board = r.Frame
			session.refresh(r.Model)
			recordDisplay(dir, painted, session.Doc != nil)
			log.Printf("UI_REFRESH displayed=%t reading=%t", painted, session.Doc != nil)
		case tap := <-taps:
			if svc.activity != nil {
				svc.activity()
			}
			if ctx.Err() != nil || !mounted() {
				return nil
			}
			// Repeated taps during e-ink rendering must not turn several pages.
			if tap.At.Before(readyAt) || len(board) == 0 {
				continue
			}
			candidate, changed := session.navigate(tap)
			if !changed {
				continue
			}
			if candidate.Doc == nil {
				e = render(candidate.Model, dir, path)
			} else {
				candidate.Pages, e = renderReading(*candidate.Doc, candidate.Page, dir, path)
				if candidate.Page >= candidate.Pages && candidate.Pages > 0 {
					candidate.Page = 0
					candidate.Pages, e = renderReading(*candidate.Doc, 0, dir, path)
				}
			}
			if e == nil {
				e = svc.display(ctx, path, driver)
			}
			readyAt = time.Now()
			if e != nil {
				if svc.fault != nil {
					svc.fault(e)
				}
				log.Printf("READING_FAILED retained prior view: %v", e)
				continue
			}
			session = candidate
			if session.Doc == nil {
				if b, err := os.ReadFile(path); err == nil {
					board = b
				}
				log.Print("READER_BACK")
			} else {
				session.Memory.remember(*session.Doc, session.Page)
				if mounted() {
					if err := session.Memory.save(dir); err != nil {
						log.Printf("READING_SAVE_FAILED: %v", err)
					}
				}
				log.Printf("READER_PAGE section=%s page=%d/%d", session.Doc.Label, session.Page+1, session.Pages)
			}
		}
	}
}

func recordDisplay(dir string, painted, reading bool) {
	path := filepath.Join(dir, "state", "receipt.json")
	b, e := os.ReadFile(path)
	var r map[string]any
	if e != nil || json.Unmarshal(b, &r) != nil || r == nil {
		return
	}
	r["display_enabled"] = true
	r["painted_this_cycle"] = painted
	r["reading_open"] = reading
	if painted {
		r["last_dashboard_display_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	b, e = json.MarshalIndent(r, "", "  ")
	if e == nil && mounted() {
		if e = atomic(path, b); e != nil {
			log.Printf("RECEIPT_FAILED: %v", e)
		}
	}
}
