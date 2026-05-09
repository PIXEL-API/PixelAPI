package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type ownedAccountGroupRepoStub struct {
	groupRepoNoop
	groups map[int64]*Group
}

func (s *ownedAccountGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	group := s.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	cp := *group
	return &cp, nil
}

type ownedAccountUserSubRepoStub struct {
	active map[int64]*UserSubscription
}

func (s *ownedAccountUserSubRepoStub) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	sub := s.active[groupID]
	if sub == nil || sub.UserID != userID {
		return nil, ErrSubscriptionNotFound
	}
	cp := *sub
	return &cp, nil
}

type ownedAccountUserRepoStub struct {
	user *User
	err  error
}

func (s *ownedAccountUserRepoStub) GetByID(_ context.Context, _ int64) (*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.user == nil {
		return nil, ErrUserNotFound
	}
	cp := *s.user
	return &cp, nil
}

type ownedPublicShareGroupRepoStub struct {
	groupRepoNoop
	groups []Group
}

func (s *ownedPublicShareGroupRepoStub) ListActiveByPlatform(_ context.Context, platform string) ([]Group, error) {
	out := make([]Group, 0, len(s.groups))
	for _, group := range s.groups {
		if group.Platform == platform && group.IsActive() {
			out = append(out, group)
		}
	}
	return out, nil
}

type ownedPublicSharePolicyRepoStub struct {
	policy *AccountSharePolicy
	err    error
}

func (s *ownedPublicSharePolicyRepoStub) ListAccountSharePolicies(context.Context, pagination.PaginationParams, AccountSharePolicyFilters) ([]AccountSharePolicy, *pagination.PaginationResult, error) {
	panic("unexpected ListAccountSharePolicies call")
}

func (s *ownedPublicSharePolicyRepoStub) GetAccountSharePolicyByID(context.Context, int64) (*AccountSharePolicy, error) {
	panic("unexpected GetAccountSharePolicyByID call")
}

func (s *ownedPublicSharePolicyRepoStub) ResolveEnabledAccountSharePolicy(context.Context, int64, *int64, string, *int64) (*AccountSharePolicy, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.policy == nil {
		return nil, nil
	}
	cp := *s.policy
	return &cp, nil
}

func (s *ownedPublicSharePolicyRepoStub) CreateAccountSharePolicy(context.Context, CreateAccountSharePolicyInput) (*AccountSharePolicy, error) {
	panic("unexpected CreateAccountSharePolicy call")
}

func (s *ownedPublicSharePolicyRepoStub) UpdateAccountSharePolicy(context.Context, int64, UpdateAccountSharePolicyInput) (*AccountSharePolicy, error) {
	panic("unexpected UpdateAccountSharePolicy call")
}

func (s *ownedPublicSharePolicyRepoStub) DeleteAccountSharePolicy(context.Context, int64) error {
	panic("unexpected DeleteAccountSharePolicy call")
}

type ownedPrivateGroupProvisionerStub struct {
	group          *Group
	err            error
	provisionErr   error
	provisionCalls int
}

func (s *ownedPrivateGroupProvisionerStub) ProvisionUserPrivateGroups(context.Context, int64) error {
	s.provisionCalls++
	return s.provisionErr
}

func (s *ownedPrivateGroupProvisionerStub) GetActiveUserPrivateGroup(context.Context, int64, string) (*Group, error) {
	if s.err != nil && s.provisionCalls == 0 {
		return nil, s.err
	}
	if s.group == nil {
		return nil, ErrGroupNotFound
	}
	cp := *s.group
	return &cp, nil
}

type ownedAccountDuplicateRepoStub struct {
	createdAccounts     []*Account
	updatedAccounts     []*Account
	bulkUpdateCalls     int
	boundAccountIDs     []int64
	getByIDAccounts     map[int64]*Account
	getByIDsAccounts    map[int64]*Account
	listOwnedByPlatform map[string][]Account
}

func (s *ownedAccountDuplicateRepoStub) Create(_ context.Context, account *Account) error {
	cp := *account
	s.createdAccounts = append(s.createdAccounts, &cp)
	return nil
}

