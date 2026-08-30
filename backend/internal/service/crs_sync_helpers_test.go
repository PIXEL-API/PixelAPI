package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type crsSyncAccountRepoStub struct {
	AccountRepository
	accounts        map[string]*Account
	creates         []*Account
	updates         []*Account
	createErr       error
	preview         []CRSAccountPreviewSnapshot
	previewSequence [][]CRSAccountPreviewSnapshot
	previewErr      error
	previewCalls    int
	uowCalls        int
	uowErr          error
}

func (r *crsSyncAccountRepoStub) GetByCRSAccountID(_ context.Context, crsAccountID string) (*Account, error) {
	return r.accounts[crsAccountID], nil
}

func (r *crsSyncAccountRepoStub) Update(_ context.Context, account *Account) error {
	r.updates = append(r.updates, account)
	return nil
}

func (r *crsSyncAccountRepoStub) Create(_ context.Context, account *Account) error {
	if r.createErr != nil {
		return r.createErr
	}
	if account.ID == 0 {
		account.ID = int64(len(r.creates) + 100)
	}
	r.creates = append(r.creates, account)
	return nil
}

func (r *crsSyncAccountRepoStub) WithCRSProxyAccountUnitOfWork(
	ctx context.Context,
	mutate func(context.Context) error,
) error {
	r.uowCalls++
	if r.uowErr != nil {
		return r.uowErr
	}
	return mutate(ctx)
}

func (r *crsSyncAccountRepoStub) ListCRSAccountPreviewSnapshots(
	context.Context,
) ([]CRSAccountPreviewSnapshot, error) {
	r.previewCalls++
	if r.previewErr != nil {
		return nil, r.previewErr
	}
	if len(r.previewSequence) >= r.previewCalls {
		return append(
			[]CRSAccountPreviewSnapshot(nil),
			r.previewSequence[r.previewCalls-1]...,
		), nil
	}
	return append([]CRSAccountPreviewSnapshot(nil), r.preview...), nil
}

type crsPreviewMissingCapabilityRepoStub struct {
	AccountRepository
}

type crsProxyUOWMissingAccountRepoStub struct {
	AccountRepository
}

type crsSyncGuardedAccountRepoStub struct {
	*crsSyncAccountRepoStub
	requests       []AccountMutationGuardRequest
	guardErr       error
	afterMutateErr error
}

func (r *crsSyncGuardedAccountRepoStub) WithAccountMutationGuard(
	ctx context.Context,
	request AccountMutationGuardRequest,
	mutate func(context.Context) error,
) error {
	r.requests = append(r.requests, request)
	if r.guardErr != nil {
		return r.guardErr
	}
	for _, target := range request.Targets {
		if target.After != nil &&
			target.After.AccountShareModeListingID != nil &&
			!request.ForceActiveEdit {
			return ErrAccountMutationForceRequired
		}
	}
	if err := mutate(WithAccountMutationGuardContext(ctx)); err != nil {
		return err
	}
	return r.afterMutateErr
}

type crsSyncProxyRepoStub struct {
	ProxyRepository
	active      []Proxy
	listErr     error
	createErr   error
	listCalls   int
	createCalls int
	creates     []*Proxy
}

func (r *crsSyncProxyRepoStub) ListActive(context.Context) ([]Proxy, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]Proxy(nil), r.active...), nil
}

func (r *crsSyncProxyRepoStub) Create(_ context.Context, proxy *Proxy) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	if proxy.ID == 0 {
		proxy.ID = int64(500 + len(r.creates))
	}
	r.creates = append(r.creates, proxy)
	return nil
}

func TestBuildSelectedSet(t *testing.T) {
	tests := []struct {
		name     string
		ids      []string
		wantNil  bool
		wantSize int
	}{
		{
			name:    "nil input returns nil (backward compatible: create all)",
			ids:     nil,
			wantNil: true,
		},
		{
			name:     "empty slice returns empty map (create none)",
			ids:      []string{},
			wantNil:  false,
			wantSize: 0,
		},
		{
			name:     "single ID",
			ids:      []string{"abc-123"},
			wantNil:  false,
			wantSize: 1,
		},
		{
			name:     "multiple IDs",
			ids:      []string{"a", "b", "c"},
			wantNil:  false,
			wantSize: 3,
		},
		{
			name:     "duplicate IDs are deduplicated",
			ids:      []string{"a", "a", "b"},
			wantNil:  false,
			wantSize: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSelectedSet(tt.ids)
			if tt.wantNil {
				if got != nil {
					t.Errorf("buildSelectedSet(%v) = %v, want nil", tt.ids, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("buildSelectedSet(%v) = nil, want non-nil map", tt.ids)
			}
			if len(got) != tt.wantSize {
				t.Errorf("buildSelectedSet(%v) has %d entries, want %d", tt.ids, len(got), tt.wantSize)
			}
			// Verify all unique IDs are present
			for _, id := range tt.ids {
				if _, ok := got[id]; !ok {
					t.Errorf("buildSelectedSet(%v) missing key %q", tt.ids, id)
				}
			}
		})
	}
}

func TestShouldCreateAccount(t *testing.T) {
	tests := []struct {
		name        string
		crsID       string
		selectedSet map[string]struct{}
		want        bool
	}{
		{
			name:        "nil set allows all (backward compatible)",
			crsID:       "any-id",
			selectedSet: nil,
			want:        true,
		},
		{
			name:        "empty set blocks all",
			crsID:       "any-id",
			selectedSet: map[string]struct{}{},
			want:        false,
		},
		{
			name:        "ID in set is allowed",
			crsID:       "abc-123",
			selectedSet: map[string]struct{}{"abc-123": {}, "def-456": {}},
			want:        true,
		},
		{
			name:        "ID not in set is blocked",
			crsID:       "xyz-789",
			selectedSet: map[string]struct{}{"abc-123": {}, "def-456": {}},
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldCreateAccount(tt.crsID, tt.selectedSet)
			if got != tt.want {
				t.Errorf("shouldCreateAccount(%q, %v) = %v, want %v",
					tt.crsID, tt.selectedSet, got, tt.want)
			}
		})
	}
}

