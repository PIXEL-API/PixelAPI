//go:build unit

package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type accountRepoStubForBulkUpdate struct {
	accountRepoStub
	bulkUpdateErr    error
	bulkUpdateIDs    []int64
	bulkUpdateUpdate AccountBulkUpdate
	updatedAccount   *Account
	bindGroupErrByID map[int64]error
	bindGroupsCalls  []int64
	getByIDsAccounts []*Account
	getByIDsErr      error
	getByIDsCalled   bool
	getByIDsIDs      []int64
	getByIDAccounts  map[int64]*Account
	getByIDErrByID   map[int64]error
	getByIDCalled    []int64
	listByGroupData  map[int64][]Account
	listByGroupErr   map[int64]error
	listData         []Account
	listResult       *pagination.PaginationResult
	listErr          error
	listCalled       bool
	lastListParams   pagination.PaginationParams
	lastListFilters  struct {
		platform    string
		accountType string
		status      string
		search      string
		groupID     int64
		privacyMode string
	}
}

func (s *accountRepoStubForBulkUpdate) BulkUpdate(_ context.Context, ids []int64, update AccountBulkUpdate) (int64, error) {
	s.bulkUpdateIDs = append([]int64{}, ids...)
	s.bulkUpdateUpdate = update
	if s.bulkUpdateErr != nil {
		return 0, s.bulkUpdateErr
	}
	return int64(len(ids)), nil
}

func (s *accountRepoStubForBulkUpdate) Update(_ context.Context, account *Account) error {
	s.updatedAccount = account
	return nil
}

func (s *accountRepoStubForBulkUpdate) BindGroups(_ context.Context, accountID int64, _ []int64) error {
	s.bindGroupsCalls = append(s.bindGroupsCalls, accountID)
	if err, ok := s.bindGroupErrByID[accountID]; ok {
		return err
	}
	return nil
}

func (s *accountRepoStubForBulkUpdate) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	s.getByIDsCalled = true
	s.getByIDsIDs = append([]int64{}, ids...)
	if s.getByIDsErr != nil {
		return nil, s.getByIDsErr
	}
	return s.getByIDsAccounts, nil
}

func (s *accountRepoStubForBulkUpdate) GetByID(_ context.Context, id int64) (*Account, error) {
	s.getByIDCalled = append(s.getByIDCalled, id)
	if err, ok := s.getByIDErrByID[id]; ok {
		return nil, err
	}
	if account, ok := s.getByIDAccounts[id]; ok {
		return account, nil
	}
	return nil, errors.New("account not found")
}

func (s *accountRepoStubForBulkUpdate) ListByGroup(_ context.Context, groupID int64) ([]Account, error) {
	if err, ok := s.listByGroupErr[groupID]; ok {
		return nil, err
	}
	if rows, ok := s.listByGroupData[groupID]; ok {
		return rows, nil
	}
	return nil, nil
}

func (s *accountRepoStubForBulkUpdate) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	s.listCalled = true
	s.lastListParams = params
	s.lastListFilters.platform = platform
	s.lastListFilters.accountType = accountType
	s.lastListFilters.status = status
	s.lastListFilters.search = search
	s.lastListFilters.groupID = groupID
	s.lastListFilters.privacyMode = privacyMode
	if s.listErr != nil {
		return nil, nil, s.listErr
	}
	if s.listResult != nil {
		return s.listData, s.listResult, nil
	}
	return s.listData, &pagination.PaginationResult{Total: int64(len(s.listData))}, nil
}

// TestAdminService_BulkUpdateAccounts_AllSuccessIDs 验证批量更新成功时返回 success_ids/failed_ids。
func TestAdminService_BulkUpdateAccounts_AllSuccessIDs(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{accountRepo: repo}

	schedulable := true
	input := &BulkUpdateAccountsInput{
		AccountIDs:  []int64{1, 2, 3},
		Schedulable: &schedulable,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 3, result.Success)
	require.Equal(t, 0, result.Failed)
	require.ElementsMatch(t, []int64{1, 2, 3}, result.SuccessIDs)
	require.Empty(t, result.FailedIDs)
	require.Len(t, result.Results, 3)
}

// TestAdminService_BulkUpdateAccounts_PartialFailureIDs 验证部分失败时 success_ids/failed_ids 正确。
func TestAdminService_BulkUpdateAccounts_PartialFailureIDs(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		bindGroupErrByID: map[int64]error{
			2: errors.New("bind failed"),
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo:   &groupRepoStubForAdmin{getByID: &Group{ID: 10, Name: "g10"}},
	}

	groupIDs := []int64{10}
	schedulable := false
	input := &BulkUpdateAccountsInput{
		AccountIDs:            []int64{1, 2, 3},
		GroupIDs:              &groupIDs,
		Schedulable:           &schedulable,
		SkipMixedChannelCheck: true,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 1, result.Failed)
	require.ElementsMatch(t, []int64{1, 3}, result.SuccessIDs)
	require.ElementsMatch(t, []int64{2}, result.FailedIDs)
	require.Len(t, result.Results, 3)
}

func TestAdminService_BulkUpdateAccounts_NilGroupRepoReturnsError(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{accountRepo: repo}

	groupIDs := []int64{10}
	input := &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		GroupIDs:   &groupIDs,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "group repository not configured")
}

func TestAdminService_UpdateAccountLevel_ValidatesExistingGroups(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			1: {
				ID:           1,
				Name:         "plus-account",
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPlus,
				Concurrency:  OpenAIPlusDefaultConcurrency,
				GroupIDs:     []int64{10},
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   10,
				Name:                 "Plus Pool",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelPlus,
			},
		},
	}

	level := AccountLevelFree
	result, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		AccountLevel: &level,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, AccountLevelPlus, repo.updatedAccount.AccountLevel)
}

