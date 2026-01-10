package config

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestNormalizeRunMode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"SIMPLE", "simple"},
		{"standard", "standard"},
		{"invalid", "standard"},
		{"", "standard"},
	}

	for _, tt := range tests {
		result := NormalizeRunMode(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeRunMode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestLoadDefaultSchedulingConfig(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Gateway.Scheduling.StickySessionMaxWaiting != 3 {
		t.Fatalf("StickySessionMaxWaiting = %d, want 3", cfg.Gateway.Scheduling.StickySessionMaxWaiting)
	}
	if cfg.Gateway.Scheduling.StickySessionWaitTimeout != 45*time.Second {
		t.Fatalf("StickySessionWaitTimeout = %v, want 45s", cfg.Gateway.Scheduling.StickySessionWaitTimeout)
	}
	if cfg.Gateway.Scheduling.FallbackWaitTimeout != 30*time.Second {
		t.Fatalf("FallbackWaitTimeout = %v, want 30s", cfg.Gateway.Scheduling.FallbackWaitTimeout)
	}
	if cfg.Gateway.Scheduling.FallbackMaxWaiting != 100 {
		t.Fatalf("FallbackMaxWaiting = %d, want 100", cfg.Gateway.Scheduling.FallbackMaxWaiting)
	}
	if !cfg.Gateway.Scheduling.LoadBatchEnabled {
		t.Fatalf("LoadBatchEnabled = false, want true")
	}
	if cfg.Gateway.Scheduling.SlotCleanupInterval != 30*time.Second {
		t.Fatalf("SlotCleanupInterval = %v, want 30s", cfg.Gateway.Scheduling.SlotCleanupInterval)
	}
}

func TestLoadSchedulingConfigFromEnv(t *testing.T) {
	viper.Reset()
	t.Setenv("GATEWAY_SCHEDULING_STICKY_SESSION_MAX_WAITING", "5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Gateway.Scheduling.StickySessionMaxWaiting != 5 {
		t.Fatalf("StickySessionMaxWaiting = %d, want 5", cfg.Gateway.Scheduling.StickySessionMaxWaiting)
	}
}

func TestLoadDefaultSecurityToggles(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Security.URLAllowlist.Enabled {
		t.Fatalf("URLAllowlist.Enabled = true, want false")
	}
	if !cfg.Security.URLAllowlist.AllowInsecureHTTP {
		t.Fatalf("URLAllowlist.AllowInsecureHTTP = false, want true")
	}
	if !cfg.Security.URLAllowlist.AllowPrivateHosts {
		t.Fatalf("URLAllowlist.AllowPrivateHosts = false, want true")
	}
	if cfg.Security.ResponseHeaders.Enabled {
		t.Fatalf("ResponseHeaders.Enabled = true, want false")
	}
}

func TestValidateLinuxDoFrontendRedirectURL(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.LinuxDo.Enabled = true
	cfg.LinuxDo.ClientID = "test-client"
	cfg.LinuxDo.ClientSecret = "test-secret"
	cfg.LinuxDo.RedirectURL = "https://example.com/api/v1/auth/oauth/linuxdo/callback"
	cfg.LinuxDo.TokenAuthMethod = "client_secret_post"
	cfg.LinuxDo.UsePKCE = false

	cfg.LinuxDo.FrontendRedirectURL = "javascript:alert(1)"
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for javascript scheme, got nil")
	}
	if !strings.Contains(err.Error(), "linuxdo_connect.frontend_redirect_url") {
		t.Fatalf("Validate() expected frontend_redirect_url error, got: %v", err)
	}
}

func TestValidateLinuxDoPKCERequiredForPublicClient(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.LinuxDo.Enabled = true
	cfg.LinuxDo.ClientID = "test-client"
	cfg.LinuxDo.ClientSecret = ""
	cfg.LinuxDo.RedirectURL = "https://example.com/api/v1/auth/oauth/linuxdo/callback"
	cfg.LinuxDo.FrontendRedirectURL = "/auth/linuxdo/callback"
	cfg.LinuxDo.TokenAuthMethod = "none"
	cfg.LinuxDo.UsePKCE = false

	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error when token_auth_method=none and use_pkce=false, got nil")
	}
	if !strings.Contains(err.Error(), "linuxdo_connect.use_pkce") {
		t.Fatalf("Validate() expected use_pkce error, got: %v", err)
	}
}
