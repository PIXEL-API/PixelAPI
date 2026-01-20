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
	if cfg.Gateway.Scheduling.StickySessionWaitTimeout != 120*time.Second {
		t.Fatalf("StickySessionWaitTimeout = %v, want 120s", cfg.Gateway.Scheduling.StickySessionWaitTimeout)
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

func TestLoadDefaultDashboardCacheConfig(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.Dashboard.Enabled {
		t.Fatalf("Dashboard.Enabled = false, want true")
	}
	if cfg.Dashboard.KeyPrefix != "sub2api:" {
		t.Fatalf("Dashboard.KeyPrefix = %q, want %q", cfg.Dashboard.KeyPrefix, "sub2api:")
	}
	if cfg.Dashboard.StatsFreshTTLSeconds != 15 {
		t.Fatalf("Dashboard.StatsFreshTTLSeconds = %d, want 15", cfg.Dashboard.StatsFreshTTLSeconds)
	}
	if cfg.Dashboard.StatsTTLSeconds != 30 {
		t.Fatalf("Dashboard.StatsTTLSeconds = %d, want 30", cfg.Dashboard.StatsTTLSeconds)
	}
	if cfg.Dashboard.StatsRefreshTimeoutSeconds != 30 {
		t.Fatalf("Dashboard.StatsRefreshTimeoutSeconds = %d, want 30", cfg.Dashboard.StatsRefreshTimeoutSeconds)
	}
}

func TestValidateDashboardCacheConfigEnabled(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.Dashboard.Enabled = true
	cfg.Dashboard.StatsFreshTTLSeconds = 10
	cfg.Dashboard.StatsTTLSeconds = 5
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for stats_fresh_ttl_seconds > stats_ttl_seconds, got nil")
	}
	if !strings.Contains(err.Error(), "dashboard_cache.stats_fresh_ttl_seconds") {
		t.Fatalf("Validate() expected stats_fresh_ttl_seconds error, got: %v", err)
	}
}

func TestValidateDashboardCacheConfigDisabled(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.Dashboard.Enabled = false
	cfg.Dashboard.StatsTTLSeconds = -1
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for negative stats_ttl_seconds, got nil")
	}
	if !strings.Contains(err.Error(), "dashboard_cache.stats_ttl_seconds") {
		t.Fatalf("Validate() expected stats_ttl_seconds error, got: %v", err)
	}
}

func TestLoadDefaultDashboardAggregationConfig(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.DashboardAgg.Enabled {
		t.Fatalf("DashboardAgg.Enabled = false, want true")
	}
	if cfg.DashboardAgg.IntervalSeconds != 60 {
		t.Fatalf("DashboardAgg.IntervalSeconds = %d, want 60", cfg.DashboardAgg.IntervalSeconds)
	}
	if cfg.DashboardAgg.LookbackSeconds != 120 {
		t.Fatalf("DashboardAgg.LookbackSeconds = %d, want 120", cfg.DashboardAgg.LookbackSeconds)
	}
	if cfg.DashboardAgg.BackfillEnabled {
		t.Fatalf("DashboardAgg.BackfillEnabled = true, want false")
	}
	if cfg.DashboardAgg.BackfillMaxDays != 31 {
		t.Fatalf("DashboardAgg.BackfillMaxDays = %d, want 31", cfg.DashboardAgg.BackfillMaxDays)
	}
	if cfg.DashboardAgg.Retention.UsageLogsDays != 90 {
		t.Fatalf("DashboardAgg.Retention.UsageLogsDays = %d, want 90", cfg.DashboardAgg.Retention.UsageLogsDays)
	}
	if cfg.DashboardAgg.Retention.HourlyDays != 180 {
		t.Fatalf("DashboardAgg.Retention.HourlyDays = %d, want 180", cfg.DashboardAgg.Retention.HourlyDays)
	}
	if cfg.DashboardAgg.Retention.DailyDays != 730 {
		t.Fatalf("DashboardAgg.Retention.DailyDays = %d, want 730", cfg.DashboardAgg.Retention.DailyDays)
	}
	if cfg.DashboardAgg.RecomputeDays != 2 {
		t.Fatalf("DashboardAgg.RecomputeDays = %d, want 2", cfg.DashboardAgg.RecomputeDays)
	}
}

