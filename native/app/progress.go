package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type readPosition struct {
	Page     int    `json:"page"`
	Revision string `json:"revision"`
	At       string `json:"at"`
}
type readingMemory struct {
	Last      string                  `json:"last"`
	Positions map[string]readPosition `json:"positions"`
}

func loadReading(dir string) readingMemory {
	m := readingMemory{Positions: map[string]readPosition{}}
	p := filepath.Join(dir, "state", "reading-state.json")
	if st, e := os.Stat(p); e != nil || st.Size() > 256*1024 {
		return m
	}
	b, e := os.ReadFile(p)
	if e != nil || json.Unmarshal(b, &m) != nil || m.Positions == nil {
		return readingMemory{Positions: map[string]readPosition{}}
	}
	return m
}

func documentRevision(d document) string {
	// Queue order and network status do not alter pagination of the article.
	b, _ := json.Marshal(struct {
		Layout string
		Blocks []block
	}{"reader-v2", d.Blocks})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (m readingMemory) page(d document) int {
	if pos, ok := m.Positions[d.Key]; ok && pos.Revision == documentRevision(d) && pos.Page >= 0 && pos.Page < 5000 {
		return pos.Page
	}
	return 0
}

func (m *readingMemory) remember(d document, page int) {
	if d.Key == "" {
		return
	}
	if m.Positions == nil {
		m.Positions = map[string]readPosition{}
	}
	m.Last = d.Key
	m.Positions[d.Key] = readPosition{page, documentRevision(d), time.Now().UTC().Format(time.RFC3339Nano)}
	if len(m.Positions) > 100 {
		keys := []string{}
		for k := range m.Positions {
			if k != m.Last {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return m.Positions[keys[i]].At < m.Positions[keys[j]].At })
		for len(m.Positions) > 100 {
			delete(m.Positions, keys[0])
			keys = keys[1:]
		}
	}
}
func (m readingMemory) save(dir string) error {
	b, e := json.Marshal(m)
	if e != nil {
		return e
	}
	return atomic(filepath.Join(dir, "state", "reading-state.json"), b)
}

func (m readingMemory) resumeTarget(model Model) (string, int, bool) {
	if m.Last == "" {
		return "", 0, false
	}
	for _, kind := range []string{"focus", "radars"} {
		if readingDocument(model, kind, 0).Key == m.Last {
			return kind, 0, true
		}
	}
	for i := range model.Radars {
		if readingDocument(model, "radar", i).Key == m.Last {
			return "radar", i, true
		}
	}
	for i, p := range model.Papers {
		if "paper:"+paperKey(p) == m.Last {
			return "paper", i, true
		}
	}
	return "", 0, false
}
func (m readingMemory) resumeExists(model Model) bool { _, _, ok := m.resumeTarget(model); return ok }

func (s readerSession) open(kind string, index int) readerSession {
	if kind == "papers" {
		kind, index = "paper", 0
	}
	if kind == "paper" {
		s.Queue = append([]Paper(nil), s.Model.Papers...)
		s.Index = index
	} else {
		s.Queue = nil
		s.Index = 0
	}
	d := readingDocument(s.Model, kind, index)
	s.Doc = &d
	s.Page = s.Memory.page(d)
	s.Pages = 0
	return s
}

func (s readerSession) adjacentPaper(delta int) (readerSession, bool) {
	next := s.Index + delta
	if s.Doc == nil || s.Doc.Kind != "paper" || next < 0 || next >= len(s.Queue) {
		return s, false
	}
	d := readingDocument(Model{Papers: s.Queue}, "paper", next)
	s.Index = next
	s.Doc = &d
	s.Page = s.Memory.page(d)
	s.Pages = 0
	return s, true
}
