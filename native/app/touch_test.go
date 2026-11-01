package main

import (
	"testing"
	"time"
)

func TestHomeTap(t *testing.T) {
	for _, tc := range []struct {
		name string
		x, y int32
		want bool
	}{
		{"home", 690, 972, true}, {"article", 300, 600, false}, {"weather", 300, 970, false}, {"credit", 680, 1010, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var g homeGesture
			now := time.Now()
			for _, e := range [][3]int32{{3, 0x39, 1}, {3, 0x35, tc.x}, {3, 0x36, tc.y}, {0, 0, 0}} {
				if g.event(uint16(e[0]), uint16(e[1]), e[2], now) {
					t.Fatal("fired before release")
				}
			}
			g.event(3, 0x39, -1, now.Add(100*time.Millisecond))
			if hit := g.event(0, 0, 0, now.Add(100*time.Millisecond)) && (Tap{Start: g.start, End: g.start}).in(homeTarget); hit != tc.want {
				t.Fatal(hit)
			}
			if g.event(0, 0, 0, now) {
				t.Fatal("duplicate tap")
			}
		})
	}
}

func TestHomeRejectsDragDropAndMultitouch(t *testing.T) {
	for _, mode := range []string{"drag", "dropped", "multi", "other-first", "long"} {
		t.Run(mode, func(t *testing.T) {
			var g homeGesture
			now := time.Now()
			if mode == "other-first" {
				g.event(3, 0x2f, 1, now)
				g.event(3, 0x39, 8, now)
				g.event(3, 0x2f, 0, now)
			}
			for _, e := range [][3]int32{{3, 0x39, 1}, {3, 0x35, 690}, {3, 0x36, 972}, {0, 0, 0}} {
				g.event(uint16(e[0]), uint16(e[1]), e[2], now)
			}
			switch mode {
			case "drag":
				g.event(3, 0x35, 730, now)
				g.event(0, 0, 0, now)
				g.event(3, 0x35, 690, now)
			case "dropped":
				g.event(0, 3, 0, now)
			case "multi":
				g.event(3, 0x2f, 1, now)
				g.event(3, 0x39, 2, now)
				g.event(3, 0x2f, 0, now)
			case "long":
				now = now.Add(3 * time.Second)
			}
			g.event(3, 0x39, -1, now)
			if g.event(0, 0, 0, now) {
				t.Fatal("unexpected home exit")
			}
		})
	}
}

func TestTouchScale(t *testing.T) {
	if v, ok := touchPixel(4095, 0, 4095, 758); !ok || v != 757 {
		t.Fatal(v, ok)
	}
	if _, ok := touchPixel(1, 0, 0, 758); ok {
		t.Fatal("invalid range")
	}
	if _, ok := touchPixel(-1, 0, 4095, 758); ok {
		t.Fatal("outside range")
	}
}
