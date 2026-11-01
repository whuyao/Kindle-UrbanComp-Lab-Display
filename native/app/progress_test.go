package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArticleNavigationAndSavedPositions(t *testing.T) {
	m := selectionPapers(4, time.Now())
	s := (readerSession{Model: m}).open("paper", 1)
	s.Page, s.Pages = 2, 5
	s.Memory.remember(*s.Doc, s.Page)
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	if e := s.Memory.save(dir); e != nil {
		t.Fatal(e)
	}
	loaded := loadReading(dir)
	resumed, ok := (readerSession{Model: m, Memory: loaded}).navigate(tapped(600, 750))
	if !ok || resumed.Page != 2 || resumed.Index != 1 {
		t.Fatal("restart did not resume article", resumed.Page, resumed.Index)
	}
	resumed, ok = resumed.navigate(tapped(650, 115))
	if !ok || resumed.Index != 2 || resumed.Page != 0 {
		t.Fatal("next article failed")
	}
	newer := m
	newer.Papers = append([]Paper(nil), m.Papers...)
	newer.Papers[1].Title = "Updated title"
	resumed.refresh(newer)
	resumed, ok = resumed.navigate(tapped(100, 115))
	if !ok || resumed.Page != 2 || resumed.Doc.Blocks[0].Text != m.Papers[1].Title {
		t.Fatal("queue changed during reading")
	}
	back, _ := resumed.navigate(tapped(100, 45))
	back = back.open("paper", 1)
	if back.Page != 0 || back.Doc.Blocks[0].Text != "Updated title" {
		t.Fatal("changed content did not reset page")
	}
	first := (readerSession{Model: m}).open("paper", 0)
	if _, ok := first.adjacentPaper(-1); ok {
		t.Fatal("previous before first")
	}
	last := (readerSession{Model: m}).open("paper", 3)
	if _, ok := last.adjacentPaper(1); ok {
		t.Fatal("next after last")
	}
}

func TestCorruptProgressAndBoundedHistory(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	os.WriteFile(filepath.Join(dir, "state", "reading-state.json"), []byte(`{"last":`), 0600)
	if m := loadReading(dir); m.Last != "" || len(m.Positions) != 0 {
		t.Fatal("corrupt progress trusted")
	}
	m := readingMemory{}
	for i := 0; i < 130; i++ {
		d := document{Key: string(rune(1000 + i)), Blocks: []block{{Text: "article", Kind: "sans", Size: 26}}}
		m.remember(d, 3)
	}
	if len(m.Positions) != 100 {
		t.Fatal("history not bounded")
	}
	if _, ok := m.Positions[m.Last]; !ok {
		t.Fatal("last document evicted")
	}
}
