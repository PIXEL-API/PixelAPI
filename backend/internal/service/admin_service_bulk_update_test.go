//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type accountRepoStubForBulkUpdate struct {
	accountRepoStub
	createdAccount   *Account
	createErr        error
	bulkUpdateErr    error
	bulkUpdateIDs    []int64
	bulkUpdateUpdate AccountBulkUpdate
	updatedAccount   *Account
	bindGroupErrByID map[int64]error
	bindGroupsCalls  []int64
	boundGroupIDs    map[int64][]int64
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
		ownerSearch string
		groupID     int64
		proxyID     int64
		privacyMode string
	}
}

func TestAdminServiceCreateAccountRejectsUnsupportedPlatform(t *testing.T) {
	svc := &adminServiceImpl{}

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{Platform: "unsupported"})

	require.ErrorIs(t, err, ErrAccountPlatformUnsupported)
}

func (s *accountRepoStubForBulkUpdate) Create(_ context.Context, account *Account) error {
	if s.createErr != nil {
		return s.createErr
	}
	if account.ID == 0 {
		account.ID = 1
	}
	clone := cloneAccountForNotice(account)
	s.createdAccount = clone
	if s.getByIDAccounts == nil {
		s.getByIDAccounts = map[int64]*Account{}
	}
	s.getByIDAccounts[account.ID] = cloneAccountForNotice(account)
	return nil
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

func (s *accountRepoStubForBulkUpdate) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	s.bindGroupsCalls = append(s.bindGroupsCalls, accountID)
	if s.boundGroupIDs == nil {
		s.boundGroupIDs = map[int64][]int64{}
	}
	s.boundGroupIDs[accountID] = append([]int64(nil), groupIDs...)
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

func (s *accountRepoStubForBulkUpdate) ListWithFilters(_ context.Context, params pagination.PaginationParams, platform, accountType, status, search, ownerSearch string, groupID, proxyID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	s.listCalled = true
	s.lastListParams = params
	s.lastListFilters.platform = platform
	s.lastListFilters.accountType = accountType
	s.lastListFilters.status = status
	s.lastListFilters.search = search
	s.lastListFilters.ownerSearch = ownerSearch
	s.lastListFilters.groupID = groupID
	s.lastListFilters.proxyID = proxyID
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
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}, {ID: 3}},
	}
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

// TestAdminService_BulkUpdateAccounts_PartialFailureIDs 验证分组绑定失败时不会报告部分成功。
func TestAdminService_BulkUpdateAccounts_PartialFailureIDs(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{{ID: 1}, {ID: 2}, {ID: 3}},
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
	require.Nil(t, result)
	require.EqualError(t, err, "bind failed")
	require.Equal(t, []int64{1, 2, 3}, repo.bulkUpdateIDs)
	require.Equal(t, []int64{1, 2}, repo.bindGroupsCalls)
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

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, 400, infraerrors.Code(err))
	require.Contains(t, err.Error(), "account_level mismatch")
	require.Nil(t, repo.updatedAccount)
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

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, 400, infraerrors.Code(err))
	require.Contains(t, err.Error(), "account_level mismatch")
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminService_UpdateAccount_KeepsExplicitOpenAILevelForGroupBinding(t *testing.T) {
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

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, repo.updatedAccount)
	require.Equal(t, AccountLevelPro, repo.updatedAccount.AccountLevel)
	require.Equal(t, []int64{20}, repo.boundGroupIDs[1])
}

func TestAdminService_CreateAccount_RejectsOpenAILevelMismatchBeforeCreate(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   20,
				Name:                 "PLUS共享号池",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelPlus,
			},
		},
	}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "free-account",
		Platform:              PlatformOpenAI,
		AccountLevel:          AccountLevelFree,
		Type:                  AccountTypeOAuth,
		Concurrency:           OpenAIPlusDefaultConcurrency,
		GroupIDs:              []int64{20},
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, account)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "ACCOUNT_GROUP_BINDING_INVALID", infraerrors.Reason(err))
	require.Contains(t, infraerrors.Message(err), "account_level mismatch")
	require.Empty(t, repo.boundGroupIDs)
}

