//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type adminDuplicateAccountRepoStub struct {
	ownedAccountDuplicateRepoStub
	accounts        map[int64]*Account
	nextID          int64
	atomicCreateErr error
	createdGroups   map[int64][]AccountGroup
}

func newAdminDuplicateAccountRepoStub(source *Account) *adminDuplicateAccountRepoStub {
	repo := &adminDuplicateAccountRepoStub{
		accounts:      make(map[int64]*Account),
		nextID:        100,
		createdGroups: make(map[int64][]AccountGroup),
	}
	repo.getByIDAccounts = repo.accounts
	repo.getByIDsAccounts = repo.accounts
	if source != nil {
		stored := *source
		repo.accounts[source.ID] = &stored
	}
	return repo
}

func (s *adminDuplicateAccountRepoStub) FindByExtraField(_ context.Context, key string, value any) ([]Account, error) {
	wanted, ok := value.(string)
	if !ok {
		return nil, nil
	}
	matches := make([]Account, 0, 1)
	for _, account := range s.accounts {
		if actual, ok := account.Extra[key].(string); ok && actual == wanted {
			matches = append(matches, *account)
		}
	}
	return matches, nil
}

func (s *adminDuplicateAccountRepoStub) CreateWithAccountGroups(_ context.Context, account *Account, groups []AccountGroup) error {
	if s.atomicCreateErr != nil {
		return s.atomicCreateErr
	}
	s.nextID++
	account.ID = s.nextID
	storedGroups := make([]AccountGroup, len(groups))
	copy(storedGroups, groups)
	for i := range storedGroups {
		storedGroups[i].AccountID = account.ID
	}
	account.AccountGroups = storedGroups
	stored := *account
	s.accounts[account.ID] = &stored
	s.createdGroups[account.ID] = storedGroups
	return nil
}

func TestAdminDuplicateAccountCopiesStaticConfigurationAndResetsRuntime(t *testing.T) {
	source := &Account{
		ID:            7,
		Name:          "primary",
		Platform:      PlatformAnthropic,
		AccountLevel:  AccountLevelUnknown,
		Type:          AccountTypeAPIKey,
		Concurrency:   6,
		Priority:      42,
		Status:        StatusError,
		ErrorMessage:  "upstream failed",
		Schedulable:   true,
		ShareMode:     AccountShareModePrivate,
		ShareStatus:   AccountShareStatusApproved,
		Credentials:   map[string]any{"api_key": "secret", "nested": map[string]any{"region": "us-east-1"}},
		Extra:         map[string]any{"config": map[string]any{"enabled": true}, "quota_limit": 1000, "quota_used": 300, "model_rate_limits": map[string]any{"m": "limited"}, "crs_account_id": "remote-7", "codex_5h_used_percent": 80},
		AccountGroups: nil,
	}
	repo := newAdminDuplicateAccountRepoStub(source)
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}

	duplicated, err := svc.DuplicateAccount(context.Background(), source.ID, "admin:9", "operation-1")
	require.NoError(t, err)
	require.Equal(t, "primary (Copy)", duplicated.Name)
	require.Equal(t, StatusActive, duplicated.Status)
	require.False(t, duplicated.Schedulable)
	require.Empty(t, duplicated.ErrorMessage)
	require.Nil(t, duplicated.OwnerUserID)
	require.Nil(t, duplicated.SharePolicyID)
	require.Equal(t, AccountShareModePrivate, duplicated.ShareMode)
	require.Equal(t, AccountShareStatusApproved, duplicated.ShareStatus)
	require.Equal(t, source.Credentials, duplicated.Credentials)
	require.Equal(t, float64(1000), duplicated.Extra["quota_limit"])
	require.NotContains(t, duplicated.Extra, "quota_used")
	require.NotContains(t, duplicated.Extra, "model_rate_limits")
	require.NotContains(t, duplicated.Extra, "crs_account_id")
	require.NotContains(t, duplicated.Extra, "codex_5h_used_percent")
	require.NotEmpty(t, duplicated.Extra[duplicateAccountOperationIDExtraKey])

	duplicated.Credentials["nested"].(map[string]any)["region"] = "changed"
	duplicated.Extra["config"].(map[string]any)["enabled"] = false
	require.Equal(t, "us-east-1", source.Credentials["nested"].(map[string]any)["region"])
	require.Equal(t, true, source.Extra["config"].(map[string]any)["enabled"])

	replayed, err := svc.DuplicateAccount(context.Background(), source.ID, "admin:9", "operation-1")
	require.NoError(t, err)
	require.Equal(t, duplicated.ID, replayed.ID)
	otherAdmin, err := svc.DuplicateAccount(context.Background(), source.ID, "admin:10", "operation-1")
	require.NoError(t, err)
	require.NotEqual(t, duplicated.ID, otherAdmin.ID)

	secondSource := *source
	secondSource.ID = 8
	secondSource.Name = "secondary"
	repo.accounts[secondSource.ID] = &secondSource
	otherSource, err := svc.DuplicateAccount(context.Background(), secondSource.ID, "admin:9", "operation-1")
	require.NoError(t, err)
	require.NotEqual(t, duplicated.ID, otherSource.ID)
	require.Equal(t, "secondary (Copy)", otherSource.Name)
}