func TestValidateDashboardAggregationConfigDisabled(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.DashboardAgg.Enabled = false
	cfg.DashboardAgg.IntervalSeconds = -1
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for negative dashboard_aggregation.interval_seconds, got nil")
	}
	if !strings.Contains(err.Error(), "dashboard_aggregation.interval_seconds") {
		t.Fatalf("Validate() expected interval_seconds error, got: %v", err)
	}
}

func TestValidateDashboardAggregationBackfillMaxDays(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.DashboardAgg.BackfillEnabled = true
	cfg.DashboardAgg.BackfillMaxDays = 0
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for dashboard_aggregation.backfill_max_days, got nil")
	}
	if !strings.Contains(err.Error(), "dashboard_aggregation.backfill_max_days") {
		t.Fatalf("Validate() expected backfill_max_days error, got: %v", err)
	}
}

func TestLoadDefaultUsageCleanupConfig(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if !cfg.UsageCleanup.Enabled {
		t.Fatalf("UsageCleanup.Enabled = false, want true")
	}
	if cfg.UsageCleanup.MaxRangeDays != 31 {
		t.Fatalf("UsageCleanup.MaxRangeDays = %d, want 31", cfg.UsageCleanup.MaxRangeDays)
	}
	if cfg.UsageCleanup.BatchSize != 5000 {
		t.Fatalf("UsageCleanup.BatchSize = %d, want 5000", cfg.UsageCleanup.BatchSize)
	}
	if cfg.UsageCleanup.WorkerIntervalSeconds != 10 {
		t.Fatalf("UsageCleanup.WorkerIntervalSeconds = %d, want 10", cfg.UsageCleanup.WorkerIntervalSeconds)
	}
	if cfg.UsageCleanup.TaskTimeoutSeconds != 1800 {
		t.Fatalf("UsageCleanup.TaskTimeoutSeconds = %d, want 1800", cfg.UsageCleanup.TaskTimeoutSeconds)
	}
}

func TestValidateUsageCleanupConfigEnabled(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.UsageCleanup.Enabled = true
	cfg.UsageCleanup.MaxRangeDays = 0
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for usage_cleanup.max_range_days, got nil")
	}
	if !strings.Contains(err.Error(), "usage_cleanup.max_range_days") {
		t.Fatalf("Validate() expected max_range_days error, got: %v", err)
	}
}

func TestValidateUsageCleanupConfigDisabled(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.UsageCleanup.Enabled = false
	cfg.UsageCleanup.BatchSize = -1
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for usage_cleanup.batch_size, got nil")
	}
	if !strings.Contains(err.Error(), "usage_cleanup.batch_size") {
		t.Fatalf("Validate() expected batch_size error, got: %v", err)
	}
}

func TestConfigAddressHelpers(t *testing.T) {
	server := ServerConfig{Host: "127.0.0.1", Port: 9000}
	if server.Address() != "127.0.0.1:9000" {
		t.Fatalf("ServerConfig.Address() = %q", server.Address())
	}

	dbCfg := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "",
		DBName:   "sub2api",
		SSLMode:  "disable",
	}
	if !strings.Contains(dbCfg.DSN(), "password=") {
	} else {
		t.Fatalf("DatabaseConfig.DSN() should not include password when empty")
	}

	dbCfg.Password = "secret"
	if !strings.Contains(dbCfg.DSN(), "password=secret") {
		t.Fatalf("DatabaseConfig.DSN() missing password")
	}

	dbCfg.Password = ""
	if strings.Contains(dbCfg.DSNWithTimezone("UTC"), "password=") {
		t.Fatalf("DatabaseConfig.DSNWithTimezone() should omit password when empty")
	}

	if !strings.Contains(dbCfg.DSNWithTimezone(""), "TimeZone=Asia/Shanghai") {
		t.Fatalf("DatabaseConfig.DSNWithTimezone() should use default timezone")
	}
	if !strings.Contains(dbCfg.DSNWithTimezone("UTC"), "TimeZone=UTC") {
		t.Fatalf("DatabaseConfig.DSNWithTimezone() should use provided timezone")
	}

	redis := RedisConfig{Host: "redis", Port: 6379}
	if redis.Address() != "redis:6379" {
		t.Fatalf("RedisConfig.Address() = %q", redis.Address())
	}
}

func TestNormalizeStringSlice(t *testing.T) {
	values := normalizeStringSlice([]string{" a ", "", "b", "   ", "c"})
	if len(values) != 3 || values[0] != "a" || values[1] != "b" || values[2] != "c" {
		t.Fatalf("normalizeStringSlice() unexpected result: %#v", values)
	}
	if normalizeStringSlice(nil) != nil {
		t.Fatalf("normalizeStringSlice(nil) expected nil slice")
	}
}

