package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyLifecycleRepositoryStub struct {
	ProxyRepository
	created  *Proxy
	existing *Proxy
	updated  *Proxy
}

func (s *proxyLifecycleRepositoryStub) Create(_ context.Context, proxy *Proxy) error {
	s.created = proxy
	return nil
}

func (s *proxyLifecycleRepositoryStub) GetByID(_ context.Context, _ int64) (*Proxy, error) {
	if s.existing == nil {
		return nil, ErrProxyNotFound
	}
	copy := *s.existing
	return &copy, nil
}

func (s *proxyLifecycleRepositoryStub) Update(_ context.Context, proxy *Proxy) error {
	s.updated = proxy
	return nil
}

func TestProxyIsExpiredUsesInclusiveBoundaryAndNilMeansNever(t *testing.T) {
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Second)
	future := now.Add(time.Second)

	tests := []struct {
		name      string
		expiresAt *time.Time
		want      bool
	}{
		{name: "never", expiresAt: nil, want: false},
		{name: "past", expiresAt: &past, want: true},
		{name: "equal", expiresAt: &now, want: true},
		{name: "future", expiresAt: &future, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			proxy := Proxy{ExpiresAt: test.expiresAt}
			require.Equal(t, test.want, proxy.IsExpired(now))
		})
	}
}

func TestResolveProxyFallbackTargetCoversTerminalModesAndChains(t *testing.T) {
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name       string
		start      Proxy
		proxies    map[int64]Proxy
		wantTarget *int64
		wantChange bool
	}{
		{
			name:       "none keeps binding",
			start:      Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeNone},
			wantChange: false,
		},
		{
			name:       "direct clears binding",
			start:      Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeDirect},
			wantChange: true,
		},
		{
			name:  "one hop",
			start: Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(2)},
			proxies: map[int64]Proxy{
				2: {ID: 2, ExpiresAt: &future, Status: StatusActive},
			},
			wantTarget: proxyExpiryInt64Ptr(2),
			wantChange: true,
		},
		{
			name:  "multi hop skips expired backup",
			start: Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(2)},
			proxies: map[int64]Proxy{
				2: {ID: 2, ExpiresAt: &past, Status: StatusExpired, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(3)},
				3: {ID: 3, ExpiresAt: &future, Status: StatusActive},
			},
			wantTarget: proxyExpiryInt64Ptr(3),
			wantChange: true,
		},
		{
			name:  "expired backup can terminate at direct",
			start: Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(2)},
			proxies: map[int64]Proxy{
				2: {ID: 2, ExpiresAt: &past, Status: StatusExpired, FallbackMode: FallbackModeDirect},
			},
			wantChange: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, change := ResolveProxyFallbackTarget(test.start, test.proxies, now)
			require.Equal(t, test.wantChange, change)
			if test.wantTarget == nil {
				require.Nil(t, target)
			} else {
				require.NotNil(t, target)
				require.Equal(t, *test.wantTarget, *target)
			}
		})
	}
}

func TestResolveProxyFallbackTargetFailsClosedForBrokenChains(t *testing.T) {
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)

	tests := []struct {
		name    string
		start   Proxy
		proxies map[int64]Proxy
	}{
		{
			name:  "missing target",
			start: Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(99)},
		},
		{
			name:  "cycle",
			start: Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(2)},
			proxies: map[int64]Proxy{
				2: {ID: 2, ExpiresAt: &past, Status: StatusExpired, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(1)},
			},
		},
		{
			name:  "expired terminal none",
			start: Proxy{ID: 1, ExpiresAt: &past, FallbackMode: FallbackModeProxy, BackupProxyID: proxyExpiryInt64Ptr(2)},
			proxies: map[int64]Proxy{
				2: {ID: 2, ExpiresAt: &past, Status: StatusExpired, FallbackMode: FallbackModeNone},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, change := ResolveProxyFallbackTarget(test.start, test.proxies, now)
			require.Nil(t, target)
			require.False(t, change, "broken fallback chains must not silently switch to direct")
		})
	}
}