func TestAdminDuplicateAccountRejectsUnsafeSources(t *testing.T) {
	tests := []struct {
		name       string
		account    Account
		wantReason string
	}{
		{name: "oauth", account: Account{Type: AccountTypeOAuth}, wantReason: "ACCOUNT_DUPLICATE_CREDENTIAL_TYPE_UNSUPPORTED"},
		{name: "setup token", account: Account{Type: AccountTypeSetupToken}, wantReason: "ACCOUNT_DUPLICATE_CREDENTIAL_TYPE_UNSUPPORTED"},
		{name: "unknown", account: Account{Type: "cookie"}, wantReason: "ACCOUNT_DUPLICATE_CREDENTIAL_TYPE_UNSUPPORTED"},
		{name: "owned", account: Account{Type: AccountTypeAPIKey, OwnerUserID: int64Pointer(3)}, wantReason: "ACCOUNT_DUPLICATE_SHARED_IDENTITY_UNSUPPORTED"},
		{name: "public share", account: Account{Type: AccountTypeAPIKey, ShareMode: AccountShareModePublic}, wantReason: "ACCOUNT_DUPLICATE_SHARED_IDENTITY_UNSUPPORTED"},
		{name: "share policy", account: Account{Type: AccountTypeAPIKey, SharePolicyID: int64Pointer(4)}, wantReason: "ACCOUNT_DUPLICATE_SHARED_IDENTITY_UNSUPPORTED"},
		{name: "share listing", account: Account{Type: AccountTypeAPIKey, AccountShareModeListingID: int64Pointer(5)}, wantReason: "ACCOUNT_DUPLICATE_SHARED_IDENTITY_UNSUPPORTED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.account.ID = 1
			tt.account.Name = "source"
			tt.account.Platform = PlatformAnthropic
			tt.account.Status = StatusActive
			tt.account.Credentials = map[string]any{"api_key": "secret"}
			repo := newAdminDuplicateAccountRepoStub(&tt.account)
			svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}

			_, err := svc.DuplicateAccount(context.Background(), tt.account.ID, "admin:1", "op")
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Equal(t, tt.wantReason, infraerrors.Reason(err))
			require.Len(t, repo.accounts, 1)
		})
	}
}

func TestAdminDuplicateAccountAtomicFailureLeavesNoCopy(t *testing.T) {
	source := &Account{ID: 1, Name: "source", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	repo := newAdminDuplicateAccountRepoStub(source)
	repo.atomicCreateErr = errors.New("group binding failed")
	svc := &adminServiceImpl{accountRepo: repo, accountDuplicateRepo: repo}

	_, err := svc.DuplicateAccount(context.Background(), source.ID, "admin:1", "op")
	require.ErrorContains(t, err, "group binding failed")
	require.Len(t, repo.accounts, 1)
}

func TestDuplicateAccountExtraDiscardsEveryRuntimeKey(t *testing.T) {
	extra := make(map[string]any, len(duplicateAccountDiscardedExtraKeys)+1)
	for key := range duplicateAccountDiscardedExtraKeys {
		extra[key] = "runtime"
	}
	extra["reusable_config"] = map[string]any{"enabled": true}

	cloned, err := duplicateAccountExtra(extra)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"reusable_config": map[string]any{"enabled": true}}, cloned)
}

func TestDuplicateAccountGroupsPreservesExplicitPriority(t *testing.T) {
	groups, ids := duplicateAccountGroups(&Account{AccountGroups: []AccountGroup{{GroupID: 8, Priority: 40}, {GroupID: 2, Priority: 7}}})
	require.Equal(t, []int64{8, 2}, ids)
	require.Equal(t, []AccountGroup{{GroupID: 8, Priority: 40}, {GroupID: 2, Priority: 7}}, groups)
}

func TestDuplicateAccountNameStaysWithinSchemaLimit(t *testing.T) {
	name := duplicateAccountName(strings.Repeat("界", maxAccountNameRunes))
	require.Equal(t, maxAccountNameRunes, utf8.RuneCountInString(name))
	require.True(t, strings.HasSuffix(name, " (Copy)"))
}

func int64Pointer(value int64) *int64 {
	return &value
}
