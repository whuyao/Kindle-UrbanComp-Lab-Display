package main

import (
	"fmt"
	"image"
	"strings"
	"time"
)

type block struct {
	Text, Kind string
	Size       int
	Bold       bool
	Shade      uint8
}
type document struct {
	Label        string
	Blocks       []block
	Key, Kind    string
	Index, Count int
}
type readingRow struct {
	block
	Y int
}
type readingPage []readingRow

func (d *document) add(text, kind string, size int, bold bool, shade uint8) {
	if text = clean(text); text != "" {
		d.Blocks = append(d.Blocks, block{text, kind, size, bold, shade})
	}
}
func (d *document) title(s string) { d.add(s, "serif", 34, true, 0) }
func (d *document) body(s string)  { d.add(s, "sans", 26, false, 17) }
func (d *document) meta(s string)  { d.add(s, "sans", 18, false, 68) }
func readableDate(s string) string {
	if t, e := time.Parse(time.RFC3339, s); e == nil {
		return t.In(china).Format("2006.01.02 15:04")
	}
	if t, e := time.Parse("2006-01-02", s); e == nil {
		return t.Format("2006.01.02")
	}
	return ""
}
func (d *document) card(c Card) {
	d.title(c.Title)
	stamp := readableDate(c.Updated)
	if stamp == "" {
		stamp = readableDate(c.Date)
	}
	if stamp != "" {
		d.meta("更新 " + stamp)
	}
	d.body(c.Summary)
	for _, group := range []struct {
		title string
		items []string
	}{
		{"简报要点", c.Highlights}, {"趋势信号", c.Signals}, {"核心观点", c.Viewpoints},
	} {
		if len(group.items) == 0 {
			continue
		}
		d.add(group.title, "sans", 26, true, 0)
		for i, s := range group.items {
			d.body(fmt.Sprintf("%02d  %s", i+1, s))
		}
	}
}
func (d *document) paper(p Paper) {
	d.title(p.Title)
	source := p.Source
	if p.Topic != "" {
		source = p.Topic + " · " + source
	}
	d.meta(source)
	dates := []string{}
	if date := readableDate(p.Published); date != "" {
		dates = append(dates, "发表 "+date)
	}
	if date := readableDate(p.Created); date != "" {
		dates = append(dates, "收录 "+date)
	}
	d.meta(strings.Join(dates, "  /  "))
	d.add("摘要", "sans", 22, true, 0)
	if summary, ready := readableAbstract(p); !ready {
		d.body("摘要尚未就绪，可在原文查看。")
	} else {
		d.body(summary)
	}
	d.meta("原文来源")
	d.meta(p.URL)
}

func readingDocument(m Model, kind string, index int) document {
	d := document{Kind: kind}
	switch kind {
	case "focus":
		d.Key = "focus:" + m.Focus.Key + ":" + m.Focus.Date
		d.Label = "研究简报"
		d.card(m.Focus)
	case "radar":
		d.Label = "研究雷达"
		if index >= 0 && index < len(m.Radars) {
			d.Key = "radar:" + m.Radars[index].Title
			d.card(m.Radars[index])
		}
	case "radars":
		d.Key = "radars"
		d.Label = "研究雷达"
		for _, c := range m.Radars {
			d.card(c)
		}
	case "paper":
		d.Label = "值得精读"
		if index >= 0 && index < len(m.Papers) {
			d.Key = "paper:" + paperKey(m.Papers[index])
			d.Index = index
			d.Count = len(m.Papers)
			d.paper(m.Papers[index])
		}
	case "papers":
		d.Label = "值得精读"
		for _, p := range m.Papers {
			d.paper(p)
		}
	case "weather":
		d.Label = "光谷天气"
		d.title("武汉 · 光谷")
		if w := m.Weather; w != nil {
			d.body(fmt.Sprintf("%s  %d℃", w.Condition, w.Temp))
			d.body(fmt.Sprintf("今日 %d–%d℃", w.Low, w.High))
			d.meta("观测 " + readableDate(w.Time))
			if w.Stale {
				d.meta("天气待更新")
			}
		} else {
			d.body("天气暂不可用")
		}
		d.meta("Open-Meteo · 光谷附近网格预报")
	default:
		d.Label = "关于知新"
		d.title("知新")
		d.body("城市、空间与 GeoAI 研究前沿。")
		d.meta("资料更新 " + readableDate(m.Updated))
		d.body("每半小时检查新内容。")
	}
	// The dashboard shows connectivity; it must not change an article's layout
	// or invalidate a saved page merely because a request failed.
	d.meta("知识库  research.urbancomp.dev")
	return d
}

// Paginate measured rows instead of clipping text to a fixed number of lines.
// Each source block remains complete, including long English titles and URLs.
func paginate(p *Painter, d document) []readingPage {
	pages := []readingPage{{}}
	top := 112
	if d.Kind == "paper" {
		top = 163
	}
	y := top
	for _, b := range d.Blocks {
		step := int(float64(b.Size)*1.48 + 0.5)
		for _, s := range wrap(p.face(b.Kind, b.Size), b.Text, 674) {
			if y+step > 906 {
				pages = append(pages, readingPage{})
				y = top
			}
			row := b
			row.Text = s
			pages[len(pages)-1] = append(pages[len(pages)-1], readingRow{row, y})
			y += step
		}
		y += 18
	}
	return pages
}

