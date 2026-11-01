package main

import (
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("../../fixtures/synthetic-dashboard.json")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestContent(t *testing.T) {
	m, e := adapt(fixture(t))
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Papers) != 6 || len(m.Radars) != 2 {
		t.Fatal("missing content")
	}
	if m.Papers[1].Published == m.Papers[1].Created {
		t.Fatal("conflated publication and ingestion")
	}
	if !strings.Contains(m.Papers[0].Summary, "尚未就绪") {
		t.Fatal("volume metadata treated as abstract")
	}
}
func TestPartialRejected(t *testing.T) {
	var r map[string]any
	json.Unmarshal(fixture(t), &r)
	r["unavailable"] = []string{"papers"}
	b, _ := json.Marshal(r)
	if _, e := adapt(b); e == nil {
		t.Fatal("partial feed accepted")
	}
}
func TestInvalidSources(t *testing.T) {
	for _, s := range []string{"javascript:alert(1)", "file:///tmp/x", "https://name:password@example.org"} {
		if validURL(s) {
			t.Fatal(s)
		}
	}
}
func TestNativeRender(t *testing.T) {
	m, e := adapt(fixture(t))
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "frame.png")
	if e = render(m, "../package/urbancomp-ink", p); e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(p)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	im, e := png.Decode(f)
	if e != nil {
		t.Fatal(e)
	}
	if im.Bounds().Dx() != 758 || im.Bounds().Dy() != 1024 {
		t.Fatal("wrong resolution")
	}
	for y := 0; y < 1024; y++ {
		for x := 0; x < 758; x++ {
			r, g, b, a := im.At(x, y).RGBA()
			if r != g || g != b || r/257%17 != 0 || a != 65535 {
				t.Fatal("not 16-level opaque grayscale")
			}
		}
	}
}
func TestBatteryHeaderLayout(t *testing.T) {
	m, err := adapt(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{100, 21, 0} {
		m.Battery = &level
		if err := withPainter("../package/urbancomp-ink", filepath.Join(t.TempDir(), "battery.png"), func(p *Painter) error { return p.board(m) }); err != nil {
			t.Fatal(level, err)
		}
	}
}

func TestBatteryHeaderRejectsStaleOrInvalidSamples(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	now := time.Now()
	for _, c := range []struct {
		level any
		age   time.Duration
		valid bool
	}{
		{21, 15 * time.Minute, true}, {0, 0, true}, {100, 0, true},
		{21, 61 * time.Minute, false}, {21, -6 * time.Minute, false},
		{-1, 0, false}, {101, 0, false}, {nil, 0, false}, {"21", 0, false},
	} {
		b, _ := json.Marshal(map[string]any{"battery_percent": c.level, "at": now.Add(-c.age).Format(time.RFC3339Nano)})
		if err := os.WriteFile(filepath.Join(dir, "state", "power.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
		if got := currentBattery(dir, now); (got != nil) != c.valid {
			t.Fatalf("%+v: %v", c, got)
		}
	}
}
func TestLongMixedTitles(t *testing.T) {
	m, _ := adapt(fixture(t))
	m.Focus.Title = strings.Repeat("城市空间智能研究（Geospatial Foundation Model）", 5)
	for i := range m.Radars {
		m.Radars[i].Title = m.Focus.Title
	}
	for i := range m.Papers {
		m.Papers[i].Title = m.Focus.Title
	}
	if e := render(m, "../package/urbancomp-ink", filepath.Join(t.TempDir(), "long.png")); e != nil {
		t.Fatal(e)
	}
}
func TestSparse(t *testing.T) {
	m, _ := adapt(fixture(t))
	m.Radars = nil
	m.Papers = m.Papers[:1]
	m.Focus.Summary = ""
	if e := render(m, "../package/urbancomp-ink", filepath.Join(t.TempDir(), "sparse.png")); e != nil {
		t.Fatal(e)
	}
}
func TestWeatherAge(t *testing.T) {
	b := []byte(`{"current":{"time":"2026-10-25T00:15","temperature_2m":18.2,"weather_code":1},"daily":{"time":["2026-10-25"],"temperature_2m_min":[15],"temperature_2m_max":[24]}}`)
	now := time.Date(2026, 10, 25, 0, 30, 0, 0, china)
	w, e := parseWeather(b, now)
	if e != nil || w.Temp != 18 {
		t.Fatal(w, e)
	}
	if _, e = parseWeather(b, now.Add(10*time.Hour)); e == nil {
		t.Fatal("stale weather accepted")
	}
}
func TestAtomicReplace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if e := atomic(p, []byte("old")); e != nil {
		t.Fatal(e)
	}
	if e := atomic(p, []byte("new")); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "new" {
		t.Fatal(string(b), e)
	}
}

func TestStableResearch(t *testing.T) {
	for _, tc := range []struct {
		name, before, snapshot, after string
		accepted                      bool
	}{
		{"stable", `{"version":"a","running":false}`, `{"version":"a"}`, `{"version":"a","running":false}`, true},
		{"running", `{"version":"a","running":true}`, `{"version":"a"}`, `{"version":"a","running":false}`, false},
		{"changed", `{"version":"a","running":false}`, `{"version":"a"}`, `{"version":"b","running":false}`, false},
		{"mismatch", `{"version":"a","running":false}`, `{"version":"b"}`, `{"version":"a","running":false}`, false},
		{"missing", `{"version":"a"}`, `{"version":"a"}`, `{"version":"a","running":false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			_, err := stableResearch(func(u string) ([]byte, error) {
				calls++
				if calls == 1 {
					return []byte(tc.before), nil
				}
				if calls == 2 {
					if u != researchURL {
						t.Fatal(u)
					}
					return []byte(tc.snapshot), nil
				}
				return []byte(tc.after), nil
			})
			if (err == nil) != tc.accepted {
				t.Fatal(err)
			}
		})
	}
}

func TestNativeWeatherRender(t *testing.T) {
	m, _ := adapt(fixture(t))
	m.ResearchStale = true
	m.Weather = &Weather{Condition: "雷雨伴冰雹", Temp: 18, Low: 15, High: 24, Time: "2026-10-25T00:15:00+08:00"}
	if e := render(m, "../package/urbancomp-ink", filepath.Join(t.TempDir(), "weather.png")); e != nil {
		t.Fatal(e)
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCancelInFlightNetwork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	entered := make(chan struct{})
	c := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	go func() { <-entered; cancel() }()
	_, e := get(ctx, c, researchURL)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestCanceledCycleKeepsFrame(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	p := filepath.Join(dir, "state", "frame.png")
	os.WriteFile(p, []byte("existing valid frame"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := cycle(ctx, dir, "../../fixtures/synthetic-dashboard.json", "", true)
	if !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "existing valid frame" {
		t.Fatal("cancelled operation replaced cache")
	}
}