func newCRSSyncTestConfig() *config.Config {
	return &config.Config{
		JWT: config.JWTConfig{
			Secret: "crs-sync-test-secret",
		},
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				Enabled:           false,
				AllowInsecureHTTP: true,
			},
		},
	}
}

func previewCRSSyncToken(
	t *testing.T,
	svc *CRSSyncService,
	baseURL string,
	actorAdminID int64,
) string {
	t.Helper()
	preview, err := svc.PreviewFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      baseURL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: actorAdminID,
	})
	require.NoError(t, err)
	require.NotEmpty(t, preview.PreviewToken)
	return preview.PreviewToken
}

func TestCRSSyncUpdateExistingAccountFailsClosedWithoutGuardForRoomAccounts(t *testing.T) {
	listingID := int64(71)
	tests := []struct {
		name    string
		account *Account
	}{
		{
			name: "room listing marker",
			account: &Account{
				ID:                        11,
				AccountShareModeListingID: &listingID,
			},
		},
		{
			name: "external room placement",
			account: &Account{
				ID: 12,
				ExternalPlacement: &AccountExternalPlacement{
					Target: AccountExternalPlacementRoom,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &crsSyncAccountRepoStub{}
			svc := NewCRSSyncService(repo, nil, nil, nil, nil, nil)

			err := svc.updateExistingAccount(context.Background(), SyncFromCRSInput{}, tt.account)

			require.ErrorIs(t, err, ErrAccountMutationGuardUnavailable)
			require.Empty(t, repo.updates)
		})
	}
}

func TestCRSSyncUpdateExistingAccountKeepsUnboundLegacyRepositoryCompatibility(t *testing.T) {
	repo := &crsSyncAccountRepoStub{}
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, nil)
	account := &Account{ID: 13, Name: "unbound"}

	err := svc.updateExistingAccount(context.Background(), SyncFromCRSInput{}, account)

	require.NoError(t, err)
	require.Equal(t, []*Account{account}, repo.updates)
}

func TestCRSSyncUpdateExistingAccountForwardsCompleteAdminGuardContract(t *testing.T) {
	expectedVersion := int64(8)
	expectedVersions := map[int64]int64{71: 8, 72: 3}
	updatedAt := time.Date(2026, time.July, 27, 9, 30, 0, 0, time.UTC)
	account := &Account{
		ID:        14,
		Name:      "guarded",
		UpdatedAt: updatedAt,
		GroupIDs:  []int64{5, 3},
	}
	repo := &crsSyncGuardedAccountRepoStub{
		crsSyncAccountRepoStub: &crsSyncAccountRepoStub{},
	}
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, nil)
	input := SyncFromCRSInput{
		ActorAdminID:     91,
		ForceActiveEdit:  true,
		Confirmed:        true,
		Reason:           "CRS 管理员强制同步",
		ExpectedVersion:  &expectedVersion,
		ExpectedVersions: expectedVersions,
		OperationID:      "crs-sync-operation",
	}

	err := svc.updateExistingAccount(context.Background(), input, account)

	require.NoError(t, err)
	require.Len(t, repo.requests, 1)
	request := repo.requests[0]
	require.Equal(t, int64(91), request.ActorUserID)
	require.True(t, request.ActorIsAdmin)
	require.Equal(t, AccountMutationIntentAdmin, request.Intent)
	require.True(t, request.ForceActiveEdit)
	require.True(t, request.Confirmed)
	require.Equal(t, input.Reason, request.Reason)
	require.Same(t, input.ExpectedVersion, request.ExpectedListingVersion)
	require.Equal(t, expectedVersions, request.ExpectedListingVersions)
	require.Equal(t, input.OperationID, request.OperationID)
	require.Len(t, request.Targets, 1)
	require.Equal(t, account.ID, request.Targets[0].AccountID)
	require.Equal(t, updatedAt, request.Targets[0].ExpectedUpdatedAt)
	require.NotSame(t, account, request.Targets[0].After, "guard must receive a staged account so commit failure cannot leak mutations")
	require.Equal(t, *account, *request.Targets[0].After)
	require.Equal(t, account.GroupIDs, request.Targets[0].GroupIDs)
	require.Len(t, repo.updates, 1)
	require.Same(t, request.Targets[0].After, repo.updates[0])
	require.Equal(t, *account, *repo.updates[0])
}