func renderReading(d document, page int, dir, path string) (count int, err error) {
	err = withPainter(dir, path, func(p *Painter) (e error) {
		pages := paginate(p, d)
		count = len(pages)
		if page < 0 || page >= count {
			return fmt.Errorf("reading page out of range: %d/%d", page, count)
		}
		text := func(s string, x, y, size, width int, kind string, bold bool, shade uint8) {
			if e == nil {
				_, e = p.text(s, x, y, size, kind, width, 1, bold, shade)
			}
		}
		text("‹  返回知新", 40, 39, 22, 250, "sans", false, 34)
		text(d.Label, 718-measure(p.face("sans", 21), d.Label), 40, 21, 300, "sans", true, 0)
		p.rule(80, true)
		if d.Kind == "paper" {
			previous, next := uint8(187), uint8(187)
			if d.Index > 0 {
				previous = 34
			}
			if d.Index+1 < d.Count {
				next = 34
			}
			text("‹ 上一篇", 40, 106, 20, 170, "sans", false, previous)
			text(fmt.Sprintf("%d / %d 篇", d.Index+1, d.Count), 325, 106, 18, 150, "sans", false, 68)
			text("下一篇 ›", 620, 106, 20, 98, "sans", false, next)
			p.rule(143, false)
		}
		for _, row := range pages[page] {
			text(row.Text, 40, row.Y, row.Size, 678, row.Kind, row.Bold, row.Shade)
		}
		p.rule(929, true)
		previous, next := uint8(187), uint8(187)
		if page > 0 {
			previous = 34
		}
		if page+1 < count {
			next = 34
		}
		text("‹  上一页", 40, 963, 22, 170, "sans", false, previous)
		text("下一页  ›", 236, 963, 22, 170, "sans", false, next)
		text(fmt.Sprintf("%d / %d", page+1, count), 451, 965, 19, 160, "sans", false, 68)
		text("主页", 669, 963, 18, 48, "sans", false, 68)
		if e == nil {
			e = p.brand(609, 996, 20, 14)
		}
		return e
	})
	return count, err
}

var backTarget = image.Rect(25, 20, 285, 85)
var previousTarget = image.Rect(25, 943, 211, 1005)
var nextTarget = image.Rect(220, 943, 410, 1005)
var previousPaperTarget = image.Rect(25, 94, 230, 143)
var nextPaperTarget = image.Rect(530, 94, 733, 143)
var resumeTarget = image.Rect(530, 726, 729, 772)

type readerSession struct {
	Model       Model
	Doc         *document
	Page, Pages int
	Memory      readingMemory
	Queue       []Paper
	Index       int
}

// A refresh changes only the dashboard model. The open document is a snapshot
// so a scheduled refresh cannot move the reader's page or replace its text.
func (s *readerSession) refresh(m Model) { s.Model = m }

func (s readerSession) navigate(t Tap) (readerSession, bool) {
	if s.Doc != nil {
		if t.in(previousPaperTarget) {
			return s.adjacentPaper(-1)
		}
		if t.in(nextPaperTarget) {
			return s.adjacentPaper(1)
		}
		switch {
		case t.in(backTarget):
			s.Doc = nil
			s.Page, s.Pages = 0, 0
		case t.in(previousTarget) && s.Page > 0:
			s.Page--
		case t.in(nextTarget) && s.Page+1 < s.Pages:
			s.Page++
		default:
			return s, false
		}
		return s, true
	}
	kind, index := "", 0
	switch {
	case t.in(resumeTarget) && s.Memory.resumeExists(s.Model):
		kind, index, _ = s.Memory.resumeTarget(s.Model)
	case t.in(image.Rect(30, 25, 729, 205)):
		kind = "about"
	case t.in(image.Rect(30, 225, 729, 477)):
		kind = "focus"
	case t.in(image.Rect(30, 492, 729, 536)):
		kind = "radars"
	case t.in(image.Rect(30, 537, 375, 704)):
		kind = "radar"
	case t.in(image.Rect(385, 537, 729, 704)):
		kind, index = "radar", 1
	case t.in(image.Rect(30, 726, 729, 772)):
		kind = "papers"
	case t.in(image.Rect(30, 773, 729, 848)):
		kind = "paper"
		indices := featuredIndices(s.Model)
		if len(indices) == 0 {
			return s, false
		}
		index = indices[0]
	case t.in(image.Rect(30, 849, 729, 928)):
		kind = "paper"
		indices := featuredIndices(s.Model)
		if len(indices) < 2 {
			return s, false
		}
		index = indices[1]
	case t.in(image.Rect(30, 947, 625, 992)):
		kind = "weather"
	default:
		return s, false
	}
	if (kind == "radar" && index >= len(s.Model.Radars)) || (kind == "paper" && index >= len(s.Model.Papers)) {
		return s, false
	}
	return s.open(kind, index), true
}

// Used by the offline preview command; it does not fetch or launch a browser.
func previewDocument(m Model, selector string) document {
	kind, value, _ := strings.Cut(selector, ":")
	index := 0
	if value != "" {
		fmt.Sscanf(value, "%d", &index)
	}
	return readingDocument(m, kind, index)
}
