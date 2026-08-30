package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// recordingMutationGuardRepo keeps the service contract tests independent from
// Ent/SQL while still proving that every broad write receives the guard marker
// and that Update and BindGroups share one callback.
type recordingMutationGuardRepo struct {
	AccountRepository
	requests      []AccountMutationGuardRequest
	guardErr      error
	updateCalls   int
	bindCalls     int
	updateGuarded []bool
	bindGuarded   []bool
}

func (r *recordingMutationGuardRepo) WithAccountMutationGuard(
	ctx context.Context,
	request AccountMutationGuardRequest,
	mutate func(context.Context) error,
) error {
	r.requests = append(r.requests, request)
	if r.guardErr != nil {
		return r.guardErr
	}
	return mutate(WithAccountMutationGuardContext(ctx))
}

func (r *recordingMutationGuardRepo) Update(ctx context.Context, account *Account) error {
	r.updateCalls++
	r.updateGuarded = append(r.updateGuarded, AccountMutationGuardActive(ctx))
	return r.AccountRepository.Update(ctx, account)
}

func (r *recordingMutationGuardRepo) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	r.bindCalls++
	r.bindGuarded = append(r.bindGuarded, AccountMutationGuardActive(ctx))
	return r.AccountRepository.BindGroups(ctx, accountID, groupIDs)
}

func (r *recordingMutationGuardRepo) IsAccountShareModeListingAccount(ctx context.Context, accountID int64) (bool, error) {
	checker, ok := r.AccountRepository.(interface {
		IsAccountShareModeListingAccount(context.Context, int64) (bool, error)
	})
	if !ok {
		return false, nil
	}
	return checker.IsAccountShareModeListingAccount(ctx, accountID)
}

func mutationSafetyAccount(id, ownerID int64) *Account {
	return &Account{
		ID:           id,
		Name:         "safety-test",
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelPlus,
		OwnerUserID:  &ownerID,
		Status:       StatusActive,
		Schedulable:  true,
		Concurrency:  1,
		Priority:     1,
		ShareMode:    AccountShareModePrivate,
		ShareStatus:  AccountShareStatusApproved,
		GroupIDs:     []int64{99},
		Credentials:  map[string]any{"access_token": "token"},
		Extra: map[string]any{
			"quota_weekly_limit":    50.0,
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
		},
		UpdatedAt: time.Now().UTC(),
	}
}

func TestAccountServiceUpdateUsesAdminMutationGuardWithoutForgedAdminActor(t *testing.T) {
	ownerID := int64(101)
	account := mutationSafetyAccount(1, ownerID)
	base := &ownedAccountDuplicateRepoStub{getByIDAccounts: map[int64]*Account{account.ID: account}}
	repo := &recordingMutationGuardRepo{AccountRepository: base}
	svc := &AccountService{accountRepo: repo}

	updated, err := svc.Update(context.Background(), account.ID, UpdateAccountRequest{Name: mutationSafetyStringPtr("renamed")})

	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Name)
	require.Len(t, repo.requests, 1)
	require.Equal(t, AccountMutationIntentAdmin, repo.requests[0].Intent)
	require.False(t, repo.requests[0].ActorIsAdmin)
	require.Equal(t, account.UpdatedAt, repo.requests[0].Targets[0].ExpectedUpdatedAt)
	require.Equal(t, []int64{99}, repo.requests[0].Targets[0].GroupIDs)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, []bool{true}, repo.updateGuarded)
}

func TestAccountServiceMarkOwnedPublicSharePendingUpdatesAndBindsInsideGuard(t *testing.T) {
	ownerID := int64(101)
	account := mutationSafetyAccount(2, ownerID)
	base := &ownedAccountDuplicateRepoStub{getByIDAccounts: map[int64]*Account{account.ID: account}}
	repo := &recordingMutationGuardRepo{AccountRepository: base}
	svc := &AccountService{
		accountRepo: repo,
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 199, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}

	updated, err := svc.MarkOwnedPublicSharePending(context.Background(), ownerID, account.ID, "review")

	require.NoError(t, err)
	require.Equal(t, AccountShareModePublic, updated.ShareMode)
	require.Equal(t, AccountShareStatusPending, updated.ShareStatus)
	require.Equal(t, []int64{199}, updated.GroupIDs)
	require.Len(t, repo.requests, 1)
	require.Equal(t, AccountMutationIntentOwner, repo.requests[0].Intent)
	require.Equal(t, ownerID, repo.requests[0].ActorUserID)
	require.Equal(t, []int64{199}, repo.requests[0].Targets[0].GroupIDs)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.bindCalls)
	require.Equal(t, []bool{true}, repo.updateGuarded)
	require.Equal(t, []bool{true}, repo.bindGuarded)
}