func TestCanAccountUseProxyFallbackPreservesLocalOwnerScopeAndCapacityRules(t *testing.T) {
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	ownerOne := int64(101)
	ownerTwo := int64(202)
	account := Account{Platform: PlatformOpenAI, AccountLevel: "pro", OwnerUserID: &ownerOne}

	tests := []struct {
		name            string
		target          Proxy
		currentBindings int64
		want            bool
	}{
		{
			name:   "platform proxy matching scope",
			target: Proxy{Status: StatusActive, Platform: PlatformOpenAI, RequiredAccountLevel: "pro", ExpiresAt: &future},
			want:   true,
		},
		{
			name:   "platform mismatch",
			target: Proxy{Status: StatusActive, Platform: PlatformAnthropic, RequiredAccountLevel: "pro", ExpiresAt: &future},
		},
		{
			name:   "account level mismatch",
			target: Proxy{Status: StatusActive, Platform: PlatformOpenAI, RequiredAccountLevel: "team", ExpiresAt: &future},
		},
		{
			name:            "capacity exhausted",
			target:          Proxy{Status: StatusActive, Platform: PlatformOpenAI, RequiredAccountLevel: "pro", MaxAccounts: 2, ExpiresAt: &future},
			currentBindings: 2,
		},
		{
			name:   "owned proxy visible to matching owner",
			target: Proxy{Status: StatusActive, OwnerUserID: &ownerOne, Platform: PlatformAnthropic, RequiredAccountLevel: "team", ExpiresAt: &future},
			want:   true,
		},
		{
			name:   "owned proxy rejects another owner",
			target: Proxy{Status: StatusActive, OwnerUserID: &ownerTwo, ExpiresAt: &future},
		},
		{
			name:   "inactive proxy rejected",
			target: Proxy{Status: StatusDisabled, Platform: PlatformOpenAI, ExpiresAt: &future},
		},
		{
			name:   "expired proxy rejected",
			target: Proxy{Status: StatusActive, Platform: PlatformOpenAI, ExpiresAt: &past},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, CanAccountUseProxyFallback(test.target, account, test.currentBindings, now))
		})
	}
}

func TestAdminServiceCreateProxyPersistsLifecycleAndKeepsLegacyDefault(t *testing.T) {
	t.Run("upstream lifecycle fields", func(t *testing.T) {
		expiresAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
		backupID := int64(9)
		repo := &proxyLifecycleRepositoryStub{existing: &Proxy{ID: backupID, Status: StatusActive}}
		svc := &adminServiceImpl{proxyRepo: repo}

		created, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
			Name:           "primary",
			Protocol:       "http",
			Host:           "primary.example",
			Port:           8080,
			ExpiresAt:      &expiresAt,
			FallbackMode:   FallbackModeProxy,
			BackupProxyID:  &backupID,
			ExpiryWarnDays: 3,
		})

		require.NoError(t, err)
		require.Same(t, repo.created, created)
		require.Equal(t, &expiresAt, created.ExpiresAt)
		require.Equal(t, FallbackModeProxy, created.FallbackMode)
		require.Equal(t, &backupID, created.BackupProxyID)
		require.Equal(t, 3, created.ExpiryWarnDays)
	})

	t.Run("legacy caller defaults to none", func(t *testing.T) {
		repo := &proxyLifecycleRepositoryStub{}
		svc := &adminServiceImpl{proxyRepo: repo}

		created, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
			Name: "legacy", Protocol: "http", Host: "legacy.example", Port: 8080,
		})

		require.NoError(t, err)
		require.Equal(t, FallbackModeNone, created.FallbackMode)
	})
}

