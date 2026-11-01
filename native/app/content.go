package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const researchURL = "https://research.urbancomp.dev/api/dashboard"
const versionURL = researchURL + "?mode=version"

var interestURLs = []string{
	"https://research.urbancomp.dev/api/backend/items?limit=8&offset=0&content_type=paper&sort=created&tag=GeoAI",
	"https://research.urbancomp.dev/api/backend/items?limit=8&offset=0&content_type=paper&sort=created&tag=UrbanComputing",
	"https://research.urbancomp.dev/api/backend/items?limit=8&offset=0&content_type=paper&sort=created&q=trajectory",
}

const weatherURL = "https://api.open-meteo.com/v1/forecast?latitude=30.51&longitude=114.40&current=temperature_2m,weather_code&daily=temperature_2m_max,temperature_2m_min&timezone=Asia%2FShanghai&forecast_days=1"

var china = time.FixedZone("CST", 8*3600)

type Card struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Key        string   `json:"card_key"`
	Date       string   `json:"date_key"`
	Updated    string   `json:"updated_at"`
	IDs        []string `json:"item_ids"`
	Direction  string   `json:"direction_title"`
	Trend      string   `json:"trend_summary"`
	Highlights []string `json:"highlights,omitempty"`
	Signals    []string `json:"trend_signals,omitempty"`
	Viewpoints []string `json:"core_viewpoints,omitempty"`
}
type Paper struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Translated        string   `json:"translated_title"`
	Summary           string   `json:"summary"`
	TranslatedSummary string   `json:"translated_summary"`
	Source            string   `json:"source_name"`
	URL               string   `json:"source_url"`
	Updated           string   `json:"updated_at"`
	Published         string   `json:"published_at"`
	Created           string   `json:"created_at"`
	ContentType       string   `json:"content_type,omitempty"`
	DOI               string   `json:"doi,omitempty"`
	Tags              []string `json:"topic_tags,omitempty"`
	Keywords          []string `json:"keywords,omitempty"`
	Topic             string   `json:"ink_topic,omitempty"`
	CachedAt          string   `json:"ink_cached_at,omitempty"`
}
type Dashboard struct {
	InterestFailures []string `json:"ink_interest_failures,omitempty"`
	Version          string   `json:"version"`
	Published        string   `json:"published_at"`
	Unavailable      []string `json:"unavailable"`
	Home             *struct {
		Daily *struct {
			Cards []Card `json:"cards"`
		} `json:"daily_summary"`
	} `json:"home"`
	Papers *struct {
		Items []Paper `json:"items"`
	} `json:"papers"`
	Digest *struct {
		Cards []Card `json:"cards"`
	} `json:"digest"`
	Latest *struct {
		Items []Paper `json:"items"`
	} `json:"latest"`
}
type Weather struct {
	Condition string `json:"condition"`
	Temp      int    `json:"temperature"`
	Low       int    `json:"low"`
	High      int    `json:"high"`
	Time      string `json:"time"`
	Stale     bool   `json:"stale"`
}
type Model struct {
	SelectionStale bool          `json:"selection_stale"`
	Updated        string        `json:"updated_at"`
	Focus          Card          `json:"focus"`
	Radars         []Card        `json:"radars"`
	Papers         []Paper       `json:"papers"`
	Weather        *Weather      `json:"weather,omitempty"`
	ResearchStale  bool          `json:"research_stale"`
	Rotation       rotationState `json:"rotation,omitempty"`
	Resume         bool          `json:"-"`
	Battery        *int          `json:"-"`
}

var tags = regexp.MustCompile(`<[^>]*>`)

func clean(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, ""))), " ")
}
func validURL(s string) bool {
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}
func latest(a, b string) string {
	x, e := time.Parse(time.RFC3339, a)
	y, f := time.Parse(time.RFC3339, b)
	if e != nil {
		return b
	}
	if f == nil && y.After(x) {
		return b
	}
	return a
}