func TestAccountServiceAutoRepairUsesGuardedFreshAccountSnapshot(t *testing.T) {
	ownerID := int64(101)
	proxyID := int64(777)
	fallbackProxyID := int64(778)
	updatedAt := time.Date(2026, 8, 31, 8, 30, 0, 0, time.UTC)
	resetAt := time.Now().UTC().Add(24 * time.Hour)
	account := &Account{
		ID:                    4,
		Platform:              PlatformOpenAI,
		Type:                  AccountTypeOAuth,
		AccountLevel:          AccountLevelPlus,
		OwnerUserID:           &ownerID,
		ShareMode:             AccountShareModePrivate,
		ShareStatus:           AccountShareStatusApproved,
		Status:                StatusActive,
		Schedulable:           true,
		ProxyID:               &proxyID,
		ProxyFallbackOriginID: &fallbackProxyID,
		GroupIDs:              []int64{55},
		UpdatedAt:             updatedAt,
		Extra: map[string]any{
			"quota_weekly_limit":    50.0,
			"codex_7d_used_percent": 100.0,
			"codex_7d_reset_at":     resetAt.Format(time.RFC3339),
		},
	}
	base := &ownedAccountDuplicateRepoStub{
		getByIDAccounts: map[int64]*Account{account.ID: account},
	}
	repo := &recordingMutationGuardRepo{AccountRepository: base}
	svc := &AccountService{
		accountRepo: repo,
		groupRepo: &ownedPublicShareGroupRepoStub{groups: []Group{
			{ID: 299, Name: "FREE共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelFree},
		}},
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 199, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}

	updated, repaired, err := svc.AutoRepairSuspectedOpenAIFreeAccount(context.Background(), account.ID, 60, "guarded repair")

	require.NoError(t, err)
	require.True(t, repaired)
	require.Len(t, repo.requests, 1)
	target := repo.requests[0].Targets[0]
	require.Equal(t, updatedAt, target.ExpectedUpdatedAt, "guard must use the freshly loaded account version")
	require.Equal(t, AccountLevelFree, target.After.AccountLevel)
	require.NotNil(t, target.After.ProxyID)
	require.Equal(t, proxyID, *target.After.ProxyID, "repair must preserve the current proxy binding for guard revalidation")
	require.NotNil(t, target.After.ProxyFallbackOriginID)
	require.Equal(t, fallbackProxyID, *target.After.ProxyFallbackOriginID)
	require.Equal(t, []int64{199, 299}, target.GroupIDs)
	require.Equal(t, 1, repo.updateCalls)
	require.Equal(t, 1, repo.bindCalls)
	require.Equal(t, []bool{true}, repo.updateGuarded)
	require.Equal(t, []bool{true}, repo.bindGuarded)
	require.Equal(t, AccountLevelFree, updated.AccountLevel)
	require.Equal(t, []int64{199, 299}, updated.GroupIDs)
}

func TestAccountServiceMutationGuardFailureDoesNotWritePendingState(t *testing.T) {
	ownerID := int64(101)
	account := mutationSafetyAccount(3, ownerID)
	base := &ownedAccountDuplicateRepoStub{getByIDAccounts: map[int64]*Account{account.ID: account}}
	repo := &recordingMutationGuardRepo{
		AccountRepository: base,
		guardErr:          errors.New("guard rejected"),
	}
	svc := &AccountService{
		accountRepo: repo,
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 199, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}

	updated, err := svc.MarkOwnedPublicSharePending(context.Background(), ownerID, account.ID, "review")

	require.Nil(t, updated)
	require.ErrorContains(t, err, "guard rejected")
	require.Zero(t, repo.updateCalls)
	require.Zero(t, repo.bindCalls)
	require.Equal(t, AccountShareModePrivate, base.getByIDAccounts[account.ID].ShareMode)
}

func TestAccountServiceUpdateStatusUsesNarrowCapability(t *testing.T) {
	base := &ownedAccountDuplicateRepoStub{}
	repo := &statusRecordingAccountRepo{AccountRepository: base}
	svc := &AccountService{accountRepo: repo}

	err := svc.UpdateStatus(context.Background(), 7, StatusDisabled, "manual")

	require.NoError(t, err)
	require.Equal(t, int64(7), repo.accountID)
	require.Equal(t, StatusDisabled, repo.status)
	require.Equal(t, "manual", repo.errorMessage)
	require.Zero(t, repo.updateCalls)
}

func TestAccountServiceUpdateStatusFailsClosedWithoutNarrowCapability(t *testing.T) {
	svc := &AccountService{accountRepo: &ownedAccountDuplicateRepoStub{}}

	err := svc.UpdateStatus(context.Background(), 7, StatusDisabled, "manual")

	require.ErrorIs(t, err, ErrAccountMutationGuardUnavailable)
}

type statusRecordingAccountRepo struct {
	AccountRepository
	accountID    int64
	status       string
	errorMessage string
	updateCalls  int
}

func (r *statusRecordingAccountRepo) Update(ctx context.Context, account *Account) error {
	r.updateCalls++
	return r.AccountRepository.Update(ctx, account)
}

func (r *statusRecordingAccountRepo) UpdateStatusAndError(_ context.Context, id int64, status, errorMessage string) error {
	r.accountID = id
	r.status = status
	r.errorMessage = errorMessage
	return nil
}

func mutationSafetyStringPtr(value string) *string {
	return &value
}
