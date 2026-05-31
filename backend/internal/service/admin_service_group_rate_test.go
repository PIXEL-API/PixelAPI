//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func groupRateTestFloatPtr(v float64) *float64 {
	return &v
}

// userGroupRateRepoStubForGroupRate implements UserGroupRateRepository for group rate tests.
type userGroupRateRepoStubForGroupRate struct {
	getByGroupIDData map[int64][]UserGroupRateEntry
	getByGroupIDErr  error

	deletedGroupIDs  []int64
	deleteByGroupErr error

	syncedGroupID int64
	syncedEntries []GroupRateMultiplierInput
	syncGroupErr  error

	rpmSyncedGroupID int64
	rpmSyncedEntries []GroupRPMOverrideInput
	rpmSyncErr       error
}

func (s *userGroupRateRepoStubForGroupRate) GetByUserID(_ context.Context, _ int64) (map[int64]float64, error) {
	panic("unexpected GetByUserID call")
}

func (s *userGroupRateRepoStubForGroupRate) GetByUserAndGroup(_ context.Context, _, _ int64) (*float64, error) {
	panic("unexpected GetByUserAndGroup call")
}

func (s *userGroupRateRepoStubForGroupRate) GetRPMOverrideByUserAndGroup(_ context.Context, _, _ int64) (*int, error) {
	panic("unexpected GetRPMOverrideByUserAndGroup call")
}

func (s *userGroupRateRepoStubForGroupRate) GetByGroupID(_ context.Context, groupID int64) ([]UserGroupRateEntry, error) {
	if s.getByGroupIDErr != nil {
		return nil, s.getByGroupIDErr
	}
	return s.getByGroupIDData[groupID], nil
}

func (s *userGroupRateRepoStubForGroupRate) GetRateMultipliersByGroupID(_ context.Context, groupID int64) (map[int64]float64, error) {
	out := make(map[int64]float64)
	for _, entry := range s.getByGroupIDData[groupID] {
		if entry.RateMultiplier != nil {
			out[entry.UserID] = *entry.RateMultiplier
		}
	}
	return out, s.getByGroupIDErr
}

func (s *userGroupRateRepoStubForGroupRate) SyncUserGroupRates(_ context.Context, userID int64, rates map[int64]*float64) error {
	return nil
}

func (s *userGroupRateRepoStubForGroupRate) SyncGroupRateMultipliers(_ context.Context, groupID int64, entries []GroupRateMultiplierInput) error {
	s.syncedGroupID = groupID
	s.syncedEntries = entries
	return s.syncGroupErr
}

func (s *userGroupRateRepoStubForGroupRate) SyncGroupRPMOverrides(_ context.Context, groupID int64, entries []GroupRPMOverrideInput) error {
	s.rpmSyncedGroupID = groupID
	s.rpmSyncedEntries = entries
	return s.rpmSyncErr
}

func (s *userGroupRateRepoStubForGroupRate) ClearGroupRPMOverrides(_ context.Context, _ int64) error {
	panic("unexpected ClearGroupRPMOverrides call")
}

func (s *userGroupRateRepoStubForGroupRate) DeleteByGroupID(_ context.Context, groupID int64) error {
	s.deletedGroupIDs = append(s.deletedGroupIDs, groupID)
	return s.deleteByGroupErr
}

func (s *userGroupRateRepoStubForGroupRate) DeleteByUserID(_ context.Context, _ int64) error {
	panic("unexpected DeleteByUserID call")
}

type apiKeyRepoStubForGroupNotice struct {
	keysByGroup map[int64][]APIKey
}

func (s *apiKeyRepoStubForGroupNotice) ListByGroupID(_ context.Context, groupID int64, params pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	keys := s.keysByGroup[groupID]
	start := params.Offset()
	if start > len(keys) {
		start = len(keys)
	}
	end := start + params.Limit()
	if end > len(keys) {
		end = len(keys)
	}
	out := append([]APIKey(nil), keys[start:end]...)
	return out, &pagination.PaginationResult{Total: int64(len(keys)), Page: params.Page, PageSize: params.PageSize}, nil
}

