package main

import (
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func tapped(x, y int) Tap { return Tap{image.Pt(x, y), image.Pt(x, y), time.Now()} }

func TestReaderNavigation(t *testing.T) {
	m, _ := adapt(fixture(t))
	for _, tc := range []struct {
		x, y        int
		label, text string
	}{
		{300, 320, "研究简报", m.Focus.Title}, {100, 245, "研究简报", m.Focus.Title},
		{180, 580, "研究雷达", m.Radars[0].Title}, {580, 580, "研究雷达", m.Radars[1].Title},
		{180, 510, "研究雷达", m.Radars[0].Title}, {300, 795, "值得精读", m.Papers[0].Title},
		{300, 870, "值得精读", m.Papers[1].Title}, {200, 750, "值得精读", m.Papers[0].Title},
		{200, 970, "光谷天气", "武汉 · 光谷"}, {600, 45, "关于知新", "research.urbancomp.dev"},
	} {
		s, changed := (readerSession{Model: m}).navigate(tapped(tc.x, tc.y))
		if !changed || s.Doc == nil || s.Doc.Label != tc.label {
			t.Fatalf("no target at %d,%d", tc.x, tc.y)
		}
		found := false
		for _, b := range s.Doc.Blocks {
			if strings.Contains(b.Text, tc.text) {
				found = true
			}
		}
		if !found {
			t.Fatal("opened wrong document", tc.text)
		}
		back, changed := s.navigate(tapped(100, 45))
		if !changed || back.Doc != nil {
			t.Fatal("back failed")
		}
	}
	s, _ := (readerSession{Model: m}).navigate(tapped(100, 300))
	s.Pages = 3
	if _, changed := s.navigate(tapped(100, 970)); changed {
		t.Fatal("previous enabled on first page")
	}
	s, _ = s.navigate(tapped(300, 970))
	s, _ = s.navigate(tapped(300, 970))
	if s.Page != 2 {
		t.Fatal("next failed")
	}
	if _, changed := s.navigate(tapped(300, 970)); changed {
		t.Fatal("next beyond end")
	}
	s, _ = s.navigate(tapped(100, 970))
	if s.Page != 1 {
		t.Fatal("previous failed")
	}
	if _, changed := s.navigate(tapped(690, 970)); changed {
		t.Fatal("home intercepted by reader")
	}
}

func TestReadingRetainsSnapshotOnRefresh(t *testing.T) {
	m, _ := adapt(fixture(t))
	s, _ := (readerSession{Model: m}).navigate(tapped(200, 320))
	s.Page, s.Pages = 2, 4
	oldDoc := s.Doc
	m.Focus.Title = "新的简报标题"
	m.Focus.Summary = "刷新后的内容"
	s.refresh(m)
	if s.Doc != oldDoc || s.Page != 2 || s.Pages != 4 || s.Doc.Blocks[0].Text == m.Focus.Title {
		t.Fatal("refresh displaced reading")
	}
	s, _ = s.navigate(tapped(100, 45))
	s, _ = s.navigate(tapped(200, 320))
	if s.Doc.Blocks[0].Text != m.Focus.Title {
		t.Fatal("return did not use latest model")
	}
}

func TestPaginationPreservesContent(t *testing.T) {
	d := document{Label: "长文测试"}
	d.title(strings.Repeat("城市空间智能（Geospatial Foundation Model）", 7))
	d.body(strings.Repeat("遥感、轨迹与城市交通：测试英文 Mixed-language (2026)，跨页完整保留。", 90))
	d.meta("https://example.org/" + strings.Repeat("long-source-url", 80))
	path := filepath.Join(t.TempDir(), "page.png")
	count := 0
	err := withPainter("../package/urbancomp-ink", path, func(p *Painter) error {
		pages := paginate(p, d)
		count = len(pages)
		var actual, expected strings.Builder
		for _, b := range d.Blocks {
			expected.WriteString(b.Text)
		}
		for _, page := range pages {
			if len(page) == 0 {
				t.Fatal("empty reading page")
			}
			for _, r := range page {
				actual.WriteString(r.Text)
				if r.Y < 112 || r.Y+r.Size > 906 {
					t.Fatal("row outside reading area")
				}
				if measure(p.face(r.Kind, r.Size), r.Text) > 678 {
					t.Fatal("row too wide", r.Text)
				}
			}
		}
		compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
		if compact(actual.String()) != compact(expected.String()) {
			t.Fatal("pagination lost or duplicated text")
		}
		_, e := p.text("test", 40, 112, 26, "sans", 678, 1, false, 0)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 5 {
		t.Fatal("expected many pages", count)
	}
	for page := 0; page < count; page++ {
		if n, e := renderReading(d, page, "../package/urbancomp-ink", path); e != nil || n != count {
			t.Fatal(page, n, e)
		}
	}
}

func TestReaderRendersPublicContent(t *testing.T) {
	m, _ := adapt(fixture(t))
	m.Weather = &Weather{Condition: "多云", Temp: 18, Low: 15, High: 24, Time: "2026-10-25T00:15:00+08:00"}
	for _, selector := range []string{"focus", "radar:0", "radar:1", "radars", "paper:0", "paper:1", "papers", "weather", "about"} {
		d := previewDocument(m, selector)
		path := filepath.Join(t.TempDir(), "detail.png")
		count, e := renderReading(d, 0, "../package/urbancomp-ink", path)
		if e != nil {
			t.Fatal(selector, e)
		}
		for page := 1; page < count; page++ {
			if _, e = renderReading(d, page, "../package/urbancomp-ink", path); e != nil {
				t.Fatal(selector, page, e)
			}
		}
		before, _ := os.ReadFile(path)
		if _, e = renderReading(d, count, "../package/urbancomp-ink", path); e == nil {
			t.Fatal("accepted missing page")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("invalid page overwrote frame")
		}
	}
}

func TestReaderFullCardFields(t *testing.T) {
	m, _ := adapt(fixture(t))
	if len(m.Focus.Highlights) == 0 || len(m.Radars[0].Signals) == 0 || len(m.Radars[0].Viewpoints) == 0 {
		t.Fatal("dropped public detail fields")
	}
	d := readingDocument(m, "radar", 0)
	for _, list := range [][]string{m.Radars[0].Signals, m.Radars[0].Viewpoints} {
		for _, s := range list {
			found := false
			for _, b := range d.Blocks {
				if strings.Contains(b.Text, clean(s)) {
					found = true
				}
			}
			if !found {
				t.Fatal("missing trend or viewpoint")
			}
		}
	}
}

func TestTapRequiresSameTargetAndRejectsReleaseJump(t *testing.T) {
	m, _ := adapt(fixture(t))
	tap := tapped(365, 570)
	tap.End = image.Pt(390, 570)
	if _, changed := (readerSession{Model: m}).navigate(tap); changed {
		t.Fatal("cross-card release accepted")
	}
	var g homeGesture
	now := time.Now()
	for _, e := range [][3]int32{{3, 0x39, 1}, {3, 0x35, 300}, {3, 0x36, 300}, {0, 0, 0}, {3, 0x35, 400}, {3, 0x39, -1}} {
		g.event(uint16(e[0]), uint16(e[1]), e[2], now)
	}
	if g.event(0, 0, 0, now) {
		t.Fatal("release-only coordinate jump accepted")
	}
	// An ordinary article tap reaches the router, just like Home.
	for _, e := range [][3]int32{{3, 0x39, 2}, {3, 0x35, 300}, {3, 0x36, 300}, {0, 0, 0}, {3, 0x39, -1}} {
		g.event(uint16(e[0]), uint16(e[1]), e[2], now)
	}
	if !g.event(0, 0, 0, now) {
		t.Fatal("article tap lost")
	}
}