func TestCRSSyncExistingAccountBranchesUseGuardAndKeepPerItemFailureSemantics(t *testing.T) {
	server := newCRSSyncExistingAccountsServer(t)
	defer server.Close()

	listingID := int64(71)
	accounts := map[string]*Account{}
	for index, crsID := range []string{
		"claude",
		"claude-console",
		"openai-oauth",
		"openai-responses",
		"gemini-oauth",
		"gemini-apikey",
	} {
		accounts[crsID] = &Account{
			ID:          int64(index + 1),
			Name:        "before-" + crsID,
			Credentials: map[string]any{"preserved": true},
			Extra:       map[string]any{"crs_account_id": crsID},
			GroupIDs:    []int64{3},
			UpdatedAt:   time.Date(2026, time.July, 27, 8, index, 0, 0, time.UTC),
		}
	}
	accounts["claude"].AccountShareModeListingID = &listingID
	previewSnapshots := make([]CRSAccountPreviewSnapshot, 0, len(accounts))
	for _, account := range accounts {
		crsAccountID, ok := account.Extra["crs_account_id"].(string)
		require.True(t, ok)
		snapshot := CRSAccountPreviewSnapshot{
			CRSAccountID:   crsAccountID,
			LocalAccountID: account.ID,
			RoomBindings:   []CRSAccountRoomBindingSnapshot{},
		}
		if account.AccountShareModeListingID != nil {
			snapshot.RoomBindings = []CRSAccountRoomBindingSnapshot{{
				ListingID:  *account.AccountShareModeListingID,
				RowVersion: 1,
			}}
		}
		previewSnapshots = append(previewSnapshots, snapshot)
	}
	baseRepo := &crsSyncAccountRepoStub{
		accounts: accounts,
		preview:  previewSnapshots,
	}
	repo := &crsSyncGuardedAccountRepoStub{crsSyncAccountRepoStub: baseRepo}
	cfg := newCRSSyncTestConfig()
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
	previewToken := previewCRSSyncToken(t, svc, server.URL, 91)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		SyncProxies:  false,
		ActorAdminID: 91,
		PreviewToken: previewToken,
	})

	require.NoError(t, err)
	require.Equal(t, 5, result.Updated)
	require.Equal(t, 1, result.Failed)
	require.Zero(t, result.Created)
	require.Zero(t, result.Skipped)
	require.Len(t, result.Items, 6)
	require.Equal(t, "failed", result.Items[0].Action)
	require.Contains(t, result.Items[0].Error, ErrAccountMutationForceRequired.Reason)
	for _, item := range result.Items[1:] {
		require.Equal(t, "updated", item.Action, item.CRSAccountID)
	}
	require.Len(t, repo.requests, 6, "all six existing-account branches must pass through the shared guard")
	require.Len(t, baseRepo.updates, 5, "the rejected room account must not reach Update")
	for _, request := range repo.requests {
		require.Equal(t, int64(91), request.ActorUserID)
		require.True(t, request.ActorIsAdmin)
		require.Equal(t, AccountMutationIntentAdmin, request.Intent)
		require.False(t, request.ForceActiveEdit)
	}
}

func TestCRSPreviewReturnsStableForceEditSnapshots(t *testing.T) {
	server := newCRSSyncExistingAccountsServer(t)
	defer server.Close()

	repo := &crsSyncAccountRepoStub{
		preview: []CRSAccountPreviewSnapshot{
			{
				CRSAccountID:   "gemini-oauth",
				LocalAccountID: 30,
				RoomBindings: []CRSAccountRoomBindingSnapshot{
					{ListingID: 91, RowVersion: 8},
					{ListingID: 72, RowVersion: 3},
				},
			},
			{
				CRSAccountID:   "claude",
				LocalAccountID: 10,
				RoomBindings:   []CRSAccountRoomBindingSnapshot{},
			},
		},
	}
	cfg := newCRSSyncTestConfig()
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
	fixedNow := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	result, err := svc.PreviewFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.previewCalls)
	require.NotEmpty(t, result.PreviewToken)
	require.Equal(t, fixedNow.Add(crsPreviewTokenTTL).Unix(), result.ExpiresAt)
	require.Equal(t, []CRSPreviewAccount{
		{
			CRSAccountID:            "claude",
			LocalAccountID:          10,
			Kind:                    "claude",
			Name:                    "Claude",
			Platform:                PlatformAnthropic,
			Type:                    AccountTypeSetupToken,
			RequiresForceActiveEdit: false,
			RoomBindings:            []CRSAccountRoomBindingSnapshot{},
		},
		{
			CRSAccountID:            "gemini-oauth",
			LocalAccountID:          30,
			Kind:                    "gemini-oauth",
			Name:                    "Gemini OAuth",
			Platform:                PlatformGemini,
			Type:                    AccountTypeOAuth,
			RequiresForceActiveEdit: true,
			RoomBindings: []CRSAccountRoomBindingSnapshot{
				{ListingID: 72, RowVersion: 3},
				{ListingID: 91, RowVersion: 8},
			},
		},
	}, result.ExistingAccounts)
	require.Equal(t, []string{
		"claude-console",
		"gemini-apikey",
		"openai-oauth",
		"openai-responses",
	}, []string{
		result.NewAccounts[0].CRSAccountID,
		result.NewAccounts[1].CRSAccountID,
		result.NewAccounts[2].CRSAccountID,
		result.NewAccounts[3].CRSAccountID,
	})
}

func TestCRSPreviewFailsClosedWhenRepositorySnapshotCapabilityIsMissing(t *testing.T) {
	svc := NewCRSSyncService(&crsPreviewMissingCapabilityRepoStub{}, nil, nil, nil, nil, nil)

	_, err := svc.PreviewFromCRS(context.Background(), SyncFromCRSInput{ActorAdminID: 91})

	require.ErrorIs(t, err, ErrCRSPreviewSnapshotUnavailable)
}

func TestCRSPreviewRequiresAuthenticatedAdministratorBeforeReadingSnapshot(t *testing.T) {
	repo := &crsSyncAccountRepoStub{}
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, newCRSSyncTestConfig())

	_, err := svc.PreviewFromCRS(context.Background(), SyncFromCRSInput{})

	require.ErrorIs(t, err, ErrCRSPreviewActorRequired)
	require.Zero(t, repo.previewCalls)
}

