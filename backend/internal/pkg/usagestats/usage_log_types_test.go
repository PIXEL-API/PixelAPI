package usagestats

import (
	"testing"
	"time"
)

func TestIsValidModelSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "requested", source: ModelSourceRequested, want: true},
		{name: "upstream", source: ModelSourceUpstream, want: true},
		{name: "mapping", source: ModelSourceMapping, want: true},
		{name: "invalid", source: "foobar", want: false},
		{name: "empty", source: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsValidModelSource(tc.source); got != tc.want {
				t.Fatalf("IsValidModelSource(%q)=%v want %v", tc.source, got, tc.want)
			}
		})
	}
}

func TestNormalizeModelSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "requested", source: ModelSourceRequested, want: ModelSourceRequested},
		{name: "upstream", source: ModelSourceUpstream, want: ModelSourceUpstream},
		{name: "mapping", source: ModelSourceMapping, want: ModelSourceMapping},
		{name: "invalid falls back", source: "foobar", want: ModelSourceRequested},
		{name: "empty falls back", source: "", want: ModelSourceRequested},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeModelSource(tc.source); got != tc.want {
				t.Fatalf("NormalizeModelSource(%q)=%q want %q", tc.source, got, tc.want)
			}
		})
	}
}

func TestResolveAccountStatsDateRange(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.Local)

	t.Run("custom inclusive range", func(t *testing.T) {
		start, end, err := ResolveAccountStatsDateRange("2026-07-01", "2026-07-25", "", now)
		if err != nil {
			t.Fatalf("ResolveAccountStatsDateRange returned error: %v", err)
		}
		if got := start.Format("2006-01-02"); got != "2026-07-01" {
			t.Fatalf("start=%s want 2026-07-01", got)
		}
		if got := end.Format("2006-01-02"); got != "2026-07-26" {
			t.Fatalf("exclusive end=%s want 2026-07-26", got)
		}
	})

	t.Run("default seven days", func(t *testing.T) {
		start, end, err := ResolveAccountStatsDateRange("", "", "", now)
		if err != nil {
			t.Fatalf("ResolveAccountStatsDateRange returned error: %v", err)
		}
		if got := start.Format("2006-01-02"); got != "2026-07-19" {
			t.Fatalf("start=%s want 2026-07-19", got)
		}
		if got := end.Format("2006-01-02"); got != "2026-07-26" {
			t.Fatalf("exclusive end=%s want 2026-07-26", got)
		}
	})

	for _, tc := range []struct {
		name      string
		startDate string
		endDate   string
		days      string
	}{
		{name: "missing end", startDate: "2026-07-01"},
		{name: "reversed", startDate: "2026-07-20", endDate: "2026-07-01"},
		{name: "over 31 calendar days", startDate: "2026-06-24", endDate: "2026-07-25"},
		{name: "future end", startDate: "2026-07-25", endDate: "2026-07-26"},
		{name: "legacy days over limit", days: "32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := ResolveAccountStatsDateRange(tc.startDate, tc.endDate, tc.days, now); err == nil {
				t.Fatal("ResolveAccountStatsDateRange expected error")
			}
		})
	}
}
