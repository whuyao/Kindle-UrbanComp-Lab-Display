package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Painter struct {
	Img   *image.Gray
	Fonts map[string]*opentype.Font
	Faces map[string]font.Face
	Boxes []image.Rectangle
	Logo  image.Image
}

func newPainter(dir string) (*Painter, error) {
	p := &Painter{Img: image.NewGray(image.Rect(0, 0, 758, 1024)), Fonts: map[string]*opentype.Font{}, Faces: map[string]font.Face{}}
	logo, err := loadBrand()
	if err != nil {
		return nil, err
	}
	p.Logo = logo
	draw.Draw(p.Img, p.Img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	for name, file := range map[string]string{"sans": "SourceHanSansCN-Regular.otf", "serif": "SourceHanSerifCN-Regular.otf"} {
		b, e := os.ReadFile(filepath.Join(dir, "fonts", file))
		if e != nil {
			return nil, e
		}
		f, e := opentype.Parse(b)
		if e != nil {
			return nil, e
		}
		p.Fonts[name] = f
	}
	return p, nil
}
func (p *Painter) face(kind string, size int) font.Face {
	k := fmt.Sprintf("%s/%d", kind, size)
	if f := p.Faces[k]; f != nil {
		return f
	}
	f, e := opentype.NewFace(p.Fonts[kind], &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingNone})
	if e != nil {
		panic(e)
	}
	p.Faces[k] = f
	return f
}
func measure(f font.Face, s string) int { return font.MeasureString(f, s).Ceil() }