func TestAdminService_BulkUpdateAccountLevel_ValidatesExistingGroups(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{
				ID:           1,
				Name:         "plus-account",
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPlus,
				Concurrency:  OpenAIPlusDefaultConcurrency,
				GroupIDs:     []int64{10},
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   10,
				Name:                 "Plus Pool",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelPlus,
			},
		},
	}

	level := AccountLevelFree
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:   []int64{1},
		AccountLevel: &level,
		Schedulable:  nil,
		Credentials:  nil,
		Extra:        nil,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []int64{1}, repo.bulkUpdateIDs)
	require.Nil(t, repo.bulkUpdateUpdate.AccountLevel)
}

func TestAdminService_UpdateAccount_UsesInferredOpenAILevelForGroupBinding(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			1: {
				ID:           1,
				Name:         "plus-account",
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPro,
				Concurrency:  OpenAIPlusDefaultConcurrency,
				Credentials:  map[string]any{"plan_type": "plus"},
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   20,
				Name:                 "PRO共享池",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelPro,
			},
		},
	}

	groupIDs := []int64{20}
	result, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "account_level mismatch")
	require.Nil(t, repo.updatedAccount)
}

func TestAdminService_UpdateOwnedPrivateAccountRejectsPublicGroupBinding(t *testing.T) {
	ownerID := int64(101)
	repo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			1: {
				ID:          1,
				Name:        "owned-private",
				Platform:    PlatformOpenAI,
				OwnerUserID: &ownerID,
				ShareMode:   AccountShareModePrivate,
				ShareStatus: AccountShareStatusApproved,
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{ID: 10, Name: "PLUS共享号池", Platform: PlatformOpenAI, Scope: GroupScopePublic, Status: StatusActive},
		},
	}

	groupIDs := []int64{10}
	result, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "approved public share")
	require.Nil(t, repo.updatedAccount)
}

func TestAdminService_BulkUpdateOwnedPrivateAccountRejectsPublicGroupBinding(t *testing.T) {
	ownerID := int64(101)
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{
				ID:          1,
				Name:        "owned-private",
				Platform:    PlatformOpenAI,
				OwnerUserID: &ownerID,
				ShareMode:   AccountShareModePrivate,
				ShareStatus: AccountShareStatusApproved,
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{ID: 10, Name: "PLUS共享号池", Platform: PlatformOpenAI, Scope: GroupScopePublic, Status: StatusActive},
		},
	}

	groupIDs := []int64{10}
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:            []int64{1},
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "approved public share")
	require.Empty(t, repo.bulkUpdateIDs)
}

// TestAdminService_BulkUpdateAccounts_MixedChannelPreCheckBlocksOnExistingConflict verifies
// that the global pre-check detects a conflict with existing group members and returns an
// error before any DB write is performed.
func TestAdminService_BulkUpdateAccounts_MixedChannelPreCheckBlocksOnExistingConflict(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{ID: 1, Platform: PlatformAntigravity},
		},
		// Group 10 already contains an Anthropic account.
		listByGroupData: map[int64][]Account{
			10: {{ID: 99, Platform: PlatformAnthropic}},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo:   &groupRepoStubForAdmin{getByID: &Group{ID: 10, Name: "target-group"}},
	}

	groupIDs := []int64{10}
	input := &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		GroupIDs:   &groupIDs,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.Nil(t, result)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mixed channel")
	// No BindGroups should have been called since the check runs before any write.
	require.Empty(t, repo.bindGroupsCalls)
}

func TestAdminServiceBulkUpdateAccounts_ResolvesIDsFromFilters(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		listData: []Account{
			{ID: 7},
			{ID: 11},
		},
		listResult: &pagination.PaginationResult{Total: 2},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	schedulable := true
	input := &BulkUpdateAccountsInput{
		Schedulable: &schedulable,
	}

	filtersField := reflect.ValueOf(input).Elem().FieldByName("Filters")
	require.True(t, filtersField.IsValid(), "BulkUpdateAccountsInput should expose Filters for filter-target bulk update")
	require.Equal(t, reflect.Ptr, filtersField.Kind(), "BulkUpdateAccountsInput.Filters should be a pointer field")

	filtersValue := reflect.New(filtersField.Type().Elem())
	filtersValue.Elem().FieldByName("Platform").SetString(PlatformOpenAI)
	filtersValue.Elem().FieldByName("Type").SetString(AccountTypeOAuth)
	filtersValue.Elem().FieldByName("Status").SetString(StatusActive)
	filtersValue.Elem().FieldByName("Group").SetString("12")
	filtersValue.Elem().FieldByName("PrivacyMode").SetString(PrivacyModeCFBlocked)
	filtersValue.Elem().FieldByName("Search").SetString("bulk-target")
	filtersField.Set(filtersValue)

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.True(t, repo.listCalled, "expected filter-target bulk update to resolve matching IDs via account list filters")
	require.Equal(t, PlatformOpenAI, repo.lastListFilters.platform)
	require.Equal(t, AccountTypeOAuth, repo.lastListFilters.accountType)
	require.Equal(t, StatusActive, repo.lastListFilters.status)
	require.Equal(t, "bulk-target", repo.lastListFilters.search)
	require.Equal(t, int64(12), repo.lastListFilters.groupID)
	require.Equal(t, PrivacyModeCFBlocked, repo.lastListFilters.privacyMode)
	require.Equal(t, []int64{7, 11}, repo.bulkUpdateIDs)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, []int64{7, 11}, result.SuccessIDs)
}
