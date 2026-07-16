package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestClaudeUsageResponse_FableWindowDecoding(t *testing.T) {
	raw := `{
  "five_hour": {"utilization": 12.0, "resets_at": "2026-07-03T10:00:00Z"},
  "seven_day": {"utilization": 34.0, "resets_at": "2026-07-08T00:00:00Z"},
  "seven_day_overage_included": {"utilization": 56.0, "resets_at": "2026-07-08T03:00:00Z"}
}`
	var resp ClaudeUsageResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp.SevenDayOverageIncluded.Utilization != 56.0 {
		t.Fatalf("SevenDayOverageIncluded.Utilization = %v, want 56", resp.SevenDayOverageIncluded.Utilization)
	}
	if resp.SevenDayOverageIncluded.ResetsAt != "2026-07-08T03:00:00Z" {
		t.Fatalf("SevenDayOverageIncluded.ResetsAt = %q", resp.SevenDayOverageIncluded.ResetsAt)
	}
}

func TestBuildUsageInfo_SevenDayFable(t *testing.T) {
	now := time.Now()
	resetAt := now.Add(72 * time.Hour).UTC().Truncate(time.Second)
	resp := &ClaudeUsageResponse{
		SevenDayOverageIncluded: ClaudeUsageWindow{
			Utilization: 88,
			ResetsAt:    resetAt.Format(time.RFC3339),
		},
	}
	resp.FiveHour.Utilization = 10

	info := (&AccountUsageService{}).buildUsageInfo(resp, &now)
	if info.SevenDayFable == nil {
		t.Fatal("expected SevenDayFable")
	}
	if info.SevenDayFable.Utilization != 88 {
		t.Fatalf("SevenDayFable.Utilization = %v, want 88", info.SevenDayFable.Utilization)
	}
	if info.SevenDayFable.ResetsAt == nil || !info.SevenDayFable.ResetsAt.Equal(resetAt) {
		t.Fatalf("SevenDayFable.ResetsAt = %v, want %v", info.SevenDayFable.ResetsAt, resetAt)
	}
	if info.SevenDayFable.RemainingSeconds <= 0 {
		t.Fatalf("SevenDayFable.RemainingSeconds = %d, want > 0", info.SevenDayFable.RemainingSeconds)
	}
}

func TestBuildPassiveUsageWindow(t *testing.T) {
	future := time.Now().Add(48 * time.Hour).Unix()

	window := buildPassiveUsageWindow(map[string]any{
		"passive_usage_7d_oi_utilization": 0.87,
		"passive_usage_7d_oi_reset":       float64(future),
	}, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset")
	if window == nil {
		t.Fatal("expected passive usage window")
	}
	if window.Utilization != 87 {
		t.Fatalf("Utilization = %v, want 87", window.Utilization)
	}
	if window.ResetsAt == nil || window.ResetsAt.Unix() != future {
		t.Fatalf("ResetsAt = %v, want unix %d", window.ResetsAt, future)
	}

	if got := buildPassiveUsageWindow(nil, "u", "r"); got != nil {
		t.Fatalf("empty extra returned %#v, want nil", got)
	}

	past := time.Now().Add(-time.Hour).Unix()
	expired := buildPassiveUsageWindow(map[string]any{"u": 0.5, "r": past}, "u", "r")
	if expired == nil || expired.RemainingSeconds != 0 {
		t.Fatalf("expired window = %#v, want remaining seconds 0", expired)
	}
}

func TestSyncActiveToPassive_WritesFableExtras(t *testing.T) {
	repo := &accountUsageCodexProbeRepo{updateExtraCh: make(chan map[string]any, 1)}
	svc := &AccountUsageService{accountRepo: repo}

	resetAt := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	usage := &UsageInfo{
		SevenDayFable: &UsageProgress{
			Utilization: 87,
			ResetsAt:    &resetAt,
		},
	}

	svc.syncActiveToPassive(context.Background(), 1, usage)

	select {
	case updates := <-repo.updateExtraCh:
		if updates["passive_usage_7d_oi_utilization"] != 0.87 {
			t.Fatalf("passive_usage_7d_oi_utilization = %v, want 0.87", updates["passive_usage_7d_oi_utilization"])
		}
		if updates["passive_usage_7d_oi_reset"] != resetAt.Unix() {
			t.Fatalf("passive_usage_7d_oi_reset = %v, want %d", updates["passive_usage_7d_oi_reset"], resetAt.Unix())
		}
	default:
		t.Fatal("expected UpdateExtra call")
	}
}