var textTokens = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_./:+-]*|.`)

func wrap(f font.Face, s string, width int) []string {
	rows := []string{}
	line := ""
	for _, token := range textTokens.FindAllString(s, -1) {
		parts := []string{token}
		if measure(f, token) > width {
			parts = nil
			for _, r := range token {
				parts = append(parts, string(r))
			}
		}
		for _, part := range parts {
			if line != "" && measure(f, line+part) > width {
				carry := ""
				runes := []rune(line)
				first := []rune(part)[0]
				if strings.ContainsRune("，。！？：；、）》」』】,.;:!?%）", first) || strings.ContainsRune("（《「『【(", runes[len(runes)-1]) {
					carry = string(runes[len(runes)-1])
					line = string(runes[:len(runes)-1])
				}
				if strings.TrimSpace(line) != "" {
					rows = append(rows, strings.TrimSpace(line))
				}
				line = carry + strings.TrimLeft(part, " ")
			} else {
				line += part
			}
		}
	}
	if line != "" {
		rows = append(rows, strings.TrimSpace(line))
	}
	return rows
}
func (p *Painter) text(s string, x, y, size int, kind string, width, limit int, bold bool, shade uint8) (int, error) {
	f := p.face(kind, size)
	rows := wrap(f, s, width)
	if len(rows) > limit {
		rows = rows[:limit]
		r := []rune(rows[limit-1])
		for len(r) > 0 && measure(f, string(r)+"…") > width {
			r = r[:len(r)-1]
		}
		rows[limit-1] = string(r) + "…"
	}
	step := int(float64(size)*1.38 + 0.5)
	for _, row := range rows {
		bounds, _ := font.BoundString(f, row)
		baseline := y - bounds.Min.Y.Floor()
		rect := image.Rect(x+bounds.Min.X.Floor(), baseline+bounds.Min.Y.Floor(), x+bounds.Max.X.Ceil(), baseline+bounds.Max.Y.Ceil())
		if bold {
			rect.Max.X++
		}
		if !rect.In(p.Img.Bounds()) {
			return y, fmt.Errorf("text overflow: %s", row)
		}
		for _, prior := range p.Boxes {
			if !rect.Intersect(prior).Empty() {
				return y, fmt.Errorf("text overlap: %s", row)
			}
		}
		p.Boxes = append(p.Boxes, rect)
		d := font.Drawer{Dst: p.Img, Src: image.NewUniform(color.Gray{Y: shade}), Face: f, Dot: fixed.P(x, baseline)}
		d.DrawString(row)
		if bold {
			d.Dot = fixed.P(x+1, baseline)
			d.DrawString(row)
		}
		y += step
	}
	return y, nil
}
func (p *Painter) rule(y int, heavy bool) {
	shade := uint8(153)
	h := 1
	if heavy {
		shade = 0
		h = 3
	}
	draw.Draw(p.Img, image.Rect(40, y, 718, y+h), image.NewUniform(color.Gray{Y: shade}), image.Point{}, draw.Src)
}

// Font data is large on PW2. Share one painter, serialized between the reader
// and background dashboard rendering; never load two font sets concurrently.
var painterMu sync.Mutex
var sharedPainter *Painter
var painterDir string

func withPainter(dir, path string, drawPage func(*Painter) error) error {
	painterMu.Lock()
	defer painterMu.Unlock()
	if sharedPainter == nil || painterDir != dir {
		if sharedPainter != nil {
			for _, face := range sharedPainter.Faces {
				face.Close()
			}
			sharedPainter = nil
		}
		p, e := newPainter(dir)
		if e != nil {
			return e
		}
		sharedPainter, painterDir = p, dir
	}
	p := sharedPainter
	p.Boxes = p.Boxes[:0]
	draw.Draw(p.Img, p.Img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if e := drawPage(p); e != nil {
		return e
	}
	return p.save(path)
}

func render(m Model, dir, path string) error {
	m.Resume = loadReading(dir).resumeExists(m)
	m.Battery = currentBattery(dir, time.Now())
	return withPainter(dir, path, func(p *Painter) error { return p.board(m) })
}

func (p *Painter) board(m Model) (err error) {
	// Stop on any layout error before saving a candidate frame.
	text := func(s string, x, y, size int, kind string, width, lines int, bold bool, shade uint8) int {
		if err != nil {
			return y
		}
		var end int
		end, err = p.text(s, x, y, size, kind, width, lines, bold, shade)
		return end
	}
	if err = p.brand(40, 28, 36, 21); err != nil {
		return err
	}
	website := "research.urbancomp.dev"
	text(website, 718-measure(p.face("sans", 20), website), 36, 20, "sans", 360, 1, false, 34)
	p.rule(71, true)
	text("知新", 38, 91, 75, "serif", 450, 1, true, 0)
	text("研究前沿", 547, 111, 34, "serif", 171, 1, true, 0)
	t, e := time.Parse(time.RFC3339, m.Updated)
	if e != nil {
		return e
	}
	t = t.In(china)
	text(t.Format("2006.01.02")+"  /  "+strings.ToUpper(t.Format("Mon")), 40, 177, 18, "sans", 350, 1, true, 0)
	if m.Battery != nil {
		text(fmt.Sprintf("电量 %d%%", *m.Battery), 400, 177, 17, "sans", 140, 1, *m.Battery <= 25, 34)
	}
	label := "GEOAI · 城市 · 空间"
	f := p.face("sans", 17)
	text(label, 718-measure(f, label), 177, 17, "sans", 300, 1, false, 68)
	p.rule(213, true)
	text("01  /  研究简报  ›", 40, 236, 18, "sans", 500, 1, true, 0)
	stamp := ""
	if updated, e := time.Parse(time.RFC3339, m.Focus.Updated); e == nil {
		stamp = updated.In(china).Format("01.02 15:04")
	} else if len(m.Focus.Date) == 10 {
		stamp = strings.ReplaceAll(m.Focus.Date[5:], "-", ".")
	}
	if stamp != "" {
		text(stamp, 718-measure(f, stamp), 236, 17, "sans", 220, 1, false, 68)
	}
	end := text(m.Focus.Title, 40, 277, 40, "serif", 678, 2, true, 0)
	lines := 2
	if end < 350 {
		lines = 3
	}
	text(m.Focus.Summary, 40, end+24, 26, "sans", 678, lines, false, 34)
	p.rule(483, false)
	text("02  /  研究雷达  ›", 40, 504, 18, "sans", 500, 1, true, 0)
	// The section timestamp follows only its displayed cards, not newer papers.
	radarsUpdated := ""
	for i, card := range m.Radars {
		if i >= 2 {
			break
		}
		radarsUpdated = latest(radarsUpdated, card.Updated)
	}
	if updated, e := time.Parse(time.RFC3339, radarsUpdated); e == nil {
		stamp = updated.In(china).Format("01.02 15:04")
		text(stamp, 718-measure(f, stamp), 504, 17, "sans", 220, 1, false, 68)
	}
	if len(m.Radars) == 0 {
		text("暂无更新，保留已收录研究。", 40, 551, 26, "sans", 678, 1, false, 34)
	}
	for i, r := range m.Radars {
		if i >= 2 {
			break
		}
		x := 40 + i*353
		end = text(r.Title, x, 543, 27, "sans", 324, 2, true, 0)
		lines = 2
		if end < 600 {
			lines = 3
		}
		text(r.Summary, x, end+16, 24, "sans", 324, lines, false, 34)
	}
	if len(m.Radars) > 1 {
		draw.Draw(p.Img, image.Rect(378, 544, 379, 686), image.NewUniform(color.Gray{Y: 187}), image.Point{}, draw.Src)
	}
	p.rule(714, false)
	text("03  /  值得精读  ›", 40, 738, 18, "sans", 500, 1, true, 0)
	readingLabel := fmt.Sprintf("共 %d 篇", len(m.Papers))
	if m.Resume {
		readingLabel = "继续读  ›"
	}
	text(readingLabel, 718-measure(p.face("sans", 18), readingLabel), 738, 18, "sans", 180, 1, false, 68)
	for i, index := range featuredIndices(m) {
		item := m.Papers[index]
		y := 779 + i*76
		text(fmt.Sprintf("%02d", i+1), 40, y, 27, "serif", 40, 1, true, 0)
		text(item.Title, 87, y, 27, "sans", 631, 1, true, 0)
		source := item.Source
		if item.Topic != "" {
			source = item.Topic + " · " + source
		}
		text(source, 87, y+39, 18, "sans", 631, 1, false, 68)
	}
	p.rule(941, true)
	weather := "武汉光谷  ·  天气暂不可用"
	footer := t.Format("资料 01.02 15:04")
	if m.ResearchStale {
		footer += "（待更新）"
	} else if m.SelectionStale {
		footer += "（方向待更新）"
	}
	if w := m.Weather; w != nil {
		weather = fmt.Sprintf("武汉光谷  ·  %s  %d℃", w.Condition, w.Temp)
		wt, _ := time.Parse(time.RFC3339, w.Time)
		footer += fmt.Sprintf("   /   天气 %s   %d–%d℃", wt.In(china).Format("15:04"), w.Low, w.High)
		if w.Stale {
			footer += "（缓存）"
		}
	}
	text(weather, 40, 962, 24, "sans", 585, 1, true, 0)
	// The visible control is small; the touch target extends beyond its border.
	for _, edge := range []image.Rectangle{image.Rect(657, 955, 718, 956), image.Rect(657, 988, 718, 989), image.Rect(657, 955, 658, 989), image.Rect(717, 955, 718, 989)} {
		draw.Draw(p.Img, edge, image.NewUniform(color.Gray{Y: 153}), image.Point{}, draw.Src)
	}
	text("主页", 669, 963, 18, "sans", 48, 1, false, 68)
	text(footer, 40, 994, 16, "sans", 575, 1, false, 68)
	if m.Weather != nil {
		text("Open-Meteo", 629, 995, 14, "sans", 89, 1, false, 68)
	}
	return err
}

func currentBattery(dir string, now time.Time) *int {
	path := filepath.Join(dir, "state", "power.json")
	if st, e := os.Stat(path); e != nil || st.Size() > 4096 {
		return nil
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil
	}
	var state struct {
		Battery *int   `json:"battery_percent"`
		At      string `json:"at"`
	}
	if json.Unmarshal(b, &state) != nil || state.Battery == nil || *state.Battery < 0 || *state.Battery > 100 {
		return nil
	}
	at, e := time.Parse(time.RFC3339Nano, state.At)
	if e != nil || now.Sub(at) > time.Hour || at.After(now.Add(5*time.Minute)) {
		return nil
	}
	return state.Battery
}

func (p *Painter) save(path string) error {
	for i, v := range p.Img.Pix {
		p.Img.Pix[i] = uint8((int(v) + 8) / 17 * 17)
	}
	fOut, e := os.Create(path)
	if e != nil {
		return e
	}
	e = png.Encode(fOut, p.Img)
	closeErr := fOut.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if len(p.Boxes) == 0 {
		return errors.New("empty output")
	}
	return nil
}