func TestAdminServiceUpdateProxyLifecycleUsesPresenceWithoutClearingLocalState(t *testing.T) {
	proxyID := int64(4)
	backupID := int64(9)
	expiresAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	existing := &Proxy{
		ID: proxyID, Name: "before", Status: StatusActive,
		ExpiresAt: &expiresAt, FallbackMode: FallbackModeProxy,
		BackupProxyID: &backupID, ExpiryWarnDays: 7,
	}

	t.Run("omitted preserves every lifecycle field", func(t *testing.T) {
		repo := &proxyLifecycleRepositoryStub{existing: existing}
		svc := &adminServiceImpl{proxyRepo: repo}

		updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{Name: "renamed"})

		require.NoError(t, err)
		require.Equal(t, "renamed", updated.Name)
		require.Equal(t, existing.ExpiresAt, updated.ExpiresAt)
		require.Equal(t, existing.FallbackMode, updated.FallbackMode)
		require.Equal(t, existing.BackupProxyID, updated.BackupProxyID)
		require.Equal(t, existing.ExpiryWarnDays, updated.ExpiryWarnDays)
	})

	t.Run("legacy empty fallback does not block unrelated update", func(t *testing.T) {
		legacy := &Proxy{ID: proxyID, Name: "before", Status: StatusActive}
		repo := &proxyLifecycleRepositoryStub{existing: legacy}
		svc := &adminServiceImpl{proxyRepo: repo}

		updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{Name: "renamed"})

		require.NoError(t, err)
		require.Equal(t, "renamed", updated.Name)
		require.Equal(t, FallbackModeNone, updated.FallbackMode)
	})

	t.Run("explicit null and zero clear only requested fields", func(t *testing.T) {
		repo := &proxyLifecycleRepositoryStub{existing: existing}
		svc := &adminServiceImpl{proxyRepo: repo}
		mode := FallbackModeNone
		warnDays := 0

		updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
			ExpiresAtProvided:     true,
			ExpiresAt:             nil,
			FallbackMode:          &mode,
			BackupProxyIDProvided: true,
			BackupProxyID:         nil,
			ExpiryWarnDays:        &warnDays,
		})

		require.NoError(t, err)
		require.Nil(t, updated.ExpiresAt)
		require.Equal(t, FallbackModeNone, updated.FallbackMode)
		require.Nil(t, updated.BackupProxyID)
		require.Zero(t, updated.ExpiryWarnDays)
	})
}

func TestAdminServiceRejectsInvalidProxyLifecycleBeforePersistence(t *testing.T) {
	proxyID := int64(4)

	t.Run("negative warning days", func(t *testing.T) {
		negative := -1
		repo := &proxyLifecycleRepositoryStub{existing: &Proxy{ID: proxyID}}
		svc := &adminServiceImpl{proxyRepo: repo}

		updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{ExpiryWarnDays: &negative})

		require.Error(t, err)
		require.Nil(t, updated)
		require.Nil(t, repo.updated)
	})

	t.Run("self backup", func(t *testing.T) {
		repo := &proxyLifecycleRepositoryStub{existing: &Proxy{ID: proxyID}}
		svc := &adminServiceImpl{proxyRepo: repo}

		updated, err := svc.UpdateProxy(context.Background(), proxyID, &UpdateProxyInput{
			BackupProxyIDProvided: true,
			BackupProxyID:         &proxyID,
		})

		require.Error(t, err)
		require.Nil(t, updated)
		require.Nil(t, repo.updated)
	})

	t.Run("proxy mode without backup on create", func(t *testing.T) {
		repo := &proxyLifecycleRepositoryStub{}
		svc := &adminServiceImpl{proxyRepo: repo}

		created, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
			Name: "invalid", Protocol: "http", Host: "invalid.example", Port: 8080,
			FallbackMode: FallbackModeProxy,
		})

		require.Error(t, err)
		require.Nil(t, created)
		require.Nil(t, repo.created)
	})
}

func TestExplicitAccountProxyChangesAbandonAutomaticFallbackOrigin(t *testing.T) {
	for _, name := range []string{"admin_service.go", "account_service.go"} {
		content, err := os.ReadFile(name)
		require.NoError(t, err)
		source := strings.ToLower(strings.Join(strings.Fields(string(content)), " "))
		require.Contains(t, source, "account.proxyfallbackoriginid = nil", "%s must prevent a later revert from overwriting an explicit proxy choice", name)
	}
}

func proxyExpiryInt64Ptr(value int64) *int64 { return &value }