func TestCRSSyncRejectsMissingPreviewTokenBeforeAnyWrite(t *testing.T) {
	repo := &crsSyncAccountRepoStub{}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())

	_, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      "http://127.0.0.1:1",
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
		SyncProxies:  true,
	})

	require.ErrorIs(t, err, ErrCRSPreviewTokenRequired)
	require.Zero(t, proxyRepo.listCalls)
	require.Zero(t, proxyRepo.createCalls)
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSSyncRejectsInvalidPreviewContextsBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*CRSSyncService, *SyncFromCRSInput)
		wantError error
	}{
		{
			name: "tampered token",
			mutate: func(_ *CRSSyncService, input *SyncFromCRSInput) {
				last := input.PreviewToken[len(input.PreviewToken)-1]
				replacement := byte('A')
				if last == replacement {
					replacement = 'B'
				}
				input.PreviewToken = input.PreviewToken[:len(input.PreviewToken)-1] + string(replacement)
			},
			wantError: ErrCRSPreviewTokenInvalid,
		},
		{
			name: "expired token",
			mutate: func(svc *CRSSyncService, _ *SyncFromCRSInput) {
				expiredAt := time.Date(2026, time.July, 27, 10, 6, 0, 0, time.UTC)
				svc.now = func() time.Time { return expiredAt }
			},
			wantError: ErrCRSPreviewTokenExpired,
		},
		{
			name: "actor mismatch",
			mutate: func(_ *CRSSyncService, input *SyncFromCRSInput) {
				input.ActorAdminID = 92
			},
			wantError: ErrCRSPreviewContextConflict,
		},
		{
			name: "connection password changed",
			mutate: func(_ *CRSSyncService, input *SyncFromCRSInput) {
				input.Password = "changed-secret"
			},
			wantError: ErrCRSPreviewContextConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newCRSSyncExistingAccountsServer(t)
			defer server.Close()

			repo := &crsSyncAccountRepoStub{}
			proxyRepo := &crsSyncProxyRepoStub{}
			svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
			previewNow := time.Date(2026, time.July, 27, 10, 0, 0, 0, time.UTC)
			svc.now = func() time.Time { return previewNow }
			input := SyncFromCRSInput{
				BaseURL:      server.URL,
				Username:     "admin",
				Password:     "secret",
				ActorAdminID: 91,
				SyncProxies:  true,
				PreviewToken: previewCRSSyncToken(t, svc, server.URL, 91),
			}
			tt.mutate(svc, &input)

			_, err := svc.SyncFromCRS(context.Background(), input)

			require.ErrorIs(t, err, tt.wantError)
			require.Zero(t, proxyRepo.listCalls)
			require.Zero(t, proxyRepo.createCalls)
			require.Empty(t, repo.creates)
			require.Empty(t, repo.updates)
		})
	}
}

func TestCRSSyncRejectsRemoteExportDriftBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name    string
		account func(call int) map[string]any
	}{
		{
			name: "account field changed",
			account: func(call int) map[string]any {
				name := "Before preview"
				if call > 1 {
					name = "Changed after preview"
				}
				return crsConsoleExportAccount("remote-drift", name, "10.0.0.1")
			},
		},
		{
			name: "proxy field changed",
			account: func(call int) map[string]any {
				host := "10.0.0.1"
				if call > 1 {
					host = "10.0.0.9"
				}
				return crsConsoleExportAccount("remote-drift", "Remote drift", host)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newCRSSyncServer(t, func(call int) map[string]any {
				return crsConsoleExportPayload(tt.account(call))
			})
			defer server.Close()

			repo := &crsSyncAccountRepoStub{}
			proxyRepo := &crsSyncProxyRepoStub{}
			svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
			token := previewCRSSyncToken(t, svc, server.URL, 91)

			_, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
				BaseURL:      server.URL,
				Username:     "admin",
				Password:     "secret",
				ActorAdminID: 91,
				SyncProxies:  true,
				PreviewToken: token,
			})

			require.ErrorIs(t, err, ErrCRSPreviewContextConflict)
			require.Zero(t, proxyRepo.listCalls)
			require.Zero(t, proxyRepo.createCalls)
			require.Empty(t, repo.creates)
			require.Empty(t, repo.updates)
		})
	}
}

