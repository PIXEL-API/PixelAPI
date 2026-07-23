//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoAvailableOpenAISelectionErrorWrapsSentinel(t *testing.T) {
	t.Parallel()

	for _, requestedModel := range []string{"", "grok-4"} {
		err := noAvailableOpenAISelectionError(requestedModel, false)
		require.True(t, errors.Is(err, ErrNoAvailableAccounts))
		require.Contains(t, err.Error(), "no available OpenAI accounts")
	}
	require.ErrorIs(t, noAvailableOpenAISelectionError("grok-4", true), ErrNoAvailableCompactAccounts)
}

func TestAccountServiceOwnedCreateRejectsGrokManagedExtra(t *testing.T) {
	t.Parallel()

	for _, key := range []string{GrokMediaEligibleExtraKey, grokBillingExtraKey} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			svc := &AccountService{}
			_, err := svc.CreateOwned(context.Background(), 101, CreateAccountRequest{
				Extra: map[string]any{key: true},
			})
			require.ErrorIs(t, err, ErrOwnedAccountGrokManagedExtraNotAllowed)
		})
	}
}

func TestAccountServiceOwnedUpdatePreservesUnchangedGrokManagedExtra(t *testing.T) {
	t.Parallel()

	repo := newOwnedAgentIdentityRepoStub()
	svc, _ := newOwnedAgentIdentityService(repo)
	created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-managed", "member-managed", "runtime-managed", "team"))
	require.NoError(t, err)

	snapshot := map[string]any{"plan": "SuperGrok", "status_code": float64(200)}
	repo.accounts[created.Account.ID].Extra = map[string]any{
		GrokMediaEligibleExtraKey: true,
		grokBillingExtraKey:       snapshot,
		"old_config":              "remove-me",
	}

	t.Run("omitted values are preserved", func(t *testing.T) {
		extra := map[string]any{"custom": "next"}
		updated, updateErr := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{Extra: &extra})
		require.NoError(t, updateErr)
		require.Equal(t, true, updated.Extra[GrokMediaEligibleExtraKey])
		require.Equal(t, snapshot, updated.Extra[grokBillingExtraKey])
		require.Equal(t, "next", updated.Extra["custom"])
	})

	t.Run("unchanged echoed values are accepted", func(t *testing.T) {
		extra := map[string]any{
			GrokMediaEligibleExtraKey: true,
			grokBillingExtraKey:       map[string]any{"plan": "SuperGrok", "status_code": float64(200)},
			"custom":                  "echo",
		}
		updated, updateErr := svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{Extra: &extra})
		require.NoError(t, updateErr)
		require.Equal(t, true, updated.Extra[GrokMediaEligibleExtraKey])
		require.Equal(t, snapshot, updated.Extra[grokBillingExtraKey])
	})
}

func TestAccountServiceOwnedUpdateRejectsGrokManagedExtraMutation(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		extra map[string]any
	}{
		{name: "override mutation", extra: map[string]any{GrokMediaEligibleExtraKey: false}},
		{name: "snapshot deletion", extra: map[string]any{grokBillingExtraKey: nil}},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repo := newOwnedAgentIdentityRepoStub()
			svc, _ := newOwnedAgentIdentityService(repo)
			created, err := svc.ImportOwnedWithResult(context.Background(), 101, ownedAgentIdentityImportRequest(t, "team-"+testCase.name, "member", "runtime", "team"))
			require.NoError(t, err)
			repo.accounts[created.Account.ID].Extra = map[string]any{
				GrokMediaEligibleExtraKey: true,
				grokBillingExtraKey:       map[string]any{"status_code": float64(200)},
			}
			updatesBefore := repo.updateCount

			_, err = svc.UpdateOwned(context.Background(), 101, created.Account.ID, UpdateAccountRequest{Extra: &testCase.extra})

			require.ErrorIs(t, err, ErrOwnedAccountGrokManagedExtraNotAllowed)
			require.Equal(t, updatesBefore, repo.updateCount)
		})
	}
}