func (s *ownedAccountDuplicateRepoStub) Update(_ context.Context, account *Account) error {
	cp := *account
	s.updatedAccounts = append(s.updatedAccounts, &cp)
	if s.getByIDAccounts != nil {
		stored := cp
		s.getByIDAccounts[account.ID] = &stored
	}
	if s.getByIDsAccounts != nil {
		stored := cp
		s.getByIDsAccounts[account.ID] = &stored
	}
	return nil
}

func (s *ownedAccountDuplicateRepoStub) BulkUpdate(_ context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	s.bulkUpdateCalls++
	return int64(len(ids)), nil
}

func (s *ownedAccountDuplicateRepoStub) BindGroups(_ context.Context, accountID int64, _ []int64) error {
	s.boundAccountIDs = append(s.boundAccountIDs, accountID)
	return nil
}

func (s *ownedAccountDuplicateRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	account := s.getByIDAccounts[id]
	if account == nil {
		return nil, ErrAccountNotFound
	}
	cp := *account
	return &cp, nil
}

func (s *ownedAccountDuplicateRepoStub) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	out := make([]*Account, 0, len(ids))
	for _, id := range ids {
		account := s.getByIDsAccounts[id]
		if account == nil {
			continue
		}
		cp := *account
		out = append(out, &cp)
	}
	return out, nil
}

func (s *ownedAccountDuplicateRepoStub) ListOwnedWithFilters(_ context.Context, ownerUserID int64, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	rows := s.listOwnedByPlatform[platform]
	filtered := make([]Account, 0, len(rows))
	for _, row := range rows {
		if row.OwnerUserID == nil || *row.OwnerUserID != ownerUserID {
			continue
		}
		if accountType != "" && row.Type != accountType {
			continue
		}
		filtered = append(filtered, row)
	}
	offset := params.Offset()
	limit := params.Limit()
	if offset >= len(filtered) {
		return nil, &pagination.PaginationResult{Total: int64(len(filtered))}, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], &pagination.PaginationResult{Total: int64(len(filtered))}, nil
}

func (s *ownedAccountDuplicateRepoStub) ExistsByID(context.Context, int64) (bool, error) {
	panic("unexpected ExistsByID call")
}

func (s *ownedAccountDuplicateRepoStub) GetByCRSAccountID(context.Context, string) (*Account, error) {
	panic("unexpected GetByCRSAccountID call")
}

func (s *ownedAccountDuplicateRepoStub) FindByExtraField(context.Context, string, any) ([]Account, error) {
	panic("unexpected FindByExtraField call")
}

func (s *ownedAccountDuplicateRepoStub) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	panic("unexpected ListCRSAccountIDs call")
}

func (s *ownedAccountDuplicateRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}

func (s *ownedAccountDuplicateRepoStub) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (s *ownedAccountDuplicateRepoStub) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (s *ownedAccountDuplicateRepoStub) ListByGroup(context.Context, int64) ([]Account, error) {
	panic("unexpected ListByGroup call")
}

func (s *ownedAccountDuplicateRepoStub) ListActive(context.Context) ([]Account, error) {
	panic("unexpected ListActive call")
}

func (s *ownedAccountDuplicateRepoStub) ListByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListByPlatform call")
}

func (s *ownedAccountDuplicateRepoStub) UpdateLastUsed(context.Context, int64) error {
	panic("unexpected UpdateLastUsed call")
}

func (s *ownedAccountDuplicateRepoStub) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	panic("unexpected BatchUpdateLastUsed call")
}

func (s *ownedAccountDuplicateRepoStub) SetError(context.Context, int64, string) error {
	panic("unexpected SetError call")
}

func (s *ownedAccountDuplicateRepoStub) ClearError(context.Context, int64) error {
	panic("unexpected ClearError call")
}

func (s *ownedAccountDuplicateRepoStub) SetSchedulable(context.Context, int64, bool) error {
	panic("unexpected SetSchedulable call")
}

