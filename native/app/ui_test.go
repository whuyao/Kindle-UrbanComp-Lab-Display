package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUIStartupWithoutCacheFetchesAndCanExit(t *testing.T) {
	dir := t.TempDir()
	fonts, _ := filepath.Abs("../package/urbancomp-ink/fonts")
	if err := os.Symlink(fonts, filepath.Join(dir, "fonts")); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	painted := make(chan bool, 1)
	fetching := make(chan bool, 1)
	home := make(chan func(), 1)
	done := make(chan error, 1)
	go func() {
		done <- runUIWith(ctx, cancel, dir, "mock", uiServices{
			touch: func(_ context.Context, stop func(), _ chan<- Tap) error { home <- stop; return nil },
			display: func(_ context.Context, path, _ string) error {
				b, err := os.ReadFile(path)
				painted <- len(b) > 0
				return err
			},
			fetch: func(ctx context.Context) refreshResult {
				fetching <- true
				<-ctx.Done()
				return refreshResult{Err: ctx.Err()}
			},
		})
	}()
	for _, ready := range []<-chan bool{painted, fetching} {
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("no startup frame")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("startup blocked")
		}
	}
	(<-home)()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("home exit blocked by fetch")
	}
}

func TestUIReadsDuringBlockedFetchAndDefersRefresh(t *testing.T) {
	dir := t.TempDir()
	fonts, _ := filepath.Abs("../package/urbancomp-ink/fonts")
	if e := os.Symlink(fonts, filepath.Join(dir, "fonts")); e != nil {
		t.Fatal(e)
	}
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	m, _ := adapt(fixture(t))
	m.Focus.Summary = strings.Repeat("这是旧版简报，阅读时应保持原文与页码。", 80)
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "state", "content.json"), b, 0600)
	os.WriteFile(filepath.Join(dir, "state", "receipt.json"), []byte(`{}`), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type controls struct {
		taps chan<- Tap
		home func()
	}
	input := make(chan controls, 1)
	painted := make(chan []byte, 8)
	requested := make(chan chan refreshResult, 2)
	ticks := make(chan time.Time, 1)
	done := make(chan error, 1)
	go func() {
		done <- runUIWith(ctx, cancel, dir, "mock", uiServices{
			touch: func(_ context.Context, home func(), taps chan<- Tap) error { input <- controls{taps, home}; return nil },
			display: func(_ context.Context, path, driver string) error {
				b, e := os.ReadFile(path)
				if e == nil {
					painted <- b
				}
				return e
			},
			fetch: func(ctx context.Context) refreshResult {
				response := make(chan refreshResult, 1)
				requested <- response
				select {
				case r := <-response:
					return r
				case <-ctx.Done():
					return refreshResult{Err: ctx.Err()}
				}
			}, ticks: ticks,
		})
	}()
	var control controls
	select {
	case control = <-input:
	case <-time.After(3 * time.Second):
		t.Fatal("input not initialized")
	}
	frame := func() []byte {
		t.Helper()
		select {
		case b := <-painted:
			return b
		case <-time.After(3 * time.Second):
			t.Fatal("screen did not respond")
			return nil
		}
	}
	tap := func(x, y int) { p := tapped(x, y); p.At = time.Now().Add(time.Second); control.taps <- p }
	initial := frame()
	var response chan refreshResult
	select {
	case response = <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("fetch not started")
	}
	tap(200, 320)
	first := frame()
	if bytes.Equal(first, initial) {
		t.Fatal("title did not open while fetch blocked")
	}
	tap(300, 970)
	second := frame()
	if bytes.Equal(first, second) {
		t.Fatal("page turn did not work during fetch")
	}
	progressDeadline := time.Now().Add(3 * time.Second)
	for {
		memory := loadReading(dir)
		if memory.page(readingDocument(m, "focus", 0)) == 1 {
			break
		}
		if time.Now().After(progressDeadline) {
			t.Fatal("displayed page was not persisted")
		}
		time.Sleep(time.Millisecond)
	}
	newModel := m
	newModel.Focus.Title = "新版简报"
	newModel.Focus.Summary = "新的资料"
	newFramePath := filepath.Join(dir, "updated.png")
	if e := render(newModel, dir, newFramePath); e != nil {
		t.Fatal(e)
	}
	newFrame, _ := os.ReadFile(newFramePath)
	response <- refreshResult{Model: newModel, Frame: newFrame}
	deadline := time.Now().Add(3 * time.Second)
	for {
		b, _ := os.ReadFile(filepath.Join(dir, "state", "receipt.json"))
		var receipt map[string]any
		json.Unmarshal(b, &receipt)
		if receipt["reading_open"] == true {
			if receipt["painted_this_cycle"] != false {
				t.Fatal("refresh claimed a screen write while reading")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh result not processed")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case <-painted:
		t.Fatal("refresh replaced an open reading page")
	default:
	}
	tap(100, 970)
	if !bytes.Equal(frame(), first) {
		t.Fatal("refresh changed open document")
	}
	tap(100, 45)
	if !bytes.Equal(frame(), newFrame) {
		t.Fatal("return did not display latest dashboard")
	}
	// Home must cancel immediately even if the next scheduled fetch is blocked.
	ticks <- time.Now()
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduled fetch not started")
	}
	control.home()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("Home blocked on network")
	}
}