func TestAdminService_CreateAccount_AllowsK12LevelToK12Group(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   21,
				Name:                 "K12共享号池",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelK12,
			},
		},
	}

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "k12-account",
		Platform:              PlatformOpenAI,
		AccountLevel:          AccountLevelK12,
		Type:                  AccountTypeOAuth,
		Concurrency:           OpenAIPlusDefaultConcurrency,
		GroupIDs:              []int64{21},
		SkipMixedChannelCheck: true,
	})

	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, AccountLevelK12, account.AccountLevel)
	require.Equal(t, []int64{21}, repo.boundGroupIDs[account.ID])
}

func TestAdminService_CustomOpenAILevelGroupLifecycleAllowsAccountBinding(t *testing.T) {
	levelConfigs := []OpenAIAccountLevelConfig{
		{
			Key:                "student",
			Label:              "Student",
			Aliases:            []string{"student-plan"},
			Enabled:            true,
			RequiresProxyLogin: false,
			SortOrder:          10,
		},
	}
	rawLevels, err := json.Marshal(levelConfigs)
	require.NoError(t, err)

	groupRepo := &groupRepoStubForAdmin{}
	accountRepo := &accountRepoStubForBulkUpdate{}
	svc := &adminServiceImpl{
		accountRepo: accountRepo,
		groupRepo:   groupRepo,
		settingService: &SettingService{
			settingRepo: &settingValueRepoStub{
				values: map[string]string{SettingKeyOpenAIAccountLevels: string(rawLevels)},
			},
		},
	}

	createdGroup, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                 "Student Pool",
		Platform:             PlatformOpenAI,
		RateMultiplier:       1,
		RequiredAccountLevel: "student",
	})
	require.NoError(t, err)
	require.NotNil(t, createdGroup)
	require.NotNil(t, groupRepo.created)
	require.Equal(t, "student", groupRepo.created.RequiredAccountLevel)

	groupRepo.getByID = &Group{
		ID:                   42,
		Name:                 "Student Pool",
		Platform:             PlatformOpenAI,
		RateMultiplier:       1,
		Status:               StatusActive,
		RequiredAccountLevel: "student",
	}
	updatedGroup, err := svc.UpdateGroup(context.Background(), 42, &UpdateGroupInput{
		RequiredAccountLevel: ptrString("student"),
	})
	require.NoError(t, err)
	require.NotNil(t, updatedGroup)
	require.NotNil(t, groupRepo.updated)
	require.Equal(t, "student", groupRepo.updated.RequiredAccountLevel)

	account, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                  "student-account",
		Platform:              PlatformOpenAI,
		AccountLevel:          "student",
		Type:                  AccountTypeOAuth,
		Concurrency:           OpenAIPlusDefaultConcurrency,
		GroupIDs:              []int64{42},
		SkipMixedChannelCheck: true,
	})
	require.NoError(t, err)
	require.NotNil(t, account)
	require.Equal(t, "student", account.AccountLevel)
	require.Equal(t, []int64{42}, accountRepo.boundGroupIDs[account.ID])
}

