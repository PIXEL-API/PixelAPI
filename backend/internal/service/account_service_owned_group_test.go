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
				{ID: 10, Name: "OpenAI Standard", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic},
				{ID: 11, Name: "OpenAI Plus 共享号池", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic},
				{ID: 12, Name: "Gemini Plus 共享号池", Platform: PlatformGemini, Status: StatusActive, Scope: GroupScopePublic},
			},
		},
	}

	group, err := svc.resolveOwnedPublicShareGroup(context.Background(), &Account{Platform: PlatformOpenAI})

	require.NoError(t, err)
	require.Equal(t, int64(11), group.ID)
}

func TestAccountServiceResolveOwnedPublicShareGroupRequiresPlusPool(t *testing.T) {
	svc := &AccountService{
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: 10, Name: "OpenAI Standard", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic},
			},
		},
	}

	_, err := svc.resolveOwnedPublicShareGroup(context.Background(), &Account{Platform: PlatformOpenAI})

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

func TestAccountServiceManagedGroupIDsKeepsApprovedPublicAccountInPublicPool(t *testing.T) {
	ownerID := int64(101)
	svc := &AccountService{
		privateGroupProvisioner: &ownedPrivateGroupProvisionerStub{
			group: &Group{ID: 99, Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopeUserPrivate},
		},
		groupRepo: &ownedPublicShareGroupRepoStub{
			groups: []Group{
				{ID: 18, Name: "Plus Shared Pool", Platform: PlatformOpenAI, Status: StatusActive, Scope: GroupScopePublic},
			},
		},
	}
	account := &Account{
		ID:          20,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		OwnerUserID: &ownerID,
		ShareMode:   AccountShareModePublic,
		ShareStatus: AccountShareStatusApproved,
	}

	groupIDs, err := svc.managedOwnedAccountGroupIDsForShareMode(context.Background(), ownerID, account, AccountShareModePublic)

	require.NoError(t, err)
	require.Equal(t, []int64{99, 18}, groupIDs)
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
