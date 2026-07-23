//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type snapshotHydrationCache struct {
	snapshot   []*Account
	accounts   map[int64]*Account
	accountErr error
}

func (c *snapshotHydrationCache) GetSnapshot(ctx context.Context, bucket SchedulerBucket) ([]*Account, bool, error) {
	return c.snapshot, true, nil
}

func (c *snapshotHydrationCache) SetSnapshot(ctx context.Context, bucket SchedulerBucket, accounts []Account) error {
	return nil
}

func (c *snapshotHydrationCache) GetAccount(ctx context.Context, accountID int64) (*Account, error) {
	if c.accountErr != nil {
		return nil, c.accountErr
	}
	if c.accounts == nil {
		return nil, nil
	}
	return c.accounts[accountID], nil
}

func (c *snapshotHydrationCache) SetAccount(ctx context.Context, account *Account) error {
	return nil
}

func (c *snapshotHydrationCache) DeleteAccount(ctx context.Context, accountID int64) error {
	return nil
}

func (c *snapshotHydrationCache) UpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	return nil
}

func (c *snapshotHydrationCache) TryLockBucket(ctx context.Context, bucket SchedulerBucket, ttl time.Duration) (bool, error) {
	return true, nil
}

func (c *snapshotHydrationCache) UnlockBucket(ctx context.Context, bucket SchedulerBucket) error {
	return nil
}

func (c *snapshotHydrationCache) ListBuckets(ctx context.Context) ([]SchedulerBucket, error) {
	return nil, nil
}

func (c *snapshotHydrationCache) GetOutboxWatermark(ctx context.Context) (int64, error) {
	return 0, nil
}

func (c *snapshotHydrationCache) SetOutboxWatermark(ctx context.Context, id int64) error {
	return nil
}

func TestOpenAISelectAccountWithLoadAwareness_HydratesSelectedAccountFromSchedulerSnapshot(t *testing.T) {
	cache := &snapshotHydrationCache{
		snapshot: []*Account{
			{
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				GroupIDs:    []int64{2},
				Credentials: map[string]any{
					"model_mapping": map[string]any{
						"gpt-4": "gpt-4",
					},
				},
			},
		},
		accounts: map[int64]*Account{
			1: {
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				GroupIDs:    []int64{2},
				Credentials: map[string]any{
					"api_key":       "sk-live",
					"model_mapping": map[string]any{"gpt-4": "gpt-4"},
				},
			},
		},
	}

	schedulerSnapshot := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	groupID := int64(2)
	svc := &OpenAIGatewayService{
		schedulerSnapshot: schedulerSnapshot,
		cache:             &stubGatewayCache{},
	}

	selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), &groupID, "", "gpt-4", nil)
	if err != nil {
		t.Fatalf("SelectAccountWithLoadAwareness error: %v", err)
	}
	if selection == nil || selection.Account == nil {
		t.Fatalf("expected selected account")
	}
	if got := selection.Account.GetOpenAIApiKey(); got != "sk-live" {
		t.Fatalf("expected hydrated api key, got %q", got)
	}
}

func TestGatewaySelectAccountWithLoadAwareness_HydratesSelectedAccountFromSchedulerSnapshot(t *testing.T) {
	cache := &snapshotHydrationCache{
		snapshot: []*Account{
			{
				ID:          9,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
			},
		},
		accounts: map[int64]*Account{
			9: {
				ID:          9,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeAPIKey,
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
				Credentials: map[string]any{
					"api_key": "anthropic-live-key",
				},
			},
		},
	}

	schedulerSnapshot := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
	svc := &GatewayService{
		schedulerSnapshot: schedulerSnapshot,
		cache:             &mockGatewayCacheForPlatform{},
		cfg:               testConfig(),
	}

	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "claude-3-5-sonnet-20241022", nil, "", 0)
	if err != nil {
		t.Fatalf("SelectAccountWithLoadAwareness error: %v", err)
	}
	if result == nil || result.Account == nil {
		t.Fatalf("expected selected account")
	}
	if got := result.Account.GetCredential("api_key"); got != "anthropic-live-key" {
		t.Fatalf("expected hydrated api key, got %q", got)
	}
}

func TestSelectionResultHydrationOwnsAcquiredSlotLifecycle(t *testing.T) {
	ownerUserID := int64(501)
	requestCtx := context.WithValue(context.Background(), ctxkey.AuthenticatedUserID, ownerUserID+1)

	tests := []struct {
		name       string
		account    *Account
		accountErr error
		wantErr    bool
	}{
		{
			name:    "missing hydrated account",
			wantErr: true,
		},
		{
			name:       "hydration read error",
			accountErr: errors.New("snapshot read failed"),
			wantErr:    true,
		},
		{
			name: "owned pending account is invisible to another user",
			account: &Account{
				ID:          9,
				OwnerUserID: &ownerUserID,
				ShareMode:   AccountShareModePublic,
				ShareStatus: AccountShareStatusPending,
			},
			wantErr: true,
		},
		{
			name:    "visible account transfers release ownership",
			account: &Account{ID: 9},
		},
	}

	for _, serviceName := range []string{"gateway", "openai"} {
		serviceName := serviceName
		for _, tt := range tests {
			tt := tt
			t.Run(serviceName+"/"+tt.name, func(t *testing.T) {
				cache := &snapshotHydrationCache{
					accounts:   map[int64]*Account{9: tt.account},
					accountErr: tt.accountErr,
				}
				snapshot := NewSchedulerSnapshotService(cache, nil, nil, nil, nil)
				releaseCount := 0
				release := func() { releaseCount++ }
				metadata := &Account{ID: 9}

				var (
					result *AccountSelectionResult
					err    error
				)
				if serviceName == "gateway" {
					result, err = (&GatewayService{schedulerSnapshot: snapshot}).newSelectionResult(requestCtx, metadata, true, release, nil)
				} else {
					result, err = (&OpenAIGatewayService{schedulerSnapshot: snapshot}).newSelectionResult(requestCtx, metadata, true, release, nil)
				}

				if tt.wantErr {
					require.Error(t, err)
					require.Nil(t, result)
					require.Equal(t, 1, releaseCount, "hydration failure must release the acquired slot exactly once")
					return
				}

				require.NoError(t, err)
				require.NotNil(t, result)
				require.Zero(t, releaseCount, "a successful result transfers release ownership to the caller")
				require.NotNil(t, result.ReleaseFunc)
				result.ReleaseFunc()
				require.Equal(t, 1, releaseCount)
			})
		}
	}
}