func adapt(data []byte) (Model, error) {
	var d Dashboard
	if e := json.Unmarshal(data, &d); e != nil {
		return Model{}, e
	}
	if d.Home == nil || d.Papers == nil || d.Version == "" || len(d.Unavailable) > 0 {
		return Model{}, errors.New("incomplete research snapshot")
	}
	m := Model{SelectionStale: len(d.InterestFailures) > 0}
	seen := map[string]bool{}
	items := append([]Paper(nil), d.Papers.Items...)
	if d.Latest != nil {
		for _, p := range d.Latest.Items {
			if p.ContentType == "paper" {
				items = append(items, p)
			}
		}
	}
	for _, p := range items {
		if p.Translated != "" {
			p.Title = p.Translated
		}
		p.Title = clean(p.Title)
		p.Source = clean(p.Source)
		if p.ID == "" || p.Title == "" || p.Source == "" || !validURL(p.URL) || seen[p.ID] || seen[p.URL] {
			continue
		}
		if p.TranslatedSummary != "" {
			p.Summary = p.TranslatedSummary
		}
		p.Summary = clean(p.Summary)
		if strings.HasPrefix(p.Summary, "Volume ") {
			p.Summary = "摘要尚未就绪，可在原文查看。"
		}
		seen[p.ID] = true
		seen[p.URL] = true
		m.Papers = append(m.Papers, p)
		m.Updated = latest(m.Updated, p.Updated)
		if len(m.Papers) == 48 {
			break
		}
	}
	if len(m.Papers) == 0 {
		return m, errors.New("no valid papers")
	}
	m.Focus = Card{Title: m.Papers[0].Title, Summary: m.Papers[0].Summary, IDs: []string{m.Papers[0].ID}}
	if d.Home.Daily != nil {
		for _, c := range d.Home.Daily.Cards {
			if c.Key == "overview" && len(c.IDs) > 0 && clean(c.Title) != "" {
				c.Title = clean(c.Title)
				c.Summary = clean(c.Summary)
				m.Focus = c
				m.Updated = latest(m.Updated, c.Updated)
				break
			}
		}
	}
	if d.Digest != nil {
		for _, c := range d.Digest.Cards {
			if len(c.IDs) == 0 || c.Direction == "" {
				continue
			}
			c.Title = clean(c.Direction)
			c.Summary = clean(c.Trend)
			m.Radars = append(m.Radars, c)
			m.Updated = latest(m.Updated, c.Updated)
			if len(m.Radars) == 2 {
				break
			}
		}
	}
	if m.Updated == "" {
		m.Updated = d.Published
	}
	if _, e := time.Parse(time.RFC3339, m.Updated); e != nil {
		return m, errors.New("invalid source timestamp")
	}
	return m, nil
}

func client(caPath string) (*http.Client, error) {
	b, e := os.ReadFile(caPath)
	if e != nil {
		return nil, e
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		return nil, errors.New("empty trust store")
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 12 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxIdleConns: 4, MaxIdleConnsPerHost: 3, IdleConnTimeout: 20 * time.Second}
	return &http.Client{Transport: tr, Timeout: 35 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error { return errors.New("redirect rejected") }}, nil
}
func get(ctx context.Context, c *http.Client, u string) ([]byte, error) {
	allowed := u == researchURL || u == versionURL || u == weatherURL
	for _, source := range interestURLs {
		if u == source {
			allowed = true
		}
	}
	if !allowed {
		return nil, errors.New("URL is not allowed")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", "UrbanCompInk-Native/"+appVersion)
	req.Header.Set("Accept", "application/json")
	resp, e := c.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 2000001))
	if e != nil {
		return nil, e
	}
	if len(b) > 2000000 {
		return nil, errors.New("response too large")
	}
	return b, nil
}

// The public site's read-only filters supply candidates beyond its homepage.
// All reads remain inside the before/after version guard.
func researchWithInterests(fetch func(string) ([]byte, error)) ([]byte, error) {
	return stableResearch(func(u string) ([]byte, error) {
		b, e := fetch(u)
		if e != nil || u != researchURL {
			return b, e
		}
		var d Dashboard
		if e = json.Unmarshal(b, &d); e != nil {
			return nil, e
		}
		if d.Papers == nil {
			return b, nil
		}
		type result struct {
			papers []Paper
			err    error
		}
		results := make([]result, len(interestURLs))
		var wg sync.WaitGroup
		for i, source := range interestURLs {
			wg.Add(1)
			go func(i int, source string) {
				defer wg.Done()
				body, err := fetch(source)
				var list struct {
					Items []Paper `json:"items"`
					Total *int    `json:"total"`
				}
				if err == nil {
					err = json.Unmarshal(body, &list)
				}
				if err == nil && (list.Total == nil || list.Items == nil) {
					err = errors.New("incomplete interest results")
				}
				results[i] = result{list.Items, err}
			}(i, source)
		}
		wg.Wait()
		for i, r := range results {
			if r.err != nil {
				log.Printf("INTEREST_FAILED source=%s error=%v", interestURLs[i], r.err)
				d.InterestFailures = append(d.InterestFailures, interestURLs[i])
				continue
			}
			for _, p := range r.papers {
				if p.ContentType == "paper" {
					d.Papers.Items = append(d.Papers.Items, p)
				}
			}
		}
		return json.Marshal(d)
	})
}