func (s *apiKeyRepoStubForGroupNotice) Create(context.Context, *APIKey) error {
	panic("unexpected Create call")
}
func (s *apiKeyRepoStubForGroupNotice) GetByID(context.Context, int64) (*APIKey, error) {
	panic("unexpected GetByID call")
}
func (s *apiKeyRepoStubForGroupNotice) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected GetKeyAndOwnerID call")
}
func (s *apiKeyRepoStubForGroupNotice) GetByKey(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKey call")
}
func (s *apiKeyRepoStubForGroupNotice) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	panic("unexpected GetByKeyForAuth call")
}
func (s *apiKeyRepoStubForGroupNotice) Update(context.Context, *APIKey) error {
	panic("unexpected Update call")
}
func (s *apiKeyRepoStubForGroupNotice) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *apiKeyRepoStubForGroupNotice) ListByUserID(context.Context, int64, pagination.PaginationParams, APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	panic("unexpected ListByUserID call")
}
func (s *apiKeyRepoStubForGroupNotice) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected VerifyOwnership call")
}
func (s *apiKeyRepoStubForGroupNotice) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected CountByUserID call")
}
func (s *apiKeyRepoStubForGroupNotice) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected ExistsByKey call")
}
func (s *apiKeyRepoStubForGroupNotice) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected SearchAPIKeys call")
}
func (s *apiKeyRepoStubForGroupNotice) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected ClearGroupIDByGroupID call")
}
func (s *apiKeyRepoStubForGroupNotice) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected UpdateGroupIDByUserAndGroup call")
}
func (s *apiKeyRepoStubForGroupNotice) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected CountByGroupID call")
}
func (s *apiKeyRepoStubForGroupNotice) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByUserID call")
}
func (s *apiKeyRepoStubForGroupNotice) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected ListKeysByGroupID call")
}
func (s *apiKeyRepoStubForGroupNotice) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected IncrementQuotaUsed call")
}
func (s *apiKeyRepoStubForGroupNotice) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected UpdateLastUsed call")
}
func (s *apiKeyRepoStubForGroupNotice) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementRateLimitUsage call")
}
func (s *apiKeyRepoStubForGroupNotice) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected ResetRateLimitWindows call")
}
func (s *apiKeyRepoStubForGroupNotice) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected GetRateLimitData call")
}

type userSubRepoStubForGroupNotice struct {
	subsByGroup map[int64][]UserSubscription
}

func (s *userSubRepoStubForGroupNotice) ListByGroupID(_ context.Context, groupID int64, params pagination.PaginationParams) ([]UserSubscription, *pagination.PaginationResult, error) {
	subs := s.subsByGroup[groupID]
	start := params.Offset()
	if start > len(subs) {
		start = len(subs)
	}
	end := start + params.Limit()
	if end > len(subs) {
		end = len(subs)
	}
	out := append([]UserSubscription(nil), subs[start:end]...)
	return out, &pagination.PaginationResult{Total: int64(len(subs)), Page: params.Page, PageSize: params.PageSize}, nil
}

func (s *userSubRepoStubForGroupNotice) Create(context.Context, *UserSubscription) error {
	panic("unexpected Create call")
}
func (s *userSubRepoStubForGroupNotice) GetByID(context.Context, int64) (*UserSubscription, error) {
	panic("unexpected GetByID call")
}
func (s *userSubRepoStubForGroupNotice) GetByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetByUserIDAndGroupID call")
}
func (s *userSubRepoStubForGroupNotice) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	panic("unexpected GetActiveByUserIDAndGroupID call")
}
func (s *userSubRepoStubForGroupNotice) Update(context.Context, *UserSubscription) error {
	panic("unexpected Update call")
}
func (s *userSubRepoStubForGroupNotice) Delete(context.Context, int64) error {
	panic("unexpected Delete call")
}
func (s *userSubRepoStubForGroupNotice) ListByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListByUserID call")
}
func (s *userSubRepoStubForGroupNotice) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	panic("unexpected ListActiveByUserID call")
}
func (s *userSubRepoStubForGroupNotice) List(context.Context, pagination.PaginationParams, *int64, *int64, string, string, string, string) ([]UserSubscription, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}
func (s *userSubRepoStubForGroupNotice) ExistsByUserIDAndGroupID(context.Context, int64, int64) (bool, error) {
	panic("unexpected ExistsByUserIDAndGroupID call")
}
func (s *userSubRepoStubForGroupNotice) ExtendExpiry(context.Context, int64, time.Time) error {
	panic("unexpected ExtendExpiry call")
}
func (s *userSubRepoStubForGroupNotice) UpdateStatus(context.Context, int64, string) error {
	panic("unexpected UpdateStatus call")
}
func (s *userSubRepoStubForGroupNotice) UpdateNotes(context.Context, int64, string) error {
	panic("unexpected UpdateNotes call")
}
func (s *userSubRepoStubForGroupNotice) ActivateWindows(context.Context, int64, time.Time) error {
	panic("unexpected ActivateWindows call")
}
func (s *userSubRepoStubForGroupNotice) ResetDailyUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetDailyUsage call")
}
func (s *userSubRepoStubForGroupNotice) ResetWeeklyUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetWeeklyUsage call")
}
func (s *userSubRepoStubForGroupNotice) ResetMonthlyUsage(context.Context, int64, time.Time) error {
	panic("unexpected ResetMonthlyUsage call")
}
func (s *userSubRepoStubForGroupNotice) IncrementUsage(context.Context, int64, float64) error {
	panic("unexpected IncrementUsage call")
}
func (s *userSubRepoStubForGroupNotice) BatchUpdateExpiredStatus(context.Context) (int64, error) {
	panic("unexpected BatchUpdateExpiredStatus call")
}