func TestGetServerAddressFromEnv(t *testing.T) {
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "9090")

	address := GetServerAddress()
	if address != "127.0.0.1:9090" {
		t.Fatalf("GetServerAddress() = %q", address)
	}
}

func TestValidateAbsoluteHTTPURL(t *testing.T) {
	if err := ValidateAbsoluteHTTPURL("https://example.com/path"); err != nil {
		t.Fatalf("ValidateAbsoluteHTTPURL valid url error: %v", err)
	}
	if err := ValidateAbsoluteHTTPURL(""); err == nil {
		t.Fatalf("ValidateAbsoluteHTTPURL should reject empty url")
	}
	if err := ValidateAbsoluteHTTPURL("/relative"); err == nil {
		t.Fatalf("ValidateAbsoluteHTTPURL should reject relative url")
	}
	if err := ValidateAbsoluteHTTPURL("ftp://example.com"); err == nil {
		t.Fatalf("ValidateAbsoluteHTTPURL should reject ftp scheme")
	}
	if err := ValidateAbsoluteHTTPURL("https://example.com/#frag"); err == nil {
		t.Fatalf("ValidateAbsoluteHTTPURL should reject fragment")
	}
}

func TestValidateFrontendRedirectURL(t *testing.T) {
	if err := ValidateFrontendRedirectURL("/auth/callback"); err != nil {
		t.Fatalf("ValidateFrontendRedirectURL relative error: %v", err)
	}
	if err := ValidateFrontendRedirectURL("https://example.com/auth"); err != nil {
		t.Fatalf("ValidateFrontendRedirectURL absolute error: %v", err)
	}
	if err := ValidateFrontendRedirectURL("example.com/path"); err == nil {
		t.Fatalf("ValidateFrontendRedirectURL should reject non-absolute url")
	}
	if err := ValidateFrontendRedirectURL("//evil.com"); err == nil {
		t.Fatalf("ValidateFrontendRedirectURL should reject // prefix")
	}
	if err := ValidateFrontendRedirectURL("javascript:alert(1)"); err == nil {
		t.Fatalf("ValidateFrontendRedirectURL should reject javascript scheme")
	}
}

func TestWarnIfInsecureURL(t *testing.T) {
	warnIfInsecureURL("test", "http://example.com")
	warnIfInsecureURL("test", "bad://url")
}

func TestGenerateJWTSecretDefaultLength(t *testing.T) {
	secret, err := generateJWTSecret(0)
	if err != nil {
		t.Fatalf("generateJWTSecret error: %v", err)
	}
	if len(secret) == 0 {
		t.Fatalf("generateJWTSecret returned empty string")
	}
}

func TestValidateOpsCleanupScheduleRequired(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Ops.Cleanup.Enabled = true
	cfg.Ops.Cleanup.Schedule = ""
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for ops.cleanup.schedule")
	}
	if !strings.Contains(err.Error(), "ops.cleanup.schedule") {
		t.Fatalf("Validate() expected ops.cleanup.schedule error, got: %v", err)
	}
}

func TestValidateConcurrencyPingInterval(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Concurrency.PingInterval = 3
	err = cfg.Validate()
	if err == nil {
		t.Fatalf("Validate() expected error for concurrency.ping_interval")
	}
	if !strings.Contains(err.Error(), "concurrency.ping_interval") {
		t.Fatalf("Validate() expected concurrency.ping_interval error, got: %v", err)
	}
}

func TestProvideConfig(t *testing.T) {
	viper.Reset()
	if _, err := ProvideConfig(); err != nil {
		t.Fatalf("ProvideConfig() error: %v", err)
	}
}

func TestValidateConfigWithLinuxDoEnabled(t *testing.T) {
	viper.Reset()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	cfg.Security.CSP.Enabled = true
	cfg.Security.CSP.Policy = "default-src 'self'"

	cfg.LinuxDo.Enabled = true
	cfg.LinuxDo.ClientID = "client"
	cfg.LinuxDo.ClientSecret = "secret"
	cfg.LinuxDo.AuthorizeURL = "https://example.com/oauth2/authorize"
	cfg.LinuxDo.TokenURL = "https://example.com/oauth2/token"
	cfg.LinuxDo.UserInfoURL = "https://example.com/oauth2/userinfo"
	cfg.LinuxDo.RedirectURL = "https://example.com/api/v1/auth/oauth/linuxdo/callback"
	cfg.LinuxDo.FrontendRedirectURL = "/auth/linuxdo/callback"
	cfg.LinuxDo.TokenAuthMethod = "client_secret_post"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
}

