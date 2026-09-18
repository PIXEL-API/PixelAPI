package service

import "testing"

func TestPlatformHasAccountLevel(t *testing.T) {
	tests := []struct {
		platform string
		want     bool
	}{
		{PlatformOpenAI, true},
		{PlatformAnthropic, true},
		{PlatformGemini, true},
		{PlatformAntigravity, true},
		{PlatformGrok, true},
		{PlatformComposite, true},
		{PlatformOpencode, false},
		{PlatformDevin, false},
		{PlatformAPIAggregation, false},
		{PlatformKimi, false},
		{PlatformZhipu, false},
		{PlatformDeepseek, false},
		{PlatformMiniMax, false},
		{PlatformQwen, false},
		{"", true},
		{"unknown-platform", true},
	}
	for _, tt := range tests {
		if got := PlatformHasAccountLevel(tt.platform); got != tt.want {
			t.Errorf("PlatformHasAccountLevel(%q) = %v, want %v", tt.platform, got, tt.want)
		}
	}
}