func newNoticeRecorder(users ...int64) (*SystemNoticeService, *[]*Conversation) {
	created := make([]*Conversation, 0)
	repo := &conversationRepoStub{
		createWithMessageFunc: func(_ context.Context, conv *Conversation, msg *ConversationMessage) error {
			cp := *conv
			cp.ID = int64(len(created) + 1)
			cp.LastMessageExcerpt = msg.Content
			created = append(created, &cp)
			return nil
		},
		getNoticeBySourceFunc: func(context.Context, int64, string, string) (*Conversation, error) {
			return nil, ErrConversationNotFound
		},
	}
	userMap := make(map[int64]*User, len(users))
	for _, userID := range users {
		userMap[userID] = &User{ID: userID, Status: StatusActive}
	}
	return NewSystemNoticeService(NewConversationService(repo, &conversationUserRepoStub{users: userMap})), &created
}

func TestAdminService_GetGroupRateMultipliers(t *testing.T) {
	t.Run("returns entries for group", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{
			getByGroupIDData: map[int64][]UserGroupRateEntry{
				10: {
					{UserID: 1, UserName: "alice", UserEmail: "alice@test.com", RateMultiplier: groupRateTestFloatPtr(1.5)},
					{UserID: 2, UserName: "bob", UserEmail: "bob@test.com", RateMultiplier: groupRateTestFloatPtr(0.8)},
				},
			},
		}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		entries, err := svc.GetGroupRateMultipliers(context.Background(), 10)
		require.NoError(t, err)
		require.Len(t, entries, 2)
		require.Equal(t, int64(1), entries[0].UserID)
		require.Equal(t, "alice", entries[0].UserName)
		require.NotNil(t, entries[0].RateMultiplier)
		require.Equal(t, 1.5, *entries[0].RateMultiplier)
		require.Equal(t, int64(2), entries[1].UserID)
		require.NotNil(t, entries[1].RateMultiplier)
		require.Equal(t, 0.8, *entries[1].RateMultiplier)
	})

	t.Run("returns nil when repo is nil", func(t *testing.T) {
		svc := &adminServiceImpl{userGroupRateRepo: nil}

		entries, err := svc.GetGroupRateMultipliers(context.Background(), 10)
		require.NoError(t, err)
		require.Nil(t, entries)
	})

	t.Run("returns empty slice for group with no entries", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{
			getByGroupIDData: map[int64][]UserGroupRateEntry{},
		}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		entries, err := svc.GetGroupRateMultipliers(context.Background(), 99)
		require.NoError(t, err)
		require.Nil(t, entries)
	})

	t.Run("propagates repo error", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{
			getByGroupIDErr: errors.New("db error"),
		}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		_, err := svc.GetGroupRateMultipliers(context.Background(), 10)
		require.Error(t, err)
		require.Contains(t, err.Error(), "db error")
	})
}

func TestAdminService_ClearGroupRateMultipliers(t *testing.T) {
	t.Run("deletes by group ID", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		err := svc.ClearGroupRateMultipliers(context.Background(), 42)
		require.NoError(t, err)
		require.Equal(t, []int64{42}, repo.deletedGroupIDs)
	})

	t.Run("returns nil when repo is nil", func(t *testing.T) {
		svc := &adminServiceImpl{userGroupRateRepo: nil}

		err := svc.ClearGroupRateMultipliers(context.Background(), 42)
		require.NoError(t, err)
	})

	t.Run("propagates repo error", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{
			deleteByGroupErr: errors.New("delete failed"),
		}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		err := svc.ClearGroupRateMultipliers(context.Background(), 42)
		require.Error(t, err)
		require.Contains(t, err.Error(), "delete failed")
	})
}