func TestAdminService_UpdateAccount_RejectsOpenAILevelMismatchBeforeUpdate(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDAccounts: map[int64]*Account{
			1: {
				ID:           1,
				Name:         "free-account",
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelFree,
				Concurrency:  OpenAIPlusDefaultConcurrency,
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   20,
				Name:                 "PLUS共享号池",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelPlus,
			},
		},
	}

	groupIDs := []int64{20}
	account, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, account)
	require.Error(t, err)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "ACCOUNT_GROUP_BINDING_INVALID", infraerrors.Reason(err))
	require.Contains(t, infraerrors.Message(err), "account_level mismatch")
	require.Nil(t, repo.updatedAccount)
	require.Empty(t, repo.boundGroupIDs)
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
	require.Equal(t, 400, infraerrors.Code(err))
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
	require.Equal(t, 400, infraerrors.Code(err))
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
		listResult:       &pagination.PaginationResult{Total: 2},
		getByIDsAccounts: []*Account{{ID: 7}, {ID: 11}},
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
	filtersValue.Elem().FieldByName("ProxyID").SetInt(34)
	filtersValue.Elem().FieldByName("PrivacyMode").SetString(PrivacyModeCFBlocked)
	filtersValue.Elem().FieldByName("Search").SetString("bulk-target")
	filtersValue.Elem().FieldByName("OwnerSearch").SetString("owner@example.com")
	filtersField.Set(filtersValue)

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.True(t, repo.listCalled, "expected filter-target bulk update to resolve matching IDs via account list filters")
	require.Equal(t, PlatformOpenAI, repo.lastListFilters.platform)
	require.Equal(t, AccountTypeOAuth, repo.lastListFilters.accountType)
	require.Equal(t, StatusActive, repo.lastListFilters.status)
	require.Equal(t, "bulk-target", repo.lastListFilters.search)
	require.Equal(t, "owner@example.com", repo.lastListFilters.ownerSearch)
	require.Equal(t, int64(12), repo.lastListFilters.groupID)
	require.Equal(t, int64(34), repo.lastListFilters.proxyID)
	require.Equal(t, PrivacyModeCFBlocked, repo.lastListFilters.privacyMode)
	require.Equal(t, []int64{7, 11}, repo.bulkUpdateIDs)
	require.Equal(t, 2, result.Success)
	require.Equal(t, 0, result.Failed)
	require.Equal(t, []int64{7, 11}, result.SuccessIDs)
}

func TestAdminServiceBulkUpdateAccounts_ResolvesIDsFromUnassignedProxyFilter(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		listData:         []Account{{ID: 7}},
		listResult:       &pagination.PaginationResult{Total: 1},
		getByIDsAccounts: []*Account{{ID: 7}},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	schedulable := true
	input := &BulkUpdateAccountsInput{
		Filters: &BulkUpdateAccountFilters{
			ProxyID: AccountListProxyUnassigned,
		},
		Schedulable: &schedulable,
	}

	result, err := svc.BulkUpdateAccounts(context.Background(), input)
	require.NoError(t, err)
	require.True(t, repo.listCalled)
	require.Equal(t, AccountListProxyUnassigned, repo.lastListFilters.proxyID)
	require.Equal(t, []int64{7}, repo.bulkUpdateIDs)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 0, result.Failed)
}

func TestAdminServiceBulkUpdateAccounts_OpenAIPlusConcurrencyAllowsAdminConfiguredValue(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{
				ID:           1,
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPlus,
				Concurrency:  OpenAIPlusDefaultConcurrency,
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	concurrency := 5
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{1},
		Concurrency: &concurrency,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []int64{1}, repo.bulkUpdateIDs)
	require.NotNil(t, repo.bulkUpdateUpdate.Concurrency)
	require.Equal(t, 5, *repo.bulkUpdateUpdate.Concurrency)
}

func TestAdminServiceBulkUpdateAccounts_NonOpenAILoadFactorTooLargeReturnsBadRequest(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{
				ID:           1,
				Platform:     PlatformAnthropic,
				AccountLevel: AccountLevelUnknown,
				Concurrency:  3,
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	loadFactor := 10001
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		LoadFactor: &loadFactor,
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, 400, infraerrors.Code(err))
	require.Equal(t, "load_factor must be <= 10000", infraerrors.Message(err))
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminServiceBulkUpdateAccounts_PreservesClearLoadFactorIntent(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{
				ID:           1,
				Platform:     PlatformAnthropic,
				AccountLevel: AccountLevelUnknown,
				Concurrency:  3,
				LoadFactor:   intPtrHelper(9),
			},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	loadFactor := 0
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1},
		LoadFactor: &loadFactor,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, repo.bulkUpdateUpdate.LoadFactor)
	require.Equal(t, 0, *repo.bulkUpdateUpdate.LoadFactor)
}