func TestCRSSyncIgnoresVolatileExportTimestampInPreviewSnapshot(t *testing.T) {
	server := newCRSSyncServer(t, func(call int) map[string]any {
		payload := crsConsoleExportPayload(
			crsConsoleExportAccount("stable-account", "Stable", ""),
		)
		exportedAt := "2026-07-27T10:00:00Z"
		if call > 1 {
			exportedAt = "2026-07-27T10:01:00Z"
		}
		data, ok := payload["data"].(map[string]any)
		require.True(t, ok)
		data["exportedAt"] = exportedAt
		return payload
	})
	defer server.Close()

	repo := &crsSyncAccountRepoStub{}
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:            server.URL,
		Username:           "admin",
		Password:           "secret",
		ActorAdminID:       91,
		SelectedAccountIDs: []string{},
		PreviewToken:       token,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Skipped)
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSSyncIgnoresRemoteAccountArrayReordering(t *testing.T) {
	server := newCRSSyncServer(t, func(call int) map[string]any {
		first := crsConsoleExportAccount("stable-a", "Stable A", "")
		second := crsConsoleExportAccount("stable-b", "Stable B", "")
		if call > 1 {
			return crsConsoleExportPayload(second, first)
		}
		return crsConsoleExportPayload(first, second)
	})
	defer server.Close()

	repo := &crsSyncAccountRepoStub{}
	svc := NewCRSSyncService(repo, nil, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:            server.URL,
		Username:           "admin",
		Password:           "secret",
		ActorAdminID:       91,
		SelectedAccountIDs: []string{},
		PreviewToken:       token,
	})

	require.NoError(t, err)
	require.Equal(t, 2, result.Skipped)
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSRejectsInvalidRemoteAccountIDsBeforeAnyWrite(t *testing.T) {
	tests := []struct {
		name     string
		accounts func() []map[string]any
	}{
		{
			name: "duplicate ID",
			accounts: func() []map[string]any {
				return []map[string]any{
					crsConsoleExportAccount("duplicate", "First", "10.0.0.1"),
					crsConsoleExportAccount("duplicate", "Second", "10.0.0.2"),
				}
			},
		},
		{
			name: "empty ID",
			accounts: func() []map[string]any {
				return []map[string]any{
					crsConsoleExportAccount("", "Empty", "10.0.0.1"),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" preview", func(t *testing.T) {
			server := newCRSSyncServer(t, func(int) map[string]any {
				return crsConsoleExportPayload(tt.accounts()...)
			})
			defer server.Close()

			repo := &crsSyncAccountRepoStub{}
			svc := NewCRSSyncService(repo, nil, nil, nil, nil, newCRSSyncTestConfig())

			_, err := svc.PreviewFromCRS(context.Background(), SyncFromCRSInput{
				BaseURL:      server.URL,
				Username:     "admin",
				Password:     "secret",
				ActorAdminID: 91,
			})

			require.ErrorIs(t, err, ErrCRSExportInvalid)
			require.Empty(t, repo.creates)
			require.Empty(t, repo.updates)
		})

		t.Run(tt.name+" sync", func(t *testing.T) {
			server := newCRSSyncServer(t, func(call int) map[string]any {
				if call == 1 {
					return crsConsoleExportPayload(
						crsConsoleExportAccount("valid-before-sync", "Valid", "10.0.0.1"),
					)
				}
				return crsConsoleExportPayload(tt.accounts()...)
			})
			defer server.Close()

			repo := &crsSyncAccountRepoStub{}
			proxyRepo := &crsSyncProxyRepoStub{}
			svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
			token := previewCRSSyncToken(t, svc, server.URL, 91)

			_, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
				BaseURL:      server.URL,
				Username:     "admin",
				Password:     "secret",
				ActorAdminID: 91,
				SyncProxies:  true,
				PreviewToken: token,
			})

			require.ErrorIs(t, err, ErrCRSExportInvalid)
			require.Zero(t, proxyRepo.listCalls)
			require.Zero(t, proxyRepo.createCalls)
			require.Empty(t, repo.creates)
			require.Empty(t, repo.updates)
		})
	}
}

func TestCRSSyncCapacityProbeUsesDistinctHighEntropyErrors(t *testing.T) {
	exported := &crsExportResponse{}
	exported.Data.ClaudeConsoleAccounts = []crsConsoleAccount{
		{ID: "capacity-a", Kind: "claude-console", Name: "Capacity A"},
		{ID: "capacity-b", Kind: "claude-console", Name: "Capacity B"},
	}

	probe := buildCRSSyncResponseCapacityProbe(exported)

	require.Len(t, probe.Items, 2)
	require.Len(t, probe.Items[0].Error, crsSyncItemErrorMaxBytes)
	require.Len(t, probe.Items[1].Error, crsSyncItemErrorMaxBytes)
	require.NotEqual(t, probe.Items[0].Error, probe.Items[1].Error)
	raw, err := json.Marshal(probe)
	require.NoError(t, err)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write(raw)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Greater(
		t,
		compressed.Len(),
		len(raw)/2,
		"capacity probe must not collapse like a repeated placeholder under gzip",
	)
}