func (s *ownedAccountDuplicateRepoStub) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	panic("unexpected AutoPauseExpiredAccounts call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulable(context.Context) ([]Account, error) {
	panic("unexpected ListSchedulable call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupID call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatform call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatform call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatforms call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatforms call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatform call")
}

func (s *ownedAccountDuplicateRepoStub) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatforms call")
}

func (s *ownedAccountDuplicateRepoStub) SetRateLimited(context.Context, int64, time.Time) error {
	panic("unexpected SetRateLimited call")
}

func (s *ownedAccountDuplicateRepoStub) SetModelRateLimit(context.Context, int64, string, time.Time) error {
	panic("unexpected SetModelRateLimit call")
}

func (s *ownedAccountDuplicateRepoStub) SetOverloaded(context.Context, int64, time.Time) error {
	panic("unexpected SetOverloaded call")
}

func (s *ownedAccountDuplicateRepoStub) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	panic("unexpected SetTempUnschedulable call")
}

func (s *ownedAccountDuplicateRepoStub) ClearTempUnschedulable(context.Context, int64) error {
	panic("unexpected ClearTempUnschedulable call")
}

func (s *ownedAccountDuplicateRepoStub) ClearRateLimit(context.Context, int64) error {
	panic("unexpected ClearRateLimit call")
}

func (s *ownedAccountDuplicateRepoStub) ClearAntigravityQuotaScopes(context.Context, int64) error {
	panic("unexpected ClearAntigravityQuotaScopes call")
}

func (s *ownedAccountDuplicateRepoStub) ClearModelRateLimits(context.Context, int64) error {
	panic("unexpected ClearModelRateLimits call")
}

func (s *ownedAccountDuplicateRepoStub) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	panic("unexpected UpdateSessionWindow call")
}

func (s *ownedAccountDuplicateRepoStub) UpdateExtra(context.Context, int64, map[string]any) error {
	panic("unexpected UpdateExtra call")
}

func (s *ownedAccountDuplicateRepoStub) IncrementQuotaUsed(context.Context, int64, float64) error {
	panic("unexpected IncrementQuotaUsed call")
}

func (s *ownedAccountDuplicateRepoStub) ResetQuotaUsed(context.Context, int64) error {
	panic("unexpected ResetQuotaUsed call")
}