// A refresh may replace several upstream tables. Only accept a complete,
// stable version observed on both sides of the snapshot request.
func stableResearch(fetch func(string) ([]byte, error)) ([]byte, error) {
	version := func() (string, error) {
		b, e := fetch(versionURL)
		if e != nil {
			return "", e
		}
		var v struct {
			Version string `json:"version"`
			Running *bool  `json:"running"`
		}
		if e = json.Unmarshal(b, &v); e != nil {
			return "", e
		}
		if v.Version == "" || v.Running == nil || *v.Running {
			return "", errors.New("research update is incomplete")
		}
		return v.Version, nil
	}
	before, e := version()
	if e != nil {
		return nil, e
	}
	b, e := fetch(researchURL)
	if e != nil {
		return nil, e
	}
	var snapshot struct {
		Version string `json:"version"`
	}
	if e = json.Unmarshal(b, &snapshot); e != nil {
		return nil, e
	}
	after, e := version()
	if e != nil {
		return nil, e
	}
	if before != after || snapshot.Version != before {
		return nil, errors.New("research version changed during fetch")
	}
	return b, nil
}

var conditions = map[int]string{0: "晴", 1: "晴间多云", 2: "多云", 3: "阴", 45: "雾", 48: "雾", 51: "毛毛雨", 53: "毛毛雨", 55: "毛毛雨", 56: "冻雨", 57: "冻雨", 61: "小雨", 63: "中雨", 65: "大雨", 66: "冻雨", 67: "冻雨", 71: "小雪", 73: "中雪", 75: "大雪", 77: "米雪", 80: "阵雨", 81: "阵雨", 82: "强阵雨", 85: "阵雪", 86: "强阵雪", 95: "雷雨", 96: "雷雨伴冰雹", 99: "雷雨伴冰雹"}

func parseWeather(b []byte, now time.Time) (*Weather, error) {
	var r struct {
		Current struct {
			Time string   `json:"time"`
			Temp *float64 `json:"temperature_2m"`
			Code *int     `json:"weather_code"`
		} `json:"current"`
		Daily struct {
			Date []string  `json:"time"`
			Low  []float64 `json:"temperature_2m_min"`
			High []float64 `json:"temperature_2m_max"`
		} `json:"daily"`
	}
	if e := json.Unmarshal(b, &r); e != nil {
		return nil, e
	}
	cur := r.Current
	if cur.Temp == nil || cur.Code == nil || len(r.Daily.Date) != 1 || len(r.Daily.Low) != 1 || len(r.Daily.High) != 1 {
		return nil, errors.New("weather fields missing")
	}
	t, e := time.ParseInLocation("2006-01-02T15:04", cur.Time, china)
	if e != nil {
		return nil, e
	}
	delta := now.Sub(t)
	if delta > 3*time.Hour || delta < -3*time.Hour || r.Daily.Date[0] != t.Format("2006-01-02") {
		return nil, errors.New("weather is stale")
	}
	cond, ok := conditions[*cur.Code]
	if !ok {
		return nil, errors.New("unknown weather code")
	}
	round := func(v float64) int {
		if v < 0 {
			return int(v - 0.5)
		}
		return int(v + 0.5)
	}
	for _, v := range []float64{*cur.Temp, r.Daily.Low[0], r.Daily.High[0]} {
		if v < -80 || v > 70 {
			return nil, errors.New("weather outside valid range")
		}
	}
	return &Weather{Condition: cond, Temp: round(*cur.Temp), Low: round(r.Daily.Low[0]), High: round(r.Daily.High[0]), Time: t.Format(time.RFC3339)}, nil
}
