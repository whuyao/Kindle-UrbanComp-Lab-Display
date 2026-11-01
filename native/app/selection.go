package main

import (
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

type rotationState struct {
	Slot     int64            `json:"slot"`
	Featured []string         `json:"featured"`
	Seen     map[string]int64 `json:"seen"`
}

var interests = []struct {
	Name  string
	Words []string
}{
	{"轨迹交通", []string{"轨迹", "交通", "出行", "移动性", "trajectory", "trajectories", "traffic", "mobility", "transportation", "v2x"}},
	{"城市计算", []string{"城市", "建成环境", "街道", "urban", "city", "cities", "built environment", "street"}},
	{"GeoAI", []string{"geoai", "空间智能", "地理人工智能", "基础模型", "大模型", "foundation model", "geospatial", "多模态", "multimodal", "深度学习", "deep learning"}},
	{"遥感", []string{"遥感", "remote sensing", "satellite", "sentinel", "landsat", "地球观测"}},
}

func keywordMatch(text, word string) bool {
	if len(word) == 0 {
		return false
	}
	if word[0] >= 128 {
		return strings.Contains(text, word)
	}
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' }
	for offset := 0; offset < len(text); {
		at := strings.Index(text[offset:], word)
		if at < 0 {
			return false
		}
		at += offset
		end := at + len(word)
		if (at == 0 || !letter(text[at-1])) && (end == len(text) || !letter(text[end])) {
			return true
		}
		offset = at + 1
	}
	return false
}

// These are transparent local topic matches, not a claim about paper quality.
func paperTopic(p Paper) (string, int) {
	title := strings.ToLower(p.Title + " " + p.Translated)
	body := strings.ToLower(p.Summary + " " + strings.Join(p.Tags, " ") + " " + strings.Join(p.Keywords, " "))
	best, topic := 0, "研究"
	for _, interest := range interests {
		score := 0
		for _, word := range interest.Words {
			if keywordMatch(title, word) {
				score += 12
			} else if keywordMatch(body, word) {
				score += 3
			}
		}
		if score > 24 {
			score = 24
		}
		if score > best {
			topic, best = interest.Name, score
		}
	}
	return topic, best
}

func paperKey(p Paper) string {
	if p.DOI != "" {
		return "doi:" + strings.ToLower(strings.TrimPrefix(strings.TrimSpace(p.DOI), "https://doi.org/"))
	}
	u, e := url.Parse(p.URL)
	if e == nil && u.Hostname() != "" {
		path := strings.ToLower(u.Path)
		if at := strings.Index(path, "/10."); at >= 0 && (strings.Contains(path, "/doi/") || strings.EqualFold(u.Hostname(), "doi.org")) {
			return "doi:" + strings.TrimSuffix(path[at+1:], "/")
		}
		u.Scheme = "https"
		u.Host = strings.ToLower(u.Host)
		u.Fragment = ""
		q := u.Query()
		for k := range q {
			if strings.HasPrefix(strings.ToLower(k), "utm_") || k == "af" || k == "fbclid" {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
		return u.String()
	}
	return p.ID
}

func titleKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

func paperScore(p Paper, now time.Time) int {
	topic, score := paperTopic(p)
	if topic == "GeoAI" || topic == "城市计算" || topic == "轨迹交通" {
		score += 8
	}
	if t, e := time.Parse(time.RFC3339, p.Created); e == nil && !t.After(now.Add(5*time.Minute)) {
		switch age := now.Sub(t); {
		case age <= 48*time.Hour:
			score += 12
		case age <= 7*24*time.Hour:
			score += 6
		case age <= 14*24*time.Hour:
			score += 2
		}
	}
	summary, ready := readableAbstract(p)
	if !ready {
		score -= 16
	}
	if ready && len([]rune(summary)) >= 80 {
		score += 4
	}
	return score
}

// Some publisher feeds expose only a publication date and author list in the
// summary field. Preserve that field in the cache, but do not call it an abstract.
func readableAbstract(p Paper) (string, bool) {
	s := clean(p.Summary)
	if s == "" || strings.HasPrefix(s, "摘要尚未") || strings.HasPrefix(s, "Volume ") {
		return "", false
	}
	if strings.HasPrefix(s, "出版日期") && strings.Contains(s, "来源") && strings.Contains(s, "作者") {
		if at := strings.Index(s, "摘要："); at >= 0 {
			s = strings.TrimSpace(s[at+len("摘要："):])
		} else {
			return "", false
		}
	}
	return s, s != ""
}

func curate(m, old Model, now time.Time) Model {
	prior := map[string]Paper{}
	for _, p := range old.Papers {
		prior[paperKey(p)] = p
	}
	pool := []Paper{}
	seen, titles := map[string]bool{}, map[string]bool{}
	add := func(p Paper, retained bool) {
		key, title := paperKey(p), titleKey(p.Title)
		if key == "" || title == "" || seen[key] || titles[title] || !validURL(p.URL) {
			return
		}
		if prev, ok := prior[key]; ok && p.CachedAt == "" {
			p.CachedAt = prev.CachedAt
		}
		if retained {
			t, e := time.Parse(time.RFC3339, p.CachedAt)
			if e != nil || now.Sub(t) > 14*24*time.Hour {
				return
			}
		}
		if p.CachedAt == "" {
			p.CachedAt = now.UTC().Format(time.RFC3339)
		}
		p.Topic, _ = paperTopic(p)
		pool = append(pool, p)
		seen[key], titles[title] = true, true
	}
	for _, p := range m.Papers {
		add(p, false)
	}
	for _, p := range old.Papers {
		add(p, true)
	}
	sort.SliceStable(pool, func(i, j int) bool {
		a, b := paperScore(pool[i], now), paperScore(pool[j], now)
		if a != b {
			return a > b
		}
		ta, _ := time.Parse(time.RFC3339, pool[i].Created)
		tb, _ := time.Parse(time.RFC3339, pool[j].Created)
		if !ta.Equal(tb) {
			return ta.After(tb)
		}
		return paperKey(pool[i]) < paperKey(pool[j])
	})
	if len(pool) > 24 {
		pool = pool[:24]
	}
	m.Papers = pool
	slot := now.Unix() / 1800
	m.Rotation = rotationState{Slot: slot, Seen: map[string]int64{}}
	present := map[string]bool{}
	for _, p := range pool {
		k := paperKey(p)
		present[k] = true
		m.Rotation.Seen[k] = old.Rotation.Seen[k]
	}
	for _, k := range old.Rotation.Featured {
		if present[k] {
			m.Rotation.Featured = append(m.Rotation.Featured, k)
		}
	}
	want := 2
	if len(pool) < want {
		want = len(pool)
	}
	if old.Rotation.Slot == slot && len(m.Rotation.Featured) == want {
		return m
	}
	m.Rotation.Featured = nil
	order := append([]Paper(nil), pool...)
	sort.SliceStable(order, func(i, j int) bool { return m.Rotation.Seen[paperKey(order[i])] < m.Rotation.Seen[paperKey(order[j])] })
	if len(order) > 2 {
		for i := 2; i < len(order); i++ {
			if order[i].Topic != order[0].Topic && m.Rotation.Seen[paperKey(order[i])] == m.Rotation.Seen[paperKey(order[1])] {
				order[1], order[i] = order[i], order[1]
				break
			}
		}
	}
	for _, p := range order {
		if len(m.Rotation.Featured) == want {
			break
		}
		k := paperKey(p)
		m.Rotation.Featured = append(m.Rotation.Featured, k)
		m.Rotation.Seen[k] = slot
	}
	return m
}

func featuredIndices(m Model) []int {
	indices := []int{}
	used := map[int]bool{}
	for _, k := range m.Rotation.Featured {
		for i, p := range m.Papers {
			if paperKey(p) == k && !used[i] {
				indices = append(indices, i)
				used[i] = true
				break
			}
		}
		if len(indices) == 2 {
			return indices
		}
	}
	for i := range m.Papers {
		if !used[i] {
			indices = append(indices, i)
		}
		if len(indices) == 2 {
			break
		}
	}
	return indices
}