func TestAccountServiceValidateOwnedAccountGroupBinding(t *testing.T) {
	t.Run("allows active standard group and deduplicates ids", func(t *testing.T) {
		svc := newOwnedAccountGroupValidationService(
			&User{ID: 101},
			map[int64]*Group{
				10: {ID: 10, Platform: PlatformOpenAI, Status: StatusActive},
			},
			nil,
		)

		groupIDs, err := svc.validateOwnedAccountGroupBinding(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, []int64{10, 10})

		require.NoError(t, err)
		require.Equal(t, []int64{10}, groupIDs)
	})

	t.Run("rejects platform mismatch", func(t *testing.T) {
		svc := newOwnedAccountGroupValidationService(
			&User{ID: 101},
			map[int64]*Group{
				10: {ID: 10, Platform: PlatformAnthropic, Status: StatusActive},
			},
			nil,
		)

		_, err := svc.validateOwnedAccountGroupBinding(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, []int64{10})

		require.ErrorIs(t, err, ErrOwnedAccountGroupPlatformMismatch)
	})

	t.Run("rejects exclusive group without user permission", func(t *testing.T) {
		svc := newOwnedAccountGroupValidationService(
			&User{ID: 101, AllowedGroups: []int64{20}},
			map[int64]*Group{
				10: {ID: 10, Platform: PlatformOpenAI, Status: StatusActive, IsExclusive: true},
			},
			nil,
		)

		_, err := svc.validateOwnedAccountGroupBinding(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, []int64{10})

		require.ErrorIs(t, err, ErrGroupNotAllowed)
	})

	t.Run("requires active subscription for subscription group", func(t *testing.T) {
		svc := newOwnedAccountGroupValidationService(
			&User{ID: 101},
			map[int64]*Group{
				10: {ID: 10, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			},
			nil,
		)

		_, err := svc.validateOwnedAccountGroupBinding(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, []int64{10})

		require.ErrorIs(t, err, ErrGroupNotAllowed)
	})

	t.Run("allows subscription group with active subscription", func(t *testing.T) {
		svc := newOwnedAccountGroupValidationService(
			&User{ID: 101},
			map[int64]*Group{
				10: {ID: 10, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
			},
			map[int64]*UserSubscription{
				10: {UserID: 101, GroupID: 10},
			},
		)

		groupIDs, err := svc.validateOwnedAccountGroupBinding(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, []int64{10})

		require.NoError(t, err)
		require.Equal(t, []int64{10}, groupIDs)
	})
}

func newOwnedAccountGroupValidationService(user *User, groups map[int64]*Group, activeSubs map[int64]*UserSubscription) *AccountService {
	return &AccountService{
		groupRepo: &ownedAccountGroupRepoStub{
			groups: groups,
		},
		userRepo: &ownedAccountUserRepoStub{
			user: user,
		},
		userSubRepo: &ownedAccountUserSubRepoStub{
			active: activeSubs,
		},
	}
}

func TestAccountServiceResolveOwnedPublicShareGroup(t *testing.T) {
	svc := &AccountService{
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: 10, Name: "FREE共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelFree},
				{ID: 11, Name: "PLUS共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelPlus},
				{ID: 12, Name: "TEAM共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelTeam},
			},
		},
	}

	group, err := svc.resolveOwnedPublicShareGroup(context.Background(), &Account{Platform: PlatformOpenAI, AccountLevel: AccountLevelPlus})

	require.NoError(t, err)
	require.Equal(t, int64(11), group.ID)
}

func TestAccountServiceResolveOwnedPublicShareGroupRequiresMatchingLevelPool(t *testing.T) {
	svc := &AccountService{
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: 10, Name: "FREE共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelFree},
			},
		},
	}

	_, err := svc.resolveOwnedPublicShareGroup(context.Background(), &Account{Platform: PlatformOpenAI, AccountLevel: AccountLevelPlus})

	require.ErrorIs(t, err, ErrOwnedAccountPublicPoolUnavailable)
}

func TestAccountServiceResolveOwnedPublicShareGroupRejectsUnknownOpenAILevel(t *testing.T) {
	svc := &AccountService{
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: 10, Name: "OpenAI Standard", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic},
			},
		},
	}

	_, err := svc.resolveOwnedPublicShareGroup(context.Background(), &Account{Platform: PlatformOpenAI, AccountLevel: AccountLevelUnknown})

	require.ErrorIs(t, err, ErrOwnedAccountPublicPoolUnavailable)
}

func TestAccountServiceValidateOwnedPublicSharePolicyRequiresEnabledPositivePolicy(t *testing.T) {
	account := &Account{ID: 20, Platform: PlatformOpenAI}
	group := &Group{ID: 11, Platform: PlatformOpenAI}

	t.Run("allows positive policy", func(t *testing.T) {
		svc := &AccountService{
			accountSharePolicyRepo: &ownedPublicSharePolicyRepoStub{
				policy: &AccountSharePolicy{ID: 1, OwnerShareRatio: 0.7, Enabled: true},
			},
		}

		err := svc.validateOwnedPublicSharePolicy(context.Background(), account, group)

		require.NoError(t, err)
	})

	t.Run("rejects missing policy", func(t *testing.T) {
		svc := &AccountService{accountSharePolicyRepo: &ownedPublicSharePolicyRepoStub{}}

		err := svc.validateOwnedPublicSharePolicy(context.Background(), account, group)

		require.ErrorIs(t, err, ErrOwnedAccountPublicPolicyUnavailable)
	})

	t.Run("rejects zero owner share ratio", func(t *testing.T) {
		svc := &AccountService{
			accountSharePolicyRepo: &ownedPublicSharePolicyRepoStub{
				policy: &AccountSharePolicy{ID: 1, OwnerShareRatio: 0, Enabled: true},
			},
		}

		err := svc.validateOwnedPublicSharePolicy(context.Background(), account, group)

		require.ErrorIs(t, err, ErrOwnedAccountPublicPolicyUnavailable)
	})
}