func TestAdminService_BulkUpdateAccounts_RejectsHigherOpenAILevelIntoLowerPool(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{
			{
				ID:           1,
				Name:         "pro-account",
				Platform:     PlatformOpenAI,
				AccountLevel: AccountLevelPro,
				Concurrency:  1,
			},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		groupRepo: &groupRepoStubForAdmin{
			getByID: &Group{
				ID:                   20,
				Name:                 "Plus Pool",
				Platform:             PlatformOpenAI,
				RequiredAccountLevel: AccountLevelPlus,
			},
		},
	}

	groupIDs := []int64{20}
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:            []int64{1},
		GroupIDs:              &groupIDs,
		SkipMixedChannelCheck: true,
	})

	require.Nil(t, result)
	require.Error(t, err)
	require.Equal(t, 400, infraerrors.Code(err))
	require.Contains(t, err.Error(), "account_level mismatch")
	require.Empty(t, repo.bulkUpdateIDs)
	require.Empty(t, repo.boundGroupIDs)
}

func newAdminOwnedAgentIdentityTestAccount(t *testing.T, ownerUserID int64, shareMode, shareStatus, runtimeID string) *Account {
	t.Helper()
	return &Account{
		ID:           1,
		Name:         "owned-agent-identity",
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		AccountLevel: AccountLevelTeam,
		Credentials: map[string]any{
			"auth_mode":          OpenAIAuthModeAgentIdentity,
			"agent_runtime_id":   runtimeID,
			"agent_private_key":  testAgentIdentityPrivateKey(t),
			"task_id":            "task-old",
			"chatgpt_account_id": "team-a",
			"chatgpt_user_id":    "member-a",
		},
		OwnerUserID: &ownerUserID,
		ShareMode:   shareMode,
		ShareStatus: shareStatus,
		Concurrency: 3,
		Status:      StatusActive,
		Schedulable: true,
	}
}

func TestAdminServiceUpdateOwnedPublicAgentIdentityAuthChangeForcesPendingAndInvalidatesWS(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	invalidator := &recordingAgentIdentityWSInvalidator{}
	svc := &adminServiceImpl{accountRepo: repo, agentIdentityWSInvalidator: invalidator}
	credentials := mergeAccountMap(account.Credentials, map[string]any{"agent_runtime_id": "runtime-new"})

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{Credentials: credentials})

	require.NoError(t, err)
	require.Equal(t, AccountShareStatusPending, updated.ShareStatus)
	require.Equal(t, "runtime-new", updated.GetCredential("agent_runtime_id"))
	require.Equal(t, []int64{account.ID}, invalidator.accountIDs)
}

func TestAdminServiceUpdateOwnedPublicAgentIdentityToPrivateInvalidatesWS(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	invalidator := &recordingAgentIdentityWSInvalidator{}
	svc := &adminServiceImpl{accountRepo: repo, agentIdentityWSInvalidator: invalidator}

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{ShareMode: AccountShareModePrivate})

	require.NoError(t, err)
	require.Equal(t, AccountShareModePrivate, updated.ShareMode)
	require.Equal(t, AccountShareStatusApproved, updated.ShareStatus)
	require.Equal(t, []int64{account.ID}, invalidator.accountIDs)
}

func TestAdminServiceUpdateOwnedPublicAgentIdentitySuspensionIsPreserved(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	invalidator := &recordingAgentIdentityWSInvalidator{}
	svc := &adminServiceImpl{accountRepo: repo, agentIdentityWSInvalidator: invalidator}

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{ShareStatus: AccountShareStatusSuspended})

	require.NoError(t, err)
	require.Equal(t, AccountShareStatusSuspended, updated.ShareStatus)
	require.Equal(t, []int64{account.ID}, invalidator.accountIDs)
}