func TestCRSSyncRejectsLocalRoomSnapshotDriftBeforeAnyWrite(t *testing.T) {
	server := newCRSSyncServer(t, func(int) map[string]any {
		return crsConsoleExportPayload(
			crsConsoleExportAccount("local-drift", "Local drift", "10.0.0.2"),
		)
	})
	defer server.Close()

	repo := &crsSyncAccountRepoStub{
		previewSequence: [][]CRSAccountPreviewSnapshot{
			{},
			{{
				CRSAccountID:   "local-drift",
				LocalAccountID: 41,
				RoomBindings: []CRSAccountRoomBindingSnapshot{{
					ListingID:  81,
					RowVersion: 2,
				}},
			}},
		},
	}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)

	_, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
		SyncProxies:  true,
		PreviewToken: token,
	})

	require.ErrorIs(t, err, ErrCRSPreviewContextConflict)
	require.Zero(t, proxyRepo.listCalls)
	require.Zero(t, proxyRepo.createCalls)
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSSyncResponseCapacityFailurePrecedesProxyAndAccountWrites(t *testing.T) {
	server := newCRSSyncServer(t, func(int) map[string]any {
		return crsConsoleExportPayload(
			crsConsoleExportAccount("capacity", "Capacity", "10.0.0.3"),
		)
	})
	defer server.Close()

	repo := &crsSyncAccountRepoStub{}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)
	capacityErr := errors.New("idempotency response capacity exceeded")
	capacityCalls := 0

	_, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
		SyncProxies:  true,
		PreviewToken: token,
		ValidateResponseCapacity: func(value any) error {
			capacityCalls++
			probe, ok := value.(*SyncFromCRSResult)
			require.True(t, ok)
			require.Len(t, probe.Items, 1)
			require.Len(t, probe.Items[0].Error, crsSyncItemErrorMaxBytes)
			return capacityErr
		},
	})

	require.ErrorIs(t, err, capacityErr)
	require.Equal(t, 1, capacityCalls)
	require.Zero(t, proxyRepo.listCalls)
	require.Zero(t, proxyRepo.createCalls)
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSSyncProxyWritesRespectNewAccountSelection(t *testing.T) {
	tests := []struct {
		name          string
		selectedIDs   []string
		wantCreated   int
		wantSkipped   int
		wantProxyAdds int
	}{
		{
			name:          "partial selection",
			selectedIDs:   []string{"selected"},
			wantCreated:   1,
			wantSkipped:   1,
			wantProxyAdds: 1,
		},
		{
			name:          "empty selection",
			selectedIDs:   []string{},
			wantCreated:   0,
			wantSkipped:   2,
			wantProxyAdds: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newCRSSyncServer(t, func(int) map[string]any {
				return crsConsoleExportPayload(
					crsConsoleExportAccount("selected", "Selected", "10.0.1.1"),
					crsConsoleExportAccount("not-selected", "Not selected", "10.0.1.2"),
				)
			})
			defer server.Close()

			repo := &crsSyncAccountRepoStub{}
			proxyRepo := &crsSyncProxyRepoStub{}
			svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
			token := previewCRSSyncToken(t, svc, server.URL, 91)

			result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
				BaseURL:            server.URL,
				Username:           "admin",
				Password:           "secret",
				ActorAdminID:       91,
				SyncProxies:        true,
				SelectedAccountIDs: tt.selectedIDs,
				PreviewToken:       token,
			})

			require.NoError(t, err)
			require.Equal(t, tt.wantCreated, result.Created)
			require.Equal(t, tt.wantSkipped, result.Skipped)
			require.Equal(t, tt.wantProxyAdds, proxyRepo.createCalls)
			require.Len(t, repo.creates, tt.wantCreated)
			if tt.wantCreated > 0 {
				require.Equal(t, "selected", repo.creates[0].Extra["crs_account_id"])
				require.NotNil(t, repo.creates[0].ProxyID)
			}
		})
	}
}

func TestPlanCRSProxyReusesOnlyActivePublicMatchingScope(t *testing.T) {
	now := time.Date(2026, time.August, 31, 4, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	ownerUserID := int64(41)
	source := &crsProxy{
		Protocol: "http",
		Host:     "10.0.0.8",
		Port:     8080,
		Username: "proxy-user",
		Password: "proxy-password",
	}
	matching := func(id int64) Proxy {
		return Proxy{
			ID:       id,
			Protocol: source.Protocol,
			Host:     source.Host,
			Port:     source.Port,
			Username: source.Username,
			Password: source.Password,
			Status:   StatusActive,
		}
	}

	tests := []struct {
		name       string
		cached     []Proxy
		resolvedID int64
	}{
		{name: "universal proxy", cached: []Proxy{matching(1)}, resolvedID: 1},
		{
			name: "matching platform proxy",
			cached: func() []Proxy {
				proxy := matching(2)
				proxy.Platform = PlatformOpenAI
				return []Proxy{proxy}
			}(),
			resolvedID: 2,
		},
		{
			name: "private proxy is not reused",
			cached: func() []Proxy {
				proxy := matching(3)
				proxy.OwnerUserID = &ownerUserID
				return []Proxy{proxy}
			}(),
		},
		{
			name: "wrong platform is not reused",
			cached: func() []Proxy {
				proxy := matching(4)
				proxy.Platform = PlatformGemini
				return []Proxy{proxy}
			}(),
		},
		{
			name: "required level is not reused for unknown CRS level",
			cached: func() []Proxy {
				proxy := matching(5)
				proxy.RequiredAccountLevel = AccountLevelPro
				return []Proxy{proxy}
			}(),
		},
		{
			name: "expired active proxy is not reused",
			cached: func() []Proxy {
				proxy := matching(6)
				proxy.ExpiresAt = &past
				return []Proxy{proxy}
			}(),
		},
		{
			name: "inactive proxy is not reused",
			cached: func() []Proxy {
				proxy := matching(7)
				proxy.Status = StatusDisabled
				return []Proxy{proxy}
			}(),
		},
		{
			name: "later safe duplicate endpoint can be reused",
			cached: func() []Proxy {
				privateProxy := matching(8)
				privateProxy.OwnerUserID = &ownerUserID
				publicProxy := matching(9)
				publicProxy.Platform = PlatformOpenAI
				publicProxy.ExpiresAt = &future
				return []Proxy{privateProxy, publicProxy}
			}(),
			resolvedID: 9,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := planCRSProxy(
				true,
				test.cached,
				source,
				"crs-openai",
				NewProxyScope(PlatformOpenAI, AccountLevelUnknown),
				now,
			)

			require.NotNil(t, plan)
			if test.resolvedID > 0 {
				require.NotNil(t, plan.resolvedID)
				require.Equal(t, test.resolvedID, *plan.resolvedID)
				require.Nil(t, plan.pending)
				return
			}
			require.Nil(t, plan.resolvedID)
			require.NotNil(t, plan.pending)
			require.Nil(t, plan.pending.OwnerUserID)
			require.Empty(t, plan.pending.Platform)
			require.Empty(t, plan.pending.RequiredAccountLevel)
			require.Equal(t, StatusActive, plan.pending.Status)
		})
	}
}