func TestAccountServiceInitialOwnedAccountGroupIDsUsesPrivateGroupForPublicMode(t *testing.T) {
	svc := &AccountService{
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}

	groupIDs, err := svc.initialOwnedAccountGroupIDs(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, AccountShareModePublic, []int64{11})

	require.NoError(t, err)
	require.Equal(t, []int64{99}, groupIDs)
}

func TestAccountServiceInitialOwnedAccountGroupIDsIgnoresRequestedGroupsForPrivateMode(t *testing.T) {
	svc := &AccountService{
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}

	groupIDs, err := svc.initialOwnedAccountGroupIDs(context.Background(), 101, PlatformOpenAI, AccountTypeOAuth, AccountShareModePrivate, []int64{11})

	require.NoError(t, err)
	require.Equal(t, []int64{99}, groupIDs)
}

func TestAccountServiceGetPrivateGroupForOwnedAccountProvisionsMissingPrivateGroup(t *testing.T) {
	provisioner := &ownedPrivateGroupProvisionerStub{
		group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		err:   ErrGroupNotFound,
	}
	svc := &AccountService{privateGroupProvisioner: provisioner}

	group, err := svc.getPrivateGroupForOwnedAccount(context.Background(), 101, PlatformOpenAI)

	require.NoError(t, err)
	require.Equal(t, int64(99), group.ID)
	require.Equal(t, 1, provisioner.provisionCalls)
}

func TestAccountServiceCreateOwnedRejectsDuplicateOpenAIIdentity(t *testing.T) {
	ownerID := int64(101)
	repo := &ownedAccountDuplicateRepoStub{
		listOwnedByPlatform: map[string][]Account{
			PlatformOpenAI: {
				{
					ID:          1,
					Platform:    PlatformOpenAI,
					Type:        AccountTypeOAuth,
					OwnerUserID: &ownerID,
					Credentials: map[string]any{"chatgpt_account_id": "acct-1"},
				},
			},
		},
	}
	svc := &AccountService{
		accountRepo: repo,
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}

	account, err := svc.CreateOwned(context.Background(), ownerID, CreateAccountRequest{
		Name:        "duplicate",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "token", "chatgpt_account_id": "acct-1"},
		Concurrency: 1,
		Priority:    1,
	})

	require.Nil(t, account)
	require.ErrorIs(t, err, ErrOwnedAccountAlreadyExists)
	require.Empty(t, repo.createdAccounts)
}

func TestAccountServiceUpdateOwnedRejectsDuplicateAnthropicIdentity(t *testing.T) {
	ownerID := int64(101)
	repo := &ownedAccountDuplicateRepoStub{
		getByIDAccounts: map[int64]*Account{
			2: {
				ID:          2,
				Platform:    PlatformAnthropic,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				Credentials: map[string]any{"access_token": "token"},
				Status:      StatusActive,
				Schedulable: true,
				Concurrency: 1,
				Priority:    1,
			},
		},
		listOwnedByPlatform: map[string][]Account{
			PlatformAnthropic: {
				{
					ID:          1,
					Platform:    PlatformAnthropic,
					Type:        AccountTypeOAuth,
					OwnerUserID: &ownerID,
					Credentials: map[string]any{"access_token": "token", "org_uuid": "org-a", "account_uuid": "acc-a"},
				},
				{
					ID:          2,
					Platform:    PlatformAnthropic,
					Type:        AccountTypeOAuth,
					OwnerUserID: &ownerID,
				},
			},
		},
	}
	svc := &AccountService{accountRepo: repo}
	credentials := map[string]any{"access_token": "token", "org_uuid": "org-a", "account_uuid": "acc-a"}

	account, err := svc.UpdateOwned(context.Background(), ownerID, 2, UpdateAccountRequest{Credentials: &credentials})

	require.Nil(t, account)
	require.ErrorIs(t, err, ErrOwnedAccountAlreadyExists)
	require.Empty(t, repo.updatedAccounts)
}

