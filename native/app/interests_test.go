package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestInterestFeedsStayInsideVersionGuard(t *testing.T) {
	raw := fixture(t)
	var snapshot Dashboard
	json.Unmarshal(raw, &snapshot)
	for _, scenario := range []string{"complete", "partial", "version-change"} {
		t.Run(scenario, func(t *testing.T) {
			versions := 0
			b, e := researchWithInterests(func(u string) ([]byte, error) {
				if u == versionURL {
					versions++
					version := snapshot.Version
					if scenario == "version-change" && versions == 2 {
						version = "changed"
					}
					return json.Marshal(map[string]any{"version": version, "running": false})
				}
				if u == researchURL {
					return raw, nil
				}
				if scenario == "partial" && u == interestURLs[1] {
					return nil, errors.New("HTTP 502")
				}
				p := Paper{ID: u, Title: "GeoAI 城市轨迹研究 " + u, URL: u, ContentType: "paper", Source: "research", Updated: snapshot.Published}
				return json.Marshal(map[string]any{"total": 1, "items": []Paper{p}})
			})
			if scenario == "version-change" {
				if e == nil {
					t.Fatal("accepted changed version")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			m, e := adapt(b)
			if e != nil {
				t.Fatal(e)
			}
			if m.SelectionStale != (scenario == "partial") {
				t.Fatal("interest failure silently treated as complete")
			}
			if len(m.Papers) <= len(snapshot.Papers.Items) {
				t.Fatal("interest candidates absent")
			}
		})
	}
}