func TestAdminService_BatchSetGroupRateMultipliers(t *testing.T) {
	t.Run("syncs entries to repo", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		entries := []GroupRateMultiplierInput{
			{UserID: 1, RateMultiplier: 1.5},
			{UserID: 2, RateMultiplier: 0.8},
		}
		err := svc.BatchSetGroupRateMultipliers(context.Background(), 10, entries)
		require.NoError(t, err)
		require.Equal(t, int64(10), repo.syncedGroupID)
		require.Equal(t, entries, repo.syncedEntries)
	})

	t.Run("returns nil when repo is nil", func(t *testing.T) {
		svc := &adminServiceImpl{userGroupRateRepo: nil}

		err := svc.BatchSetGroupRateMultipliers(context.Background(), 10, nil)
		require.NoError(t, err)
	})

	t.Run("propagates repo error", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{
			syncGroupErr: errors.New("sync failed"),
		}
		svc := &adminServiceImpl{userGroupRateRepo: repo}

		err := svc.BatchSetGroupRateMultipliers(context.Background(), 10, []GroupRateMultiplierInput{
			{UserID: 1, RateMultiplier: 1.0},
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "sync failed")
	})
}

func TestAdminService_BatchSetGroupRateMultipliers_NotifiesOnlyChangedUsers(t *testing.T) {
	rateRepo := &userGroupRateRepoStubForGroupRate{
		getByGroupIDData: map[int64][]UserGroupRateEntry{
			10: {
				{UserID: 1, RateMultiplier: groupRateTestFloatPtr(1.2)},
				{UserID: 2, RateMultiplier: groupRateTestFloatPtr(1.5)},
			},
		},
	}
	noticeSvc, created := newNoticeRecorder(1, 2, 3)
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		groupRepo:            &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{10: {ID: 10, Name: "VIP", RateMultiplier: 1.0}}},
		userGroupRateRepo:    rateRepo,
		systemNoticeService:  noticeSvc,
		authCacheInvalidator: invalidator,
	}

	err := svc.BatchSetGroupRateMultipliers(context.Background(), 10, []GroupRateMultiplierInput{
		{UserID: 1, RateMultiplier: 1.2},
		{UserID: 3, RateMultiplier: 0.9},
	})

	require.NoError(t, err)
	require.Equal(t, []int64{10}, invalidator.groupIDs)
	require.Len(t, *created, 2)
	require.ElementsMatch(t, []int64{2, 3}, []int64{(*created)[0].UserID, (*created)[1].UserID})
	for _, conv := range *created {
		require.Equal(t, SystemNoticeSourceGroup, conv.Source)
		require.Equal(t, ConversationTypeBilling, conv.Type)
		require.NotContains(t, conv.LastMessageExcerpt, "user_id")
		require.NotContains(t, conv.LastMessageExcerpt, "alice")
	}
}

func TestAdminService_UpdateUserGroupRates_NotifiesSingleUserAndInvalidatesUserCache(t *testing.T) {
	userRepo := &userRepoStub{user: &User{ID: 7, Email: "u7@test.com", Status: StatusActive}}
	rateRepo := &userGroupRateRepoStubForListUsers{
		singleData: map[int64]map[int64]float64{
			7: {10: 1.2},
		},
	}
	noticeSvc, created := newNoticeRecorder(7)
	invalidator := &authCacheInvalidatorStub{}
	nextRate := 1.8
	svc := &adminServiceImpl{
		userRepo:             userRepo,
		groupRepo:            &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{10: {ID: 10, Name: "VIP", RateMultiplier: 1.0}}},
		userGroupRateRepo:    rateRepo,
		systemNoticeService:  noticeSvc,
		authCacheInvalidator: invalidator,
	}

	_, err := svc.UpdateUser(context.Background(), 7, &UpdateUserInput{
		GroupRates: map[int64]*float64{10: &nextRate},
	})

	require.NoError(t, err)
	require.Equal(t, []int64{7}, invalidator.userIDs)
	require.Len(t, *created, 1)
	require.Equal(t, int64(7), (*created)[0].UserID)
	require.Equal(t, SystemNoticeSourceGroup, (*created)[0].Source)
	require.NotContains(t, (*created)[0].LastMessageExcerpt, "u7@test.com")
}