func TestValidateJWTSecretStrength(t *testing.T) {
	if !isWeakJWTSecret("change-me-in-production") {
		t.Fatalf("isWeakJWTSecret should detect weak secret")
	}
	if isWeakJWTSecret("StrongSecretValue") {
		t.Fatalf("isWeakJWTSecret should accept strong secret")
	}
}

func TestGenerateJWTSecretWithLength(t *testing.T) {
	secret, err := generateJWTSecret(16)
	if err != nil {
		t.Fatalf("generateJWTSecret error: %v", err)
	}
	if len(secret) == 0 {
		t.Fatalf("generateJWTSecret returned empty string")
	}
}

func TestValidateAbsoluteHTTPURLMissingHost(t *testing.T) {
	if err := ValidateAbsoluteHTTPURL("https://"); err == nil {
		t.Fatalf("ValidateAbsoluteHTTPURL should reject missing host")
	}
}

func TestValidateFrontendRedirectURLInvalidChars(t *testing.T) {
	if err := ValidateFrontendRedirectURL("/auth/\ncallback"); err == nil {
		t.Fatalf("ValidateFrontendRedirectURL should reject invalid chars")
	}
	if err := ValidateFrontendRedirectURL("http://"); err == nil {
		t.Fatalf("ValidateFrontendRedirectURL should reject missing host")
	}
	if err := ValidateFrontendRedirectURL("mailto:user@example.com"); err == nil {
		t.Fatalf("ValidateFrontendRedirectURL should reject mailto")
	}
}

func TestWarnIfInsecureURLHTTPS(t *testing.T) {
	warnIfInsecureURL("secure", "https://example.com")
}

