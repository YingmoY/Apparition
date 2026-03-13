package server

import (
	"testing"
	"time"
)

func TestCalcNextRunAtDaily(t *testing.T) {
	now := time.Date(2026, 3, 13, 8, 0, 0, 0, time.Local)
	next, err := calcNextRunAt("daily", "08:30", now)
	if err != nil {
		t.Fatalf("calc daily next run failed: %v", err)
	}
	if next.Hour() != 8 || next.Minute() != 30 {
		t.Fatalf("unexpected next time: %v", next)
	}
	if !next.After(now) {
		t.Fatalf("next run should be after now")
	}
}

func TestCalcNextRunAtInterval(t *testing.T) {
	now := time.Date(2026, 3, 13, 8, 0, 0, 0, time.Local)
	next, err := calcNextRunAt("fixed_interval", "60", now)
	if err != nil {
		t.Fatalf("calc interval next run failed: %v", err)
	}
	if next.Sub(now) != 60*time.Second {
		t.Fatalf("expected 60s interval, got %v", next.Sub(now))
	}
}

func TestParseClockinJobPath(t *testing.T) {
	id, action, ok := parseClockinJobPath("/api/v1/clockin/jobs/12/run")
	if !ok || id != 12 || action != "run" {
		t.Fatalf("unexpected parse result: id=%d action=%s ok=%v", id, action, ok)
	}
}