func TestAdminService_UpdateUserGroupRates_DoesNotNotifyWhenSyncFails(t *testing.T) {
	userRepo := &userRepoStub{user: &User{ID: 7, Email: "u7@test.com", Status: StatusActive}}
	rateRepo := &userGroupRateRepoStubForListUsers{
		singleData: map[int64]map[int64]float64{
			7: {10: 1.2},
		},
		syncErr: errors.New("sync failed"),
	}
	noticeSvc, created := newNoticeRecorder(7)
	nextRate := 1.8
	svc := &adminServiceImpl{
		userRepo:            userRepo,
		groupRepo:           &groupRepoStubForAdmin{getByIDByID: map[int64]*Group{10: {ID: 10, Name: "VIP", RateMultiplier: 1.0}}},
		userGroupRateRepo:   rateRepo,
		systemNoticeService: noticeSvc,
	}

	_, err := svc.UpdateUser(context.Background(), 7, &UpdateUserInput{
		GroupRates: map[int64]*float64{10: &nextRate},
	})

	require.NoError(t, err)
	require.Empty(t, *created)
}

func TestAdminService_UpdateGroupRateMultiplier_NotifiesAffectedUsersOnly(t *testing.T) {
	ownerID := int64(9)
	existingGroup := &Group{
		ID:             10,
		Name:           "VIP",
		RateMultiplier: 1.0,
		Status:         StatusActive,
		OwnerUserID:    &ownerID,
	}
	noticeSvc, created := newNoticeRecorder(1, 2, 3, 9)
	nextRate := 1.6
	svc := &adminServiceImpl{
		groupRepo:           &groupRepoStubForAdmin{getByID: existingGroup},
		apiKeyRepo:          &apiKeyRepoStubForGroupNotice{keysByGroup: map[int64][]APIKey{10: {{UserID: 1}, {UserID: 1}, {UserID: 2}}}},
		userSubRepo:         &userSubRepoStubForGroupNotice{subsByGroup: map[int64][]UserSubscription{10: {{UserID: 3, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}}}},
		systemNoticeService: noticeSvc,
	}

	_, err := svc.UpdateGroup(context.Background(), 10, &UpdateGroupInput{RateMultiplier: &nextRate})

	require.NoError(t, err)
	require.Len(t, *created, 4)
	require.ElementsMatch(t, []int64{1, 2, 3, 9}, []int64{(*created)[0].UserID, (*created)[1].UserID, (*created)[2].UserID, (*created)[3].UserID})
	for _, conv := range *created {
		require.NotContains(t, conv.LastMessageExcerpt, "10")
		require.NotContains(t, conv.LastMessageExcerpt, "user_id")
	}
}

func TestAdminService_UpdateGroupRateMultiplier_SkipsUsersWithCustomRate(t *testing.T) {
	existingGroup := &Group{
		ID:             10,
		Name:           "VIP",
		RateMultiplier: 1.0,
		Status:         StatusActive,
	}
	rateRepo := &userGroupRateRepoStubForGroupRate{
		getByGroupIDData: map[int64][]UserGroupRateEntry{
			10: {
				{UserID: 2, RateMultiplier: groupRateTestFloatPtr(0.8)},
			},
		},
	}
	noticeSvc, created := newNoticeRecorder(1, 2)
	nextRate := 1.6
	svc := &adminServiceImpl{
		groupRepo:           &groupRepoStubForAdmin{getByID: existingGroup},
		apiKeyRepo:          &apiKeyRepoStubForGroupNotice{keysByGroup: map[int64][]APIKey{10: {{UserID: 1}, {UserID: 2}}}},
		userGroupRateRepo:   rateRepo,
		systemNoticeService: noticeSvc,
	}

	_, err := svc.UpdateGroup(context.Background(), 10, &UpdateGroupInput{RateMultiplier: &nextRate})

	require.NoError(t, err)
	require.Len(t, *created, 1)
	require.Equal(t, int64(1), (*created)[0].UserID)
}

func TestAdminService_BatchSetGroupRPMOverrides(t *testing.T) {
	t.Run("syncs entries to repo", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{}
		svc := &adminServiceImpl{userGroupRateRepo: repo}
		override := 20
		entries := []GroupRPMOverrideInput{{UserID: 2, RPMOverride: &override}}

		err := svc.BatchSetGroupRPMOverrides(context.Background(), 10, entries)
		require.NoError(t, err)
		require.Equal(t, int64(10), repo.rpmSyncedGroupID)
		require.Equal(t, entries, repo.rpmSyncedEntries)
	})

	t.Run("rejects negative override as bad request", func(t *testing.T) {
		repo := &userGroupRateRepoStubForGroupRate{}
		svc := &adminServiceImpl{userGroupRateRepo: repo}
		negative := -1

		err := svc.BatchSetGroupRPMOverrides(context.Background(), 10, []GroupRPMOverrideInput{
			{UserID: 2, RPMOverride: &negative},
		})
		require.Error(t, err)
		require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
		require.Zero(t, repo.rpmSyncedGroupID)
	})
}