func TestAdminServiceUpdateOwnedPublicAgentIdentityExplicitApprovalForcesPending(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	invalidator := &recordingAgentIdentityWSInvalidator{}
	svc := &adminServiceImpl{accountRepo: repo, agentIdentityWSInvalidator: invalidator}

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{ShareStatus: AccountShareStatusApproved})

	require.NoError(t, err)
	require.Equal(t, AccountShareStatusPending, updated.ShareStatus)
	require.Equal(t, []int64{account.ID}, invalidator.accountIDs)
}

func TestAdminServiceUpdateOwnedPublicAgentIdentityOwnerChangeForcesPending(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	invalidator := &recordingAgentIdentityWSInvalidator{}
	svc := &adminServiceImpl{accountRepo: repo, agentIdentityWSInvalidator: invalidator}
	nextOwnerUserID := int64(202)

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{OwnerUserID: &nextOwnerUserID})

	require.NoError(t, err)
	require.NotNil(t, updated.OwnerUserID)
	require.Equal(t, nextOwnerUserID, *updated.OwnerUserID)
	require.Equal(t, AccountShareStatusPending, updated.ShareStatus)
	require.Equal(t, []int64{account.ID}, invalidator.accountIDs)
}

func TestAdminServiceUpdateOwnedAgentIdentityFailsBeforeWriteWithoutWSInvalidator(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{ShareMode: AccountShareModePrivate})

	require.Nil(t, updated)
	require.ErrorIs(t, err, ErrOwnedAgentIdentityWSInvalidatorUnavailable)
	require.Nil(t, repo.updatedAccount)
}

func TestAdminServiceUpdateExternalPlacementIdentityRequiresConversion(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePrivate, AccountShareStatusApproved, "runtime-old")
	account.ExternalPlacement = &AccountExternalPlacement{
		Target: AccountExternalPlacementRoom,
		State:  "active",
	}
	repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{account.ID: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	level := AccountLevelPlus

	updated, err := svc.UpdateAccount(context.Background(), account.ID, &UpdateAccountInput{AccountLevel: &level})

	require.Nil(t, updated)
	require.ErrorIs(t, err, ErrOwnedAccountPlacementConversionRequired)
	require.Nil(t, repo.updatedAccount)
}

func TestAdminServiceBulkUpdateExternalPlacementIdentityRequiresConversion(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePublic, AccountShareStatusApproved, "runtime-old")
	account.ExternalPlacement = &AccountExternalPlacement{
		Target: AccountExternalPlacementPublicPool,
		State:  "active",
	}
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
	svc := &adminServiceImpl{accountRepo: repo}
	level := AccountLevelPlus

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:   []int64{account.ID},
		AccountLevel: &level,
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrOwnedAccountPlacementConversionRequired)
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminServiceBulkUpdateOwnedAgentIdentityAuthChangeFailsBeforeWrite(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePrivate, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{account.ID},
		Credentials: map[string]any{"agent_runtime_id": "runtime-new"},
	})

	require.Nil(t, result)
	require.True(t, infraerrors.IsBadRequest(err))
	require.Equal(t, "ACCOUNT_BULK_OWNED_AGENT_IDENTITY_AUTH_UPDATE_UNSUPPORTED", infraerrors.Reason(err))
	require.Empty(t, repo.bulkUpdateIDs)
}

func TestAdminServiceBulkUpdateOwnedAgentIdentityNonAuthCredentialsDoesNotFalseReject(t *testing.T) {
	account := newAdminOwnedAgentIdentityTestAccount(t, 101, AccountShareModePrivate, AccountShareStatusApproved, "runtime-old")
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{account}}
	svc := &adminServiceImpl{accountRepo: repo}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{account.ID},
		Credentials: map[string]any{"email": "updated@example.com"},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, []int64{account.ID}, repo.bulkUpdateIDs)
	require.Equal(t, "updated@example.com", repo.bulkUpdateUpdate.Credentials["email"])
}