func TestAccountServiceBulkUpdateOwnedRejectsBatchDuplicateIdentityBeforeWrite(t *testing.T) {
	ownerID := int64(101)
	repo := &ownedAccountDuplicateRepoStub{
		getByIDsAccounts: map[int64]*Account{
			1: {
				ID:          1,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				Credentials: map[string]any{"access_token": "token-1"},
				Concurrency: 1,
				Priority:    1,
			},
			2: {
				ID:          2,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				Credentials: map[string]any{"access_token": "token-2"},
				Concurrency: 1,
				Priority:    1,
			},
		},
		listOwnedByPlatform: map[string][]Account{
			PlatformOpenAI: {
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, OwnerUserID: &ownerID},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, OwnerUserID: &ownerID},
			},
		},
	}
	svc := &AccountService{accountRepo: repo}

	result, err := svc.BulkUpdateOwned(context.Background(), ownerID, &BulkUpdateOwnedAccountsInput{
		AccountIDs:  []int64{1, 2},
		Credentials: map[string]any{"chatgpt_user_id": "user-same"},
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrOwnedAccountAlreadyExists)
	require.Equal(t, 0, repo.bulkUpdateCalls)
	require.Empty(t, repo.updatedAccounts)
}

func TestAccountServiceBulkUpdateOwnedRejectsDuplicateExistingOutsideBatch(t *testing.T) {
	ownerID := int64(101)
	repo := &ownedAccountDuplicateRepoStub{
		getByIDsAccounts: map[int64]*Account{
			2: {
				ID:          2,
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				OwnerUserID: &ownerID,
				Credentials: map[string]any{"access_token": "token"},
				Concurrency: 1,
				Priority:    1,
			},
		},
		listOwnedByPlatform: map[string][]Account{
			PlatformOpenAI: {
				{
					ID:          1,
					Platform:    PlatformOpenAI,
					Type:        AccountTypeOAuth,
					OwnerUserID: &ownerID,
					Credentials: map[string]any{"access_token": "token", "email": "same@example.com"},
				},
				{
					ID:          2,
					Platform:    PlatformOpenAI,
					Type:        AccountTypeOAuth,
					OwnerUserID: &ownerID,
				},
			},
		},
	}
	svc := &AccountService{accountRepo: repo}

	result, err := svc.BulkUpdateOwned(context.Background(), ownerID, &BulkUpdateOwnedAccountsInput{
		AccountIDs:  []int64{2},
		Credentials: map[string]any{"email": "SAME@example.com"},
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrOwnedAccountAlreadyExists)
	require.Equal(t, 0, repo.bulkUpdateCalls)
}

func TestAccountServiceBulkUpdateOwnedShareModeUsesPerAccountUpdateOnly(t *testing.T) {
	ownerID := int64(101)
	repo := &ownedAccountDuplicateRepoStub{
		getByIDAccounts: map[int64]*Account{
			1: {
				ID:           1,
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPlus,
				Type:         AccountTypeOAuth,
				OwnerUserID:  &ownerID,
				Credentials:  map[string]any{"access_token": "token", "chatgpt_account_id": "acct-1"},
				ShareMode:    AccountShareModePrivate,
				ShareStatus:  AccountShareStatusApproved,
				Status:       StatusActive,
				Schedulable:  true,
				Concurrency:  OpenAIPlusDefaultConcurrency,
				Priority:     1,
			},
		},
		getByIDsAccounts: map[int64]*Account{
			1: {
				ID:           1,
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPlus,
				Type:         AccountTypeOAuth,
				OwnerUserID:  &ownerID,
				Credentials:  map[string]any{"access_token": "token", "chatgpt_account_id": "acct-1"},
				ShareMode:    AccountShareModePrivate,
				ShareStatus:  AccountShareStatusApproved,
				Status:       StatusActive,
				Schedulable:  true,
				Concurrency:  OpenAIPlusDefaultConcurrency,
				Priority:     1,
			},
		},
		listOwnedByPlatform: map[string][]Account{
			PlatformOpenAI: {
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, OwnerUserID: &ownerID, Credentials: map[string]any{"chatgpt_account_id": "acct-1"}},
			},
		},
	}
	svc := &AccountService{
		accountRepo: repo,
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
	}
	shareMode := AccountShareModePrivate
	status := StatusDisabled

	result, err := svc.BulkUpdateOwned(context.Background(), ownerID, &BulkUpdateOwnedAccountsInput{
		AccountIDs: []int64{1},
		Status:     status,
		ShareMode:  &shareMode,
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 0, repo.bulkUpdateCalls)
	require.Len(t, repo.updatedAccounts, 1)
	require.Equal(t, StatusDisabled, repo.updatedAccounts[0].Status)
	require.Equal(t, AccountShareModePrivate, repo.updatedAccounts[0].ShareMode)
}

func TestAccountServiceManagedGroupIDsKeepsApprovedPublicAccountInPublicPool(t *testing.T) {
	ownerID := int64(101)
	svc := &AccountService{
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: 18, Name: "Plus Shared Pool", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic, RequiredAccountLevel: AccountLevelPlus},
			},
		},
	}
	account := &Account{
		ID:           20,
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		OwnerUserID:  &ownerID,
		ShareMode:    AccountShareModePublic,
		ShareStatus:  AccountShareStatusApproved,
		AccountLevel: AccountLevelPlus,
	}

	groupIDs, err := svc.managedOwnedAccountGroupIDsForShareMode(context.Background(), ownerID, account, AccountShareModePublic)

	require.NoError(t, err)
	require.Equal(t, []int64{99, 18}, groupIDs)
}

