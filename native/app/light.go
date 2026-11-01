package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

type idleLight struct {
	activity chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
}

func setLight(ctx context.Context, level int) error {
	if level < 0 || level > 25 {
		return fmt.Errorf("invalid frontlight %d", level)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/lipc-set-prop", "com.lab126.powerd", "flIntensity", strconv.Itoa(level)).Run()
}

func newIdleLight(ctx context.Context, reporter *telemetry) *idleLight {
	if runtime.GOOS != "linux" {
		return nil
	}
	runtimeDir := "/tmp/urbancomp-ink-runtime"
	if _, e := os.Stat(filepath.Join(runtimeDir, "light-changed")); e == nil {
		log.Print("LIGHT_UNAVAILABLE: prior restoration pending")
		return nil
	}
	value, e := prop(ctx, "flIntensity")
	level, parseErr := strconv.Atoi(value)
	if e != nil || parseErr != nil || level < 0 || level > 25 {
		log.Printf("LIGHT_UNAVAILABLE: cannot read original level %q", value)
		return nil
	}
	log.Printf("LIGHT_ORIGINAL level=%d idle_seconds=120", level)
	if level == 0 {
		return nil
	}
	mark := func() error {
		if e := atomic(filepath.Join(runtimeDir, "light-before"), []byte(strconv.Itoa(level))); e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(runtimeDir, "light-changed"), []byte("owned\n"), 0600)
	}
	set := func(ctx context.Context, target int) error {
		e := setLight(ctx, target)
		m := metric{Kind: "frontlight", Frontlight: &target, Outcome: "set"}
		if e != nil {
			m.Outcome = "failed"
			m.Error = e.Error()
		}
		reporter.emit(m)
		log.Printf("LIGHT_SET level=%d error=%v", target, e)
		return e
	}
	return startIdleLight(ctx, level, 2*time.Minute, mark, set)
}

func startIdleLight(parent context.Context, original int, delay time.Duration, mark func() error, set func(context.Context, int) error) *idleLight {
	ctx, cancel := context.WithCancel(parent)
	l := &idleLight{activity: make(chan struct{}, 1), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(l.done)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		owned, dimmed := false, false
		reset := func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(delay)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-l.activity:
				if ctx.Err() != nil {
					return
				}
				if dimmed {
					if e := set(ctx, original); e == nil {
						dimmed = false
					}
				}
				reset()
			case <-timer.C:
				if ctx.Err() != nil {
					return
				}
				if !owned {
					if e := mark(); e != nil {
						log.Printf("LIGHT_MARK_FAILED: %v", e)
						reset()
						continue
					}
					owned = true
				}
				// A failed command can have an uncertain outcome. A later touch
				// still attempts to restore the original level in that case.
				dimmed = true
				if e := set(ctx, 0); e != nil {
					reset()
				}
			}
		}
	}()
	return l
}

func (l *idleLight) touch() {
	if l == nil {
		return
	}
	select {
	case l.activity <- struct{}{}:
	default:
	}
}
func (l *idleLight) close() {
	if l == nil {
		return
	}
	l.cancel()
	<-l.done
	// The supervisor restores the saved brightness after the app has stopped,
	// including crashes, USB takeover and a manual Home exit.
}
