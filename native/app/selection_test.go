package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPublisherMetadataIsNotAnAbstract(t *testing.T) {
	p := Paper{Summary: "出版日期：2026年10月23日在线发布。来源：《城市研究》。作者：A、B、C"}
	if _, ready := readableAbstract(p); ready {
		t.Fatal("bibliographic metadata treated as abstract")
	}
	p.Summary += "。摘要：本研究提出城市交通建模方法，并验证其效果。"
	if text, ready := readableAbstract(p); !ready || text != "本研究提出城市交通建模方法，并验证其效果。" {
		t.Fatal(text, ready)
	}
}

func TestSelectedSyntheticPapersRenderEveryPage(t *testing.T) {
	b, e := os.ReadFile("../../fixtures/synthetic-dashboard-with-interests.json")
	if e != nil {
		t.Fatal(e)
	}
	m, e := adapt(b)
	if e != nil {
		t.Fatal(e)
	}
	m = curate(m, Model{}, time.Date(2026, 10, 25, 9, 0, 0, 0, china))
	if len(m.Papers) != 24 {
		t.Fatal("unexpected selected pool", len(m.Papers))
	}
	path := filepath.Join(t.TempDir(), "paper.png")
	for i := range m.Papers {
		d := readingDocument(m, "paper", i)
		count, e := renderReading(d, 0, "../package/urbancomp-ink", path)
		if e != nil {
			t.Fatal(i, e)
		}
		for page := 1; page < count; page++ {
			if _, e = renderReading(d, page, "../package/urbancomp-ink", path); e != nil {
				t.Fatal(i, page, e)
			}
		}
	}
}

func selectionPapers(n int, now time.Time) Model {
	m := Model{Updated: now.Format(time.RFC3339)}
	for i := 0; i < n; i++ {
		m.Papers = append(m.Papers, Paper{ID: fmt.Sprint(i), Title: fmt.Sprintf("城市轨迹交通研究 %d", i), Summary: "研究摘要", Source: "Source", URL: fmt.Sprintf("https://example.org/paper/%d", i), Created: now.Add(time.Duration(-i) * time.Hour).Format(time.RFC3339)})
	}
	return m
}

func TestSelectionRelevanceAndIngestionDate(t *testing.T) {
	now := time.Date(2026, 10, 25, 8, 0, 0, 0, china)
	m := selectionPapers(3, now)
	m.Papers[0].Title = "Ancient pottery classification"
	m.Papers[1].Title = "城市轨迹与交通"
	m.Papers[1].Created = now.Add(-time.Hour).Format(time.RFC3339)
	m.Papers[1].Published = "2020-01-01T00:00:00Z"
	m.Papers[2].Created = now.Add(-30 * 24 * time.Hour).Format(time.RFC3339)
	r := curate(m, Model{}, now)
	if r.Papers[0].ID != "1" || r.Papers[0].Topic != "轨迹交通" {
		t.Fatal("relevant newly collected research not prioritized", r.Papers)
	}
	if r.Papers[0].Published != m.Papers[1].Published || r.Papers[0].Created != m.Papers[1].Created || r.Papers[0].URL != m.Papers[1].URL || r.Updated != m.Updated {
		t.Fatal("source metadata changed")
	}
	if topic, _ := paperTopic(Paper{Title: "Battery capacity measurement"}); topic != "研究" {
		t.Fatal("substring mistaken for city", topic)
	}
}

func TestSelectionDeduplicatesAndExpiresArchive(t *testing.T) {
	now := time.Now()
	m := selectionPapers(3, now)
	m.Papers[0].URL = "https://www.tandfonline.com/doi/full/10.1234/ABC?af=R"
	m.Papers[1].URL = "https://doi.org/10.1234/abc"
	m.Papers[1].Title = "English title for same article"
	m.Papers[2].Title = m.Papers[0].Title
	r := curate(m, Model{}, now)
	if len(r.Papers) != 1 {
		t.Fatal("duplicate DOI or title retained", len(r.Papers))
	}
	old := selectionPapers(1, now)
	old.Papers[0].URL = "https://example.org/old"
	old.Papers[0].Title = "独立旧论文"
	old.Papers[0].CachedAt = now.Add(-15 * 24 * time.Hour).Format(time.RFC3339)
	if n := len(curate(m, old, now).Papers); n != 1 {
		t.Fatal("expired archive retained", n)
	}
	old.Papers[0].CachedAt = now.Add(-time.Hour).Format(time.RFC3339)
	if n := len(curate(m, old, now).Papers); n != 2 {
		t.Fatal("recent archive lost", n)
	}
	if n := len(curate(selectionPapers(60, now), Model{}, now).Papers); n != 24 {
		t.Fatal("unbounded archive", n)
	}
}

func TestRotationCoversPoolAndSurvivesRestart(t *testing.T) {
	now := time.Date(2026, 10, 25, 8, 0, 0, 0, china)
	incoming := selectionPapers(8, now)
	current := curate(incoming, Model{}, now)
	seen := map[string]bool{}
	for round := 0; round < 4; round++ {
		for _, k := range current.Rotation.Featured {
			if seen[k] {
				t.Fatal("repeated before pool covered")
			}
			seen[k] = true
		}
		same := curate(incoming, current, now.Add(time.Duration(round)*30*time.Minute+time.Minute))
		if !reflect.DeepEqual(same.Rotation.Featured, current.Rotation.Featured) {
			t.Fatal("restart within slot rotated again")
		}
		current = curate(incoming, current, now.Add(time.Duration(round+1)*30*time.Minute))
	}
	if len(seen) != 8 {
		t.Fatal("items starved", seen)
	}
	newPaper := incoming.Papers[0]
	newPaper.ID = "new"
	newPaper.URL = "https://example.org/new"
	newPaper.Title = "新的城市轨迹论文"
	incoming.Papers = append(incoming.Papers, newPaper)
	current = curate(incoming, current, now.Add(150*time.Minute))
	found := false
	for _, k := range current.Rotation.Featured {
		if k == paperKey(newPaper) {
			found = true
		}
	}
	if !found {
		t.Fatal("new arrival not featured")
	}
	for row, index := range featuredIndices(current) {
		s, ok := (readerSession{Model: current}).navigate(tapped(200, 795+row*76))
		if !ok || s.Doc.Key != "paper:"+paperKey(current.Papers[index]) {
			t.Fatal("rotating card opened wrong article")
		}
	}
}
