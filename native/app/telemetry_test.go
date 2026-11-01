package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBatteryDistinguishesExternalPowerAndUnknown(t *testing.T) {
	var m metric
	sampleBattery(&m, func(name string) (string, error) {
		return map[string]string{"battLevel": "100", "isCharging": "0", "battStateInfo": `[{"usb":true}]`}[name], nil
	})
	if m.Battery == nil || *m.Battery != 100 || m.Charging == nil || *m.Charging || m.USB == nil || !*m.USB {
		t.Fatal("external power reported as discharge")
	}
	m = metric{}
	sampleBattery(&m, func(string) (string, error) { return "", errors.New("missing") })
	if m.Battery != nil || m.Charging != nil || m.USB != nil {
		t.Fatal("missing sensor values became zero or false")
	}
	rss, hwm := procMemory("VmRSS:\t1234 kB\nVmHWM:\t4567 kB\n")
	if rss != 1234 || hwm != 4567 {
		t.Fatal(rss, hwm)
	}
}

func TestPowerInfoDirectObjectAndListingWrapper(t *testing.T) {
	for _, tc := range []struct {
		input, format string
		known, value  bool
	}{
		{`{"battInfo":[{"type":"PRIMARY","cap":100,"charging":false}],"state":"STANDALONE","aggr_batt":100,"usb":false}`, "object", true, false},
		{`{"usb":true}`, "object", true, true},
		{`[{"usb":false}]`, "array", true, false},
		{`null`, "", false, false}, {`{}`, "", false, false}, {`{"usb":null}`, "", false, false},
		{`{"usb":0}`, "", false, false}, {`[{"usb":false},{"usb":true}]`, "", false, false}, {`truncated`, "", false, false},
	} {
		value, format := parsePowerInfo(tc.input)
		if (value != nil) != tc.known || format != tc.format || (value != nil && *value != tc.value) {
			t.Fatal(tc.input, value, format)
		}
	}
	var m metric
	sampleBattery(&m, func(name string) (string, error) {
		return map[string]string{"battLevel": "99", "isCharging": "0", "battStateInfo": `{"usb":false}`}[name], nil
	})
	if m.USB == nil || *m.USB || m.PowerFormat != "object" || m.PowerError != "" {
		t.Fatal("direct-read sample lost USB field", m)
	}
}

func TestTelemetryRejectsOldReceiptAndTracksFailures(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "state"), 0700)
	reporter := newTelemetry(dir, false)
	start := time.Now()
	path := filepath.Join(dir, "state", "receipt.json")
	write := func(at time.Time, stale, weather bool) {
		b, _ := json.Marshal(map[string]any{"attempt_started_at": at.Format(time.RFC3339Nano), "research_stale": stale, "weather_ok": weather, "interests_ok": true})
		os.WriteFile(path, b, 0600)
	}
	write(start.Add(-time.Minute), false, true)
	reporter.refresh(context.Background(), start, errors.New("network failure before commit"))
	write(time.Now(), false, true)
	reporter.refresh(context.Background(), start, nil)
	write(time.Now(), false, false)
	reporter.refresh(context.Background(), start, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reporter.refresh(ctx, start, ctx.Err())
	b, _ := os.ReadFile(filepath.Join(dir, "state", "metrics.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 4 {
		t.Fatal(len(lines))
	}
	rows := make([]metric, 4)
	for i, line := range lines {
		if e := json.Unmarshal([]byte(line), &rows[i]); e != nil {
			t.Fatal(e)
		}
	}
	if *rows[0].UpdateOK || rows[0].ResearchOK != nil || !*rows[1].UpdateOK || *rows[2].UpdateOK || rows[3].UpdateOK != nil || rows[3].Outcome != "cancelled" {
		t.Fatal("incorrect success accounting", string(b))
	}
	for _, r := range rows {
		if r.Device {
			t.Fatal("fixture tagged as hardware evidence")
		}
	}
}