func TestAccountServiceDuplicateIdentityKeys(t *testing.T) {
	t.Run("openai uses stable account and user identity", func(t *testing.T) {
		keys := accountDuplicateIdentityKeys(&Account{
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Credentials: map[string]any{"chatgpt_account_id": "acct", "chatgpt_user_id": "user", "organization_id": "org"},
		})

		require.Contains(t, keys, ownedAccountDuplicateKey{Name: "openai.chatgpt_account_id", Value: "acct"})
		require.Contains(t, keys, ownedAccountDuplicateKey{Name: "openai.chatgpt_user_id", Value: "user"})
		require.NotContains(t, keys, ownedAccountDuplicateKey{Name: "openai.organization_id", Value: "org"})
	})

	t.Run("anthropic combines org and account uuid", func(t *testing.T) {
		keys := accountDuplicateIdentityKeys(&Account{
			Platform:    PlatformAnthropic,
			Type:        AccountTypeOAuth,
			Credentials: map[string]any{"org_uuid": "ORG", "account_uuid": "ACC"},
		})

		require.Contains(t, keys, ownedAccountDuplicateKey{Name: "anthropic.org_account", Value: "org|acc"})
	})

	t.Run("antigravity email is case insensitive", func(t *testing.T) {
		keys := accountDuplicateIdentityKeys(&Account{
			Platform:    PlatformAntigravity,
			Type:        AccountTypeOAuth,
			Credentials: map[string]any{"email": "USER@Example.COM"},
		})

		require.Contains(t, keys, ownedAccountDuplicateKey{Name: "antigravity.email", Value: "user@example.com"})
	})
}

func TestIsOwnedAccountPublicShareApprovableAllowsRateLimitedAccountWithOption(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	account := &Account{
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		Status:           StatusActive,
		Schedulable:      true,
		RateLimitResetAt: &resetAt,
	}

	require.False(t, isOwnedAccountPublicShareApprovable(account, false))
	require.True(t, isOwnedAccountPublicShareApprovable(account, true))
}

func TestIsOwnedAccountPublicShareApprovableStillRejectsDisabledAccount(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	account := &Account{
		Platform:         PlatformOpenAI,
		Type:             AccountTypeOAuth,
		Status:           StatusDisabled,
		Schedulable:      true,
		RateLimitResetAt: &resetAt,
	}

	require.False(t, isOwnedAccountPublicShareApprovable(account, true))
}