func TestAccountServiceOwnedBulkRejectsGrokManagedExtra(t *testing.T) {
	t.Parallel()

	for _, key := range []string{GrokMediaEligibleExtraKey, grokBillingExtraKey} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			svc := &AccountService{}
			_, err := svc.BulkUpdateOwned(context.Background(), 101, &BulkUpdateOwnedAccountsInput{
				AccountIDs: []int64{1},
				Extra:      map[string]any{key: nil},
			})
			require.ErrorIs(t, err, ErrOwnedAccountGrokManagedExtraNotAllowed)
		})
	}
}

func TestAdminAccountCreateValidatesGrokManagedExtra(t *testing.T) {
	t.Parallel()

	svc := &adminServiceImpl{}
	for _, testCase := range []struct {
		name    string
		extra   map[string]any
		wantErr error
	}{
		{name: "boolean override", extra: map[string]any{GrokMediaEligibleExtraKey: true}},
		{name: "null override", extra: map[string]any{GrokMediaEligibleExtraKey: nil}},
		{name: "invalid override", extra: map[string]any{GrokMediaEligibleExtraKey: "true"}, wantErr: ErrGrokMediaEligibilityOverrideInvalid},
		{name: "billing snapshot", extra: map[string]any{grokBillingExtraKey: map[string]any{}}, wantErr: ErrGrokBillingSnapshotManaged},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			account, _, err := svc.prepareAccountCreate(context.Background(), &CreateAccountInput{
				Platform:             PlatformGrok,
				Type:                 AccountTypeOAuth,
				Extra:                testCase.extra,
				SkipDefaultGroupBind: true,
			})
			if testCase.wantErr != nil {
				require.ErrorIs(t, err, testCase.wantErr)
				require.Nil(t, account)
				return
			}
			require.NoError(t, err)
			require.Contains(t, account.Extra, GrokMediaEligibleExtraKey)
		})
	}
}

func TestAdminAccountUpdatePreservesGrokBillingSnapshot(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name              string
		extra             map[string]any
		wantMediaOverride any
	}{
		{name: "ordinary update preserves override", extra: map[string]any{"custom": "next"}, wantMediaOverride: true},
		{name: "explicit null clears override", extra: map[string]any{GrokMediaEligibleExtraKey: nil, "custom": "next"}, wantMediaOverride: nil},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			repo := newOwnedAgentIdentityRepoStub()
			repo.accounts[1] = &Account{
				ID:           1,
				Platform:     PlatformGrok,
				Type:         AccountTypeOAuth,
				AccountLevel: AccountLevelUnknown,
				Concurrency:  1,
				Status:       StatusActive,
				Schedulable:  true,
				Extra: map[string]any{
					GrokMediaEligibleExtraKey: true,
					grokBillingExtraKey:       map[string]any{"status_code": float64(200)},
					"old_config":              "replace-me",
				},
			}
			svc := &adminServiceImpl{accountRepo: repo}

			updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: testCase.extra})

			require.NoError(t, err)
			require.Equal(t, testCase.wantMediaOverride, updated.Extra[GrokMediaEligibleExtraKey])
			require.Equal(t, map[string]any{"status_code": float64(200)}, updated.Extra[grokBillingExtraKey])
			require.Equal(t, "next", updated.Extra["custom"])
		})
	}
}

func TestAdminAccountUpdateAndBulkRejectGrokBillingSnapshotInjection(t *testing.T) {
	t.Parallel()

	repo := newOwnedAgentIdentityRepoStub()
	repo.accounts[1] = &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		Extra: map[string]any{grokBillingExtraKey: map[string]any{"status_code": float64(200)}},
	})
	require.ErrorIs(t, err, ErrGrokBillingSnapshotManaged)
	require.Zero(t, repo.updateCount)

	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		Extra:      map[string]any{grokBillingExtraKey: nil},
	})
	require.ErrorIs(t, err, ErrGrokBillingSnapshotManaged)
	require.Zero(t, repo.bulkUpdateCalls)

	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		Extra:      map[string]any{GrokMediaEligibleExtraKey: 1},
	})
	require.ErrorIs(t, err, ErrGrokMediaEligibilityOverrideInvalid)
	require.Zero(t, repo.bulkUpdateCalls)
}