func TestCRSCreateWithPendingProxyFailsClosedWithoutUnitOfWork(t *testing.T) {
	baseRepo := &crsSyncAccountRepoStub{}
	accountRepo := &crsProxyUOWMissingAccountRepoStub{AccountRepository: baseRepo}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(accountRepo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	cached := []Proxy{}
	plan := &crsProxyPlan{pending: &Proxy{Name: "pending", Status: StatusActive}}
	account := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"crs_account_id": "missing-uow"}}

	err := svc.createAccountWithProxyPlan(context.Background(), &cached, plan, account)

	require.ErrorIs(t, err, ErrCRSProxyAccountUnitOfWorkUnavailable)
	require.Zero(t, proxyRepo.createCalls)
	require.Empty(t, baseRepo.creates)
	require.Empty(t, cached)
	require.Nil(t, plan.resolvedID)
	require.NotNil(t, plan.pending)
	require.Nil(t, account.ProxyID)
}

func TestCRSCreateWithPendingProxyPublishesOnlyAfterSuccessfulUnitOfWork(t *testing.T) {
	createErr := errors.New("account insert failed")
	accountRepo := &crsSyncAccountRepoStub{createErr: createErr}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(accountRepo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	cached := []Proxy{}
	plan := &crsProxyPlan{pending: &Proxy{Name: "pending", Status: StatusActive}}
	account := &Account{Platform: PlatformAnthropic, Extra: map[string]any{"crs_account_id": "rollback"}}

	err := svc.createAccountWithProxyPlan(context.Background(), &cached, plan, account)

	require.ErrorIs(t, err, createErr)
	require.Equal(t, 1, accountRepo.uowCalls)
	require.Equal(t, 1, proxyRepo.createCalls)
	require.Empty(t, cached, "rolled-back proxy must not be published to the shared cache")
	require.Nil(t, plan.resolvedID)
	require.NotNil(t, plan.pending)
	require.Zero(t, plan.pending.ID, "repository-generated ID must stay on the staged copy")
	require.Nil(t, account.ProxyID)
}

func TestCRSSyncRedactsSensitiveProxyFailureInInitialResult(t *testing.T) {
	server := newCRSSyncServer(t, func(int) map[string]any {
		return crsConsoleExportPayload(
			crsConsoleExportAccount("redacted-error", "Redacted error", "10.0.4.1"),
		)
	})
	defer server.Close()

	repo := &crsSyncAccountRepoStub{}
	proxyRepo := &crsSyncProxyRepoStub{
		createErr: errors.New(
			"proxy failed: access_token=visible-secret password=hunter2",
		),
	}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
		SyncProxies:  true,
		PreviewToken: token,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Len(t, result.Items, 1)
	require.NotContains(t, result.Items[0].Error, "visible-secret")
	require.NotContains(t, result.Items[0].Error, "hunter2")
	require.Contains(t, result.Items[0].Error, "access_token=***")
	require.Contains(t, result.Items[0].Error, "password=***")
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSSyncFailsFastWhenActiveProxyListCannotBeLoaded(t *testing.T) {
	server := newCRSSyncServer(t, func(int) map[string]any {
		return crsConsoleExportPayload(
			crsConsoleExportAccount("proxy-list", "Proxy list", "10.0.2.1"),
		)
	})
	defer server.Close()

	repo := &crsSyncAccountRepoStub{}
	listErr := errors.New("proxy list unavailable")
	proxyRepo := &crsSyncProxyRepoStub{listErr: listErr}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)

	_, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
		SyncProxies:  true,
		PreviewToken: token,
	})

	require.ErrorIs(t, err, listErr)
	require.Equal(t, 1, proxyRepo.listCalls)
	require.Zero(t, proxyRepo.createCalls)
	require.Empty(t, repo.creates)
	require.Empty(t, repo.updates)
}

func TestCRSSyncRoomGuardFailureLeavesNoNewProxy(t *testing.T) {
	server := newCRSSyncServer(t, func(int) map[string]any {
		return crsConsoleExportPayload(
			crsConsoleExportAccount("guarded", "Guarded", "10.0.3.1"),
		)
	})
	defer server.Close()

	listingID := int64(93)
	account := &Account{
		ID:                        51,
		Name:                      "Before",
		Credentials:               map[string]any{"api_key": "before"},
		Extra:                     map[string]any{"crs_account_id": "guarded"},
		UpdatedAt:                 time.Date(2026, time.July, 27, 8, 0, 0, 0, time.UTC),
		AccountShareModeListingID: &listingID,
	}
	baseRepo := &crsSyncAccountRepoStub{
		accounts: map[string]*Account{"guarded": account},
		preview: []CRSAccountPreviewSnapshot{{
			CRSAccountID:   "guarded",
			LocalAccountID: account.ID,
			RoomBindings: []CRSAccountRoomBindingSnapshot{{
				ListingID:  listingID,
				RowVersion: 4,
			}},
		}},
	}
	repo := &crsSyncGuardedAccountRepoStub{
		crsSyncAccountRepoStub: baseRepo,
		guardErr:               ErrAccountMutationVersionConflict,
	}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	token := previewCRSSyncToken(t, svc, server.URL, 91)

	result, err := svc.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:      server.URL,
		Username:     "admin",
		Password:     "secret",
		ActorAdminID: 91,
		SyncProxies:  true,
		PreviewToken: token,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Len(t, repo.requests, 1)
	require.Zero(t, proxyRepo.createCalls)
	require.Empty(t, baseRepo.updates)
	require.Nil(t, account.ProxyID)
}