func TestValidateConfigErrors(t *testing.T) {
	buildValid := func(t *testing.T) *Config {
		t.Helper()
		viper.Reset()
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}
		return cfg
	}

	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "jwt expire hour positive",
			mutate:  func(c *Config) { c.JWT.ExpireHour = 0 },
			wantErr: "jwt.expire_hour must be positive",
		},
		{
			name:    "jwt expire hour max",
			mutate:  func(c *Config) { c.JWT.ExpireHour = 200 },
			wantErr: "jwt.expire_hour must be <= 168",
		},
		{
			name:    "csp policy required",
			mutate:  func(c *Config) { c.Security.CSP.Enabled = true; c.Security.CSP.Policy = "" },
			wantErr: "security.csp.policy",
		},
		{
			name: "linuxdo client id required",
			mutate: func(c *Config) {
				c.LinuxDo.Enabled = true
				c.LinuxDo.ClientID = ""
			},
			wantErr: "linuxdo_connect.client_id",
		},
		{
			name: "linuxdo token auth method",
			mutate: func(c *Config) {
				c.LinuxDo.Enabled = true
				c.LinuxDo.ClientID = "client"
				c.LinuxDo.ClientSecret = "secret"
				c.LinuxDo.AuthorizeURL = "https://example.com/authorize"
				c.LinuxDo.TokenURL = "https://example.com/token"
				c.LinuxDo.UserInfoURL = "https://example.com/userinfo"
				c.LinuxDo.RedirectURL = "https://example.com/callback"
				c.LinuxDo.FrontendRedirectURL = "/auth/callback"
				c.LinuxDo.TokenAuthMethod = "invalid"
			},
			wantErr: "linuxdo_connect.token_auth_method",
		},
		{
			name:    "billing circuit breaker threshold",
			mutate:  func(c *Config) { c.Billing.CircuitBreaker.FailureThreshold = 0 },
			wantErr: "billing.circuit_breaker.failure_threshold",
		},
		{
			name:    "billing circuit breaker reset",
			mutate:  func(c *Config) { c.Billing.CircuitBreaker.ResetTimeoutSeconds = 0 },
			wantErr: "billing.circuit_breaker.reset_timeout_seconds",
		},
		{
			name:    "billing circuit breaker half open",
			mutate:  func(c *Config) { c.Billing.CircuitBreaker.HalfOpenRequests = 0 },
			wantErr: "billing.circuit_breaker.half_open_requests",
		},
		{
			name:    "database max open conns",
			mutate:  func(c *Config) { c.Database.MaxOpenConns = 0 },
			wantErr: "database.max_open_conns",
		},
		{
			name:    "database max lifetime",
			mutate:  func(c *Config) { c.Database.ConnMaxLifetimeMinutes = -1 },
			wantErr: "database.conn_max_lifetime_minutes",
		},
		{
			name:    "database idle exceeds open",
			mutate:  func(c *Config) { c.Database.MaxIdleConns = c.Database.MaxOpenConns + 1 },
			wantErr: "database.max_idle_conns cannot exceed",
		},
		{
			name:    "redis dial timeout",
			mutate:  func(c *Config) { c.Redis.DialTimeoutSeconds = 0 },
			wantErr: "redis.dial_timeout_seconds",
		},
		{
			name:    "redis read timeout",
			mutate:  func(c *Config) { c.Redis.ReadTimeoutSeconds = 0 },
			wantErr: "redis.read_timeout_seconds",
		},
		{
			name:    "redis write timeout",
			mutate:  func(c *Config) { c.Redis.WriteTimeoutSeconds = 0 },
			wantErr: "redis.write_timeout_seconds",
		},
		{
			name:    "redis pool size",
			mutate:  func(c *Config) { c.Redis.PoolSize = 0 },
			wantErr: "redis.pool_size",
		},
		{
			name:    "redis idle exceeds pool",
			mutate:  func(c *Config) { c.Redis.MinIdleConns = c.Redis.PoolSize + 1 },
			wantErr: "redis.min_idle_conns cannot exceed",
		},
		{
			name:    "dashboard cache disabled negative",
			mutate:  func(c *Config) { c.Dashboard.Enabled = false; c.Dashboard.StatsTTLSeconds = -1 },
			wantErr: "dashboard_cache.stats_ttl_seconds",
		},
		{
			name:    "dashboard cache fresh ttl positive",
			mutate:  func(c *Config) { c.Dashboard.Enabled = true; c.Dashboard.StatsFreshTTLSeconds = 0 },
			wantErr: "dashboard_cache.stats_fresh_ttl_seconds",
		},
		{
			name:    "dashboard aggregation enabled interval",
			mutate:  func(c *Config) { c.DashboardAgg.Enabled = true; c.DashboardAgg.IntervalSeconds = 0 },
			wantErr: "dashboard_aggregation.interval_seconds",
		},
		{
			name: "dashboard aggregation backfill positive",
			mutate: func(c *Config) {
				c.DashboardAgg.Enabled = true
				c.DashboardAgg.BackfillEnabled = true
				c.DashboardAgg.BackfillMaxDays = 0
			},
			wantErr: "dashboard_aggregation.backfill_max_days",
		},
		{
			name:    "dashboard aggregation retention",
			mutate:  func(c *Config) { c.DashboardAgg.Enabled = true; c.DashboardAgg.Retention.UsageLogsDays = 0 },
			wantErr: "dashboard_aggregation.retention.usage_logs_days",
		},
		{
			name:    "dashboard aggregation disabled interval",
			mutate:  func(c *Config) { c.DashboardAgg.Enabled = false; c.DashboardAgg.IntervalSeconds = -1 },
			wantErr: "dashboard_aggregation.interval_seconds",
		},
		{
			name:    "usage cleanup max range",
			mutate:  func(c *Config) { c.UsageCleanup.Enabled = true; c.UsageCleanup.MaxRangeDays = 0 },
			wantErr: "usage_cleanup.max_range_days",
		},
		{
			name:    "usage cleanup worker interval",
			mutate:  func(c *Config) { c.UsageCleanup.Enabled = true; c.UsageCleanup.WorkerIntervalSeconds = 0 },
			wantErr: "usage_cleanup.worker_interval_seconds",
		},
		{
			name:    "usage cleanup batch size",
			mutate:  func(c *Config) { c.UsageCleanup.Enabled = true; c.UsageCleanup.BatchSize = 0 },
			wantErr: "usage_cleanup.batch_size",
		},
		{
			name:    "usage cleanup disabled negative",
			mutate:  func(c *Config) { c.UsageCleanup.Enabled = false; c.UsageCleanup.BatchSize = -1 },
			wantErr: "usage_cleanup.batch_size",
		},
		{
			name:    "gateway max body size",
			mutate:  func(c *Config) { c.Gateway.MaxBodySize = 0 },
			wantErr: "gateway.max_body_size",
		},
		{
			name:    "gateway max idle conns",
			mutate:  func(c *Config) { c.Gateway.MaxIdleConns = 0 },
			wantErr: "gateway.max_idle_conns",
		},
		{
			name:    "gateway max idle conns per host",
			mutate:  func(c *Config) { c.Gateway.MaxIdleConnsPerHost = 0 },
			wantErr: "gateway.max_idle_conns_per_host",
		},
		{
			name:    "gateway idle timeout",
			mutate:  func(c *Config) { c.Gateway.IdleConnTimeoutSeconds = 0 },
			wantErr: "gateway.idle_conn_timeout_seconds",
		},
		{
			name:    "gateway max upstream clients",
			mutate:  func(c *Config) { c.Gateway.MaxUpstreamClients = 0 },
			wantErr: "gateway.max_upstream_clients",
		},
		{
			name:    "gateway client idle ttl",
			mutate:  func(c *Config) { c.Gateway.ClientIdleTTLSeconds = 0 },
			wantErr: "gateway.client_idle_ttl_seconds",
		},
		{
			name:    "gateway concurrency slot ttl",
			mutate:  func(c *Config) { c.Gateway.ConcurrencySlotTTLMinutes = 0 },
			wantErr: "gateway.concurrency_slot_ttl_minutes",
		},
		{
			name:    "gateway max conns per host",
			mutate:  func(c *Config) { c.Gateway.MaxConnsPerHost = -1 },
			wantErr: "gateway.max_conns_per_host",
		},
		{
			name:    "gateway connection isolation",
			mutate:  func(c *Config) { c.Gateway.ConnectionPoolIsolation = "invalid" },
			wantErr: "gateway.connection_pool_isolation",
		},
		{
			name:    "gateway stream keepalive range",
			mutate:  func(c *Config) { c.Gateway.StreamKeepaliveInterval = 4 },
			wantErr: "gateway.stream_keepalive_interval",
		},
		{
			name:    "gateway stream data interval range",
			mutate:  func(c *Config) { c.Gateway.StreamDataIntervalTimeout = 5 },
			wantErr: "gateway.stream_data_interval_timeout",
		},
		{
			name:    "gateway stream data interval negative",
			mutate:  func(c *Config) { c.Gateway.StreamDataIntervalTimeout = -1 },
			wantErr: "gateway.stream_data_interval_timeout must be non-negative",
		},
		{
			name:    "gateway max line size",
			mutate:  func(c *Config) { c.Gateway.MaxLineSize = 1024 },
			wantErr: "gateway.max_line_size must be at least",
		},
		{
			name:    "gateway max line size negative",
			mutate:  func(c *Config) { c.Gateway.MaxLineSize = -1 },
			wantErr: "gateway.max_line_size must be non-negative",
		},
		{
			name:    "gateway scheduling sticky waiting",
			mutate:  func(c *Config) { c.Gateway.Scheduling.StickySessionMaxWaiting = 0 },
			wantErr: "gateway.scheduling.sticky_session_max_waiting",
		},
		{
			name:    "gateway scheduling outbox poll",
			mutate:  func(c *Config) { c.Gateway.Scheduling.OutboxPollIntervalSeconds = 0 },
			wantErr: "gateway.scheduling.outbox_poll_interval_seconds",
		},
		{
			name:    "gateway scheduling outbox failures",
			mutate:  func(c *Config) { c.Gateway.Scheduling.OutboxLagRebuildFailures = 0 },
			wantErr: "gateway.scheduling.outbox_lag_rebuild_failures",
		},
		{
			name: "gateway outbox lag rebuild",
			mutate: func(c *Config) {
				c.Gateway.Scheduling.OutboxLagWarnSeconds = 10
				c.Gateway.Scheduling.OutboxLagRebuildSeconds = 5
			},
			wantErr: "gateway.scheduling.outbox_lag_rebuild_seconds",
		},
		{
			name:    "ops metrics collector ttl",
			mutate:  func(c *Config) { c.Ops.MetricsCollectorCache.TTL = -1 },
			wantErr: "ops.metrics_collector_cache.ttl",
		},
		{
			name:    "ops cleanup retention",
			mutate:  func(c *Config) { c.Ops.Cleanup.ErrorLogRetentionDays = -1 },
			wantErr: "ops.cleanup.error_log_retention_days",
		},
		{
			name:    "ops cleanup minute retention",
			mutate:  func(c *Config) { c.Ops.Cleanup.MinuteMetricsRetentionDays = -1 },
			wantErr: "ops.cleanup.minute_metrics_retention_days",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			cfg := buildValid(t)
			tt.mutate(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
