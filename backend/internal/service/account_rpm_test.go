package service

import "testing"

func TestGetBaseRPM(t *testing.T) {
	tests := []struct {
		name     string
		extra    map[string]any
		expected int
	}{
		{"nil extra", nil, 0},
		{"no key", map[string]any{}, 0},
		{"zero", map[string]any{"base_rpm": 0}, 0},
		{"int value", map[string]any{"base_rpm": 15}, 15},
		{"float value", map[string]any{"base_rpm": 15.0}, 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{Extra: tt.extra}
			if got := a.GetBaseRPM(); got != tt.expected {
				t.Errorf("GetBaseRPM() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestGetRPMStrategy(t *testing.T) {
	tests := []struct {
		name     string
		extra    map[string]any
		expected string
	}{
		{"nil extra", nil, "tiered"},
		{"no key", map[string]any{}, "tiered"},
		{"tiered", map[string]any{"rpm_strategy": "tiered"}, "tiered"},
		{"sticky_exempt", map[string]any{"rpm_strategy": "sticky_exempt"}, "sticky_exempt"},
		{"invalid", map[string]any{"rpm_strategy": "foobar"}, "tiered"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{Extra: tt.extra}
			if got := a.GetRPMStrategy(); got != tt.expected {
				t.Errorf("GetRPMStrategy() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestCheckRPMSchedulability(t *testing.T) {
	tests := []struct {
		name       string
		extra      map[string]any
		currentRPM int
		expected   WindowCostSchedulability
	}{
		{"disabled", map[string]any{}, 100, WindowCostSchedulable},
		{"green zone", map[string]any{"base_rpm": 15}, 10, WindowCostSchedulable},
		{"yellow zone tiered", map[string]any{"base_rpm": 15}, 15, WindowCostStickyOnly},
		{"red zone tiered", map[string]any{"base_rpm": 15}, 18, WindowCostNotSchedulable},
		{"sticky_exempt at limit", map[string]any{"base_rpm": 15, "rpm_strategy": "sticky_exempt"}, 15, WindowCostStickyOnly},
		{"sticky_exempt over limit", map[string]any{"base_rpm": 15, "rpm_strategy": "sticky_exempt"}, 100, WindowCostStickyOnly},
		{"custom buffer", map[string]any{"base_rpm": 10, "rpm_sticky_buffer": 5}, 14, WindowCostStickyOnly},
		{"custom buffer red", map[string]any{"base_rpm": 10, "rpm_sticky_buffer": 5}, 15, WindowCostNotSchedulable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &Account{Extra: tt.extra}
			if got := a.CheckRPMSchedulability(tt.currentRPM); got != tt.expected {
				t.Errorf("CheckRPMSchedulability(%d) = %d, want %d", tt.currentRPM, got, tt.expected)
			}
		})
	}
}
