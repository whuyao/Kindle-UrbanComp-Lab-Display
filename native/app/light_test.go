package main

import (
	"context"
	"errors"
	"strings"
	syncatomic "sync/atomic"
	"testing"
	"time"
)

func TestIdleLightDimsAndRestoresOnTouch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var marks syncatomic.Int32
	levels := make(chan int, 8)
	l := startIdleLight(ctx, 18, 25*time.Millisecond, func() error { marks.Add(1); return nil }, func(_ context.Context, level int) error {
		if marks.Load() == 0 {
			t.Error("brightness changed without restoration marker")
		}
		levels <- level
		return nil
	})
	defer l.close()
	read := func(want int) {
		t.Helper()
		select {
		case n := <-levels:
			if n != want {
				t.Fatal(n, want)
			}
		case <-time.After(time.Second):
			t.Fatal("light action missing")
		}
	}
	read(0)
	l.touch()
	read(18)
	read(0)
	l.touch()
	read(18)
	if marks.Load() != 1 {
		t.Fatal("original brightness overwritten")
	}
	l.close()
	select {
	case n := <-levels:
		t.Fatal("changed light after shutdown", n)
	case <-time.After(40 * time.Millisecond):
	}
}

func TestIdleLightDoesNotChangeWithoutRecoveryState(t *testing.T) {
	marked := make(chan struct{}, 1)
	var changed syncatomic.Bool
	l := startIdleLight(context.Background(), 18, 10*time.Millisecond, func() error {
		select {
		case marked <- struct{}{}:
		default:
		}
		return errors.New("read-only state")
	}, func(context.Context, int) error { changed.Store(true); return nil })
	select {
	case <-marked:
	case <-time.After(time.Second):
		t.Fatal("marker not attempted")
	}
	l.close()
	if changed.Load() {
		t.Fatal("unsafe light change")
	}
}

func TestIdleLightRestoresAfterUncertainDimFailure(t *testing.T) {
	levels := make(chan int, 4)
	l := startIdleLight(context.Background(), 12, 10*time.Millisecond, func() error { return nil }, func(_ context.Context, n int) error {
		levels <- n
		if n == 0 {
			return errors.New("command outcome unknown")
		}
		return nil
	})
	defer l.close()
	select {
	case <-levels:
	case <-time.After(time.Second):
		t.Fatal("no dim attempt")
	}
	l.touch()
	deadline := time.After(time.Second)
	for {
		select {
		case n := <-levels:
			if n == 12 {
				return
			}
		case <-deadline:
			t.Fatal("touch did not restore light")
		}
	}
}

func TestCPUTicksExcludeGuestAndAcceptSpacesInComm(t *testing.T) {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[11] = "11"
	fields[12] = "7"
	own, total := cpuTicks("123 (ink native) "+strings.Join(fields, " "), "cpu 10 20 30 40 50 60 70 80 90 100\ncpu0 1 2 3")
	if own != 18 || total != 360 {
		t.Fatal(own, total)
	}
}