func TestCRSExistingUpdatePublishesPendingProxyOnlyAfterGuardCommit(t *testing.T) {
	commitErr := errors.New("mutation guard commit failed")
	account := &Account{
		ID:        51,
		Platform:  PlatformAnthropic,
		Extra:     map[string]any{"crs_account_id": "commit-failure"},
		UpdatedAt: time.Date(2026, time.July, 27, 8, 0, 0, 0, time.UTC),
	}
	baseRepo := &crsSyncAccountRepoStub{}
	repo := &crsSyncGuardedAccountRepoStub{
		crsSyncAccountRepoStub: baseRepo,
		afterMutateErr:         commitErr,
	}
	proxyRepo := &crsSyncProxyRepoStub{}
	svc := NewCRSSyncService(repo, proxyRepo, nil, nil, nil, newCRSSyncTestConfig())
	cached := []Proxy{}
	plan := &crsProxyPlan{pending: &Proxy{Name: "pending", Status: StatusActive}}

	err := svc.updateExistingAccountWithProxy(
		context.Background(),
		SyncFromCRSInput{ActorAdminID: 91},
		account,
		&cached,
		plan,
	)

	require.ErrorIs(t, err, commitErr)
	require.Equal(t, 1, proxyRepo.createCalls)
	require.Len(t, baseRepo.updates, 1, "mutation ran before the simulated commit failure")
	require.Nil(t, account.ProxyID)
	require.Empty(t, cached)
	require.Nil(t, plan.resolvedID)
	require.NotNil(t, plan.pending)
	require.Zero(t, plan.pending.ID)
}

func crsConsoleExportAccount(id, name, proxyHost string) map[string]any {
	account := map[string]any{
		"kind":               "claude-console",
		"id":                 id,
		"name":               name,
		"isActive":           true,
		"schedulable":        true,
		"priority":           10,
		"status":             StatusActive,
		"maxConcurrentTasks": 3,
		"credentials":        map[string]any{"api_key": "key-" + id},
	}
	if proxyHost != "" {
		account["proxy"] = map[string]any{
			"protocol": "http",
			"host":     proxyHost,
			"port":     8080,
			"username": "proxy-user",
			"password": "proxy-password",
		}
	}
	return account
}

func crsConsoleExportPayload(accounts ...map[string]any) map[string]any {
	return map[string]any{
		"success": true,
		"data": map[string]any{
			"claudeConsoleAccounts": accounts,
		},
	}
}

func newCRSSyncExistingAccountsServer(t *testing.T) *httptest.Server {
	t.Helper()
	exportPayload := map[string]any{
		"success": true,
		"data": map[string]any{
			"claudeAccounts": []map[string]any{{
				"kind":        "claude",
				"id":          "claude",
				"name":        "Claude",
				"authType":    AccountTypeSetupToken,
				"isActive":    true,
				"schedulable": true,
				"priority":    10,
				"status":      StatusActive,
				"credentials": map[string]any{"access_token": "claude-token"},
			}},
			"claudeConsoleAccounts": []map[string]any{{
				"kind":               "claude-console",
				"id":                 "claude-console",
				"name":               "Claude Console",
				"isActive":           true,
				"schedulable":        true,
				"priority":           11,
				"status":             StatusActive,
				"maxConcurrentTasks": 4,
				"credentials":        map[string]any{"api_key": "claude-key"},
			}},
			"openaiOAuthAccounts": []map[string]any{{
				"kind":        "openai-oauth",
				"id":          "openai-oauth",
				"name":        "OpenAI OAuth",
				"isActive":    true,
				"schedulable": true,
				"priority":    12,
				"status":      StatusActive,
				"credentials": map[string]any{"access_token": "openai-token"},
			}},
			"openaiResponsesAccounts": []map[string]any{{
				"kind":        "openai-responses",
				"id":          "openai-responses",
				"name":        "OpenAI Responses",
				"isActive":    true,
				"schedulable": true,
				"priority":    13,
				"status":      StatusActive,
				"credentials": map[string]any{"api_key": "openai-key"},
			}},
			"geminiOAuthAccounts": []map[string]any{{
				"kind":        "gemini-oauth",
				"id":          "gemini-oauth",
				"name":        "Gemini OAuth",
				"isActive":    true,
				"schedulable": true,
				"priority":    14,
				"status":      StatusActive,
				"credentials": map[string]any{"refresh_token": "gemini-refresh"},
			}},
			"geminiApiKeyAccounts": []map[string]any{{
				"kind":        "gemini-apikey",
				"id":          "gemini-apikey",
				"name":        "Gemini API Key",
				"isActive":    true,
				"schedulable": true,
				"priority":    15,
				"status":      StatusActive,
				"credentials": map[string]any{"api_key": "gemini-key"},
			}},
		},
	}

	return newCRSSyncServer(t, func(int) map[string]any {
		return exportPayload
	})
}

func newCRSSyncServer(
	t *testing.T,
	exportPayload func(call int) map[string]any,
) *httptest.Server {
	t.Helper()
	var exportCalls atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/web/auth/login":
			if r.Method != http.MethodPost {
				t.Errorf("login method = %s, want POST", r.Method)
				http.Error(w, "invalid method", http.StatusMethodNotAllowed)
				return
			}
			if err := json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"token":   "admin-token",
			}); err != nil {
				t.Errorf("encode login response: %v", err)
			}
		case "/admin/sync/export-accounts":
			if r.Method != http.MethodGet {
				t.Errorf("export method = %s, want GET", r.Method)
				http.Error(w, "invalid method", http.StatusMethodNotAllowed)
				return
			}
			if authorization := r.Header.Get("Authorization"); authorization != "Bearer admin-token" {
				t.Errorf("authorization = %q, want bearer admin token", authorization)
				http.Error(w, "invalid authorization", http.StatusUnauthorized)
				return
			}
			call := int(exportCalls.Add(1))
			if err := json.NewEncoder(w).Encode(exportPayload(call)); err != nil {
				t.Errorf("encode export response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
}
