package main

import (
	"image"
	"time"
)

var homeTarget = image.Rect(640, 947, 739, 992)

type Tap struct {
	Start, End image.Point
	At         time.Time
}

func (t Tap) in(r image.Rectangle) bool { return t.Start.In(r) && t.End.In(r) }

// Touch events are consumed as complete evdev frames. Only a single-finger
// press and release inside the control counts; swipes and dropped frames do not.
type homeGesture struct {
	slot                                              int32
	x, y                                              int
	hasX, hasY, down, placed, releasing, multi, moved bool
	start                                             image.Point
	since                                             time.Time
	others                                            map[int32]bool
}

func (g *homeGesture) event(kind, code uint16, value int32, now time.Time) bool {
	if kind == 0 && code == 3 { // SYN_DROPPED: discard an incomplete gesture.
		*g = homeGesture{}
		return false
	}
	if kind == 3 && code == 0x2f {
		g.slot = value
		return false
	}
	if kind == 3 && code == 0x39 && g.slot != 0 {
		if g.others == nil {
			g.others = map[int32]bool{}
		}
		if value >= 0 {
			g.others[g.slot] = true
			if g.down {
				g.multi = true
			}
		} else {
			delete(g.others, g.slot)
		}
		return false
	}
	begin := func() {
		if !g.down {
			g.down, g.placed, g.releasing, g.multi, g.moved = true, false, false, len(g.others) > 0, false
		}
	}
	if kind == 1 && code == 330 {
		if value == 1 {
			begin()
		} else if value == 0 {
			g.releasing = g.down
		}
	}
	if kind == 3 && g.slot == 0 {
		switch code {
		case 0x39:
			if value >= 0 {
				begin()
			} else {
				g.releasing = g.down
			}
		case 0, 0x35:
			g.x, g.hasX = int(value), true
		case 1, 0x36:
			g.y, g.hasY = int(value), true
		}
	}
	if kind != 0 || code != 0 || !g.down {
		return false
	}
	point := image.Pt(g.x, g.y)
	if g.releasing {
		delta := point.Sub(g.start)
		hit := g.placed && !g.multi && !g.moved && delta.X*delta.X+delta.Y*delta.Y <= 24*24 && now.Sub(g.since) <= 2*time.Second
		g.down, g.placed, g.releasing = false, false, false
		return hit
	}
	if !g.hasX || !g.hasY {
		return false
	}
	if !g.placed {
		g.start, g.since, g.placed = point, now, true
	}
	dx, dy := point.X-g.start.X, point.Y-g.start.Y
	if dx*dx+dy*dy > 24*24 {
		g.moved = true
	}
	return false
}

func touchPixel(value, min, max int32, size int) (int32, bool) {
	if max <= min || value < min || value > max {
		return 0, false
	}
	return int32(int64(value-min) * int64(size-1) / int64(max-min)), true
}
