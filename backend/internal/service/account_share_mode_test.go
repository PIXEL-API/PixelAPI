package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type accountShareModeRepoStub struct {
	ensureNameErr        error
	modeGroup            *bool
	modeGroups           map[string]*Group
	modeGroupGetCalls    []string
	modeGroupEnsureCalls []string
	isModeCalls          int
	bindingCalls         int
	activationCalls      int
	bindingResults       []accountShareModeBindingResult
	membership           *AccountShareMembership
	listing              *AccountShareListing
	getListingIDs        []int64
	getListingViewerIDs  []int64
	listingsByPage       map[int][]AccountShareListing
	listPages            []int
	listParams           []pagination.PaginationParams
	listFilters          AccountShareListingFilters
	spendQuery           AccountShareMySpendQuery
	spendSummary         *AccountShareMySpendSummary
	spendErr             error
	updateAdmin          bool
	updateCalls          int
	updateInput          UpdateAccountShareListingInput
	updateListing        *AccountShareListing
	beginInput           BeginAccountShareListingEditInput
	beginActorIsAdmin    bool
	beginListing         *AccountShareListing
	beginErr             error
	endSnapshot          *AccountShareMembership
	endMembership        *AccountShareMembership
	endBilling           *AccountShareSeatBillingResult
	endInput             BeginAccountShareMembershipEndInput
	endErr               error
	endCalls             int
	finalizeMembership   *AccountShareMembership
	finalizeBilling      *AccountShareSeatBillingResult
	finalizeDone         bool
	finalizeErr          error
	finalizeCalls        int
	finalizeOperationID  string
	endingCandidates     []AccountShareEndingMembershipCandidate
	endingCandidatesErr  error
	submitReview         *AccountShareReview
	submitReviewInput    SubmitAccountShareReviewInput
	submitReviewCalls    int
	submitReviewErr      error
	requestBillingCalls  int
	requestBillingErr    error
	waiverCompCalls      int
	waiverCompLimit      int
	unavailableCalls     int
	recoverableIDs       []int64
	recoverableSuspend   *AccountShareMembership
	recoverableCalls     int
	dispatchFailureCalls int
	touchCalls           int
	touchTimes           []time.Time
	touchSignal          chan time.Time
	touchErr             error
	createdAccount       *Account
	createdListing       *AccountShareListing
	createdModeGroupID   int64
	joinInput            AccountShareJoinRepositoryInput
	joinMembership       *AccountShareMembership
	joinErr              error
	revisionTerms        *AccountShareListingTermsSnapshot
	revisionTermsErr     error
	policy               *AccountSharePolicy
	policyErr            error
}

type accountShareBillingLifecycleRepoStub struct {
	AccountShareModeRepository
	endingCalls    int
	lifecycleCalls int
}

func (r *accountShareBillingLifecycleRepoStub) ListEndingMembershipCandidates(
	ctx context.Context,
	limit int,
) ([]AccountShareEndingMembershipCandidate, error) {
	r.endingCalls++
	return r.AccountShareModeRepository.ListEndingMembershipCandidates(ctx, limit)
}

func (r *accountShareBillingLifecycleRepoStub) GetRoomManagementState(
	context.Context,
	int64,
	bool,
	int64,
) (*AccountShareRoomManagementState, error) {
	return nil, ErrServiceUnavailable
}

func (r *accountShareBillingLifecycleRepoStub) TransitionRoomLifecycle(
	context.Context,
	int64,
	bool,
	int64,
	string,
	AccountShareRoomLifecycleCommandInput,
) (*AccountShareListing, error) {
	return nil, ErrServiceUnavailable
}

func (r *accountShareBillingLifecycleRepoStub) FinalizeDrainingRoom(
	context.Context,
	int64,
	int64,
) (*AccountShareListing, error) {
	return nil, ErrServiceUnavailable
}

func (r *accountShareBillingLifecycleRepoStub) ListDrainingRoomIDs(
	context.Context,
	int,
) ([]int64, error) {
	r.lifecycleCalls++
	return nil, nil
}

func (r *accountShareBillingLifecycleRepoStub) ListValidatingRoomIDs(
	context.Context,
	time.Time,
	int,
) ([]int64, error) {
	return nil, nil
}

func (r *accountShareBillingLifecycleRepoStub) SoftDeleteRoom(
	context.Context,
	int64,
	bool,
	int64,
	AccountShareRoomDeleteInput,
) (*AccountShareRoomOperation, error) {
	return nil, ErrServiceUnavailable
}

func (r *accountShareBillingLifecycleRepoStub) FinalizeRoomDeletion(
	context.Context,
	int64,
	string,
) (*AccountShareRoomOperation, error) {
	return nil, ErrServiceUnavailable
}

func (r *accountShareBillingLifecycleRepoStub) ListPendingRoomDeletionOperations(
	context.Context,
	int,
) ([]AccountShareRoomOperation, error) {
	return nil, nil
}

func (r *accountShareBillingLifecycleRepoStub) GetRoomOperation(
	context.Context,
	int64,
	bool,
	string,
) (*AccountShareRoomOperation, error) {
	return nil, ErrServiceUnavailable
}

type accountShareBillingGuardRepoStub struct {
	ClusterRepository
	renewCalls int
}

func (r *accountShareBillingGuardRepoStub) RenewTaskLease(
	context.Context,
	string,
	string,
	string,
	string,
	int64,
	time.Duration,
) (bool, error) {
	r.renewCalls++
	return true, nil
}

type accountShareHistoryRepoStub struct {
	AccountShareModeRepository
	entries        []AccountShareMembershipHistoryEntry
	result         *pagination.PaginationResult
	err            error
	consumerUserID int64
	params         pagination.PaginationParams
	calls          int
}

func (r *accountShareHistoryRepoStub) ListMembershipHistory(
	_ context.Context,
	consumerUserID int64,
	params pagination.PaginationParams,
) ([]AccountShareMembershipHistoryEntry, *pagination.PaginationResult, error) {
	r.calls++
	r.consumerUserID = consumerUserID
	r.params = params
	return append([]AccountShareMembershipHistoryEntry(nil), r.entries...), r.result, r.err
}

var _ AccountShareModeRepository = (*accountShareHistoryRepoStub)(nil)
var _ AccountShareHistoryRepository = (*accountShareHistoryRepoStub)(nil)

type accountShareRoomRepoStub struct {
	*accountShareModeRepoStub
	AccountShareRoomRepository
	roomAccountsViewerUserID  int64
	roomAccountsViewerIsAdmin bool
	roomAccountsListingID     int64
	roomAccounts              []AccountShareRoomAccount
	roomAccountsErr           error
	attachBatchInput          BatchAccountShareRoomAccountsInput
	attachBatchCalls          int
	attachBatchErr            error
	detachBatchInput          BatchAccountShareRoomAccountsInput
	detachBatchCalls          int
	detachBatchBilling        *AccountShareSeatBillingResult
	detachBatchErr            error
}

type accountShareOwnedAccountRepoStub struct {
	AccountRepository
	account       *Account
	accounts      []*Account
	calls         int
	getByIDsCalls int
}

func (r *accountShareOwnedAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	r.calls++
	if r.account == nil {
		return nil, ErrAccountNotFound
	}
	account := *r.account
	return &account, nil
}

func (r *accountShareOwnedAccountRepoStub) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	r.getByIDsCalls++
	accountsByID := make(map[int64]*Account, len(r.accounts)+1)
	if r.account != nil {
		accountsByID[r.account.ID] = r.account
	}
	for _, account := range r.accounts {
		if account != nil {
			accountsByID[account.ID] = account
		}
	}
	result := make([]*Account, 0, len(ids))
	for _, id := range ids {
		account := accountsByID[id]
		if account == nil {
			continue
		}
		cloned := *account
		result = append(result, &cloned)
	}
	return result, nil
}

type accountShareEditRuntimeRepoStub struct {
	AccountShareModeRepository
	accountShareLifecycleRepository
	state      *AccountShareRoomManagementState
	stateErr   error
	stateCalls int
	beginCalls int
}

func (r *accountShareEditRuntimeRepoStub) GetRoomManagementState(
	context.Context,
	int64,
	bool,
	int64,
) (*AccountShareRoomManagementState, error) {
	r.stateCalls++
	if r.stateErr != nil {
		return nil, r.stateErr
	}
	if r.state == nil {
		return nil, ErrAccountShareListingNotFound
	}
	state := *r.state
	state.RuntimeMembershipIDs = append([]int64(nil), r.state.RuntimeMembershipIDs...)
	state.RuntimeAccountIDs = append([]int64(nil), r.state.RuntimeAccountIDs...)
	return &state, nil
}

func (r *accountShareEditRuntimeRepoStub) BeginListingEdit(
	_ context.Context,
	actorUserID int64,
	_ bool,
	listingID int64,
	input BeginAccountShareListingEditInput,
) (*AccountShareListing, error) {
	r.beginCalls++
	return &AccountShareListing{
		ID:              listingID,
		OwnerUserID:     actorUserID,
		EditSessionID:   input.SessionID,
		EditingByUserID: &actorUserID,
	}, nil
}

func (r *accountShareRoomRepoStub) ListRoomAccounts(_ context.Context, listingID, viewerUserID int64, viewerIsAdmin bool) ([]AccountShareRoomAccount, error) {
	r.roomAccountsListingID = listingID
	r.roomAccountsViewerUserID = viewerUserID
	r.roomAccountsViewerIsAdmin = viewerIsAdmin
	return append([]AccountShareRoomAccount(nil), r.roomAccounts...), r.roomAccountsErr
}

func (r *accountShareRoomRepoStub) AttachRoomAccountsAtomic(
	_ context.Context,
	input BatchAccountShareRoomAccountsInput,
) error {
	r.attachBatchCalls++
	r.attachBatchInput = input
	return r.attachBatchErr
}

func (r *accountShareRoomRepoStub) DetachRoomAccountsAtomic(
	_ context.Context,
	input BatchAccountShareRoomAccountsInput,
) (*AccountShareSeatBillingResult, error) {
	r.detachBatchCalls++
	r.detachBatchInput = input
	return r.detachBatchBilling, r.detachBatchErr
}

type accountShareModeBindingResult struct {
	membership *AccountShareMembership
	listing    *AccountShareListing
	err        error
}

type accountShareModeProxyRepoStub struct {
	proxy            *Proxy
	createCalls      int
	getVisibleUserID int64
	getVisibleID     int64
	getVisibleCalls  int
	getVisibleErr    error
	accountCount     int64
	countCalls       int
	countErr         error
	updateCalls      int
	updateErr        error
	deleteCalls      int
	deletedID        int64
	deleteErr        error
}

type accountShareModeTesterStub struct {
	mu         sync.Mutex
	calls      int
	accountID  int64
	modelID    string
	accountIDs []int64
	modelIDs   []string
	result     *ScheduledTestResult
	err        error
}

func (s *accountShareModeTesterStub) RunTestBackground(_ context.Context, accountID int64, modelID string) (*ScheduledTestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.accountID = accountID
	s.modelID = modelID
	s.accountIDs = append(s.accountIDs, accountID)
	s.modelIDs = append(s.modelIDs, modelID)
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &ScheduledTestResult{Status: "success"}, nil
}

type accountShareModeRecoveryStub struct {
	calls      int
	accountID  int64
	accountIDs []int64
	err        error
}

type accountShareReviewSettingRepoStub struct {
	values map[string]string
}

type accountShareMembershipConcurrencyCacheStub struct {
	ConcurrencyCache
	acquireCalls           int
	releaseCalls           int
	accountRefreshCalls    int
	membershipRefreshCalls int
	current                int
	currentErr             error
	refreshErr             error
	refreshLost            bool
	leaseTTL               time.Duration
	invalidLeaseTTL        bool
}

func (s *accountShareMembershipConcurrencyCacheStub) AcquireAccountShareMembershipSlot(context.Context, int64, int, string) (bool, error) {
	s.acquireCalls++
	return true, nil
}

func (s *accountShareMembershipConcurrencyCacheStub) ReleaseAccountShareMembershipSlot(context.Context, int64, string) error {
	s.releaseCalls++
	return nil
}

func (s *accountShareMembershipConcurrencyCacheStub) GetAccountShareMembershipConcurrency(context.Context, int64) (int, error) {
	return s.current, s.currentErr
}

func (s *accountShareMembershipConcurrencyCacheStub) RefreshAccountSlot(context.Context, int64, string) (bool, error) {
	s.accountRefreshCalls++
	return !s.refreshLost, s.refreshErr
}

func (s *accountShareMembershipConcurrencyCacheStub) RefreshAccountShareMembershipSlot(context.Context, int64, string) (bool, error) {
	s.membershipRefreshCalls++
	return !s.refreshLost, s.refreshErr
}

func (s *accountShareMembershipConcurrencyCacheStub) SlotLeaseTTL() time.Duration {
	if s.invalidLeaseTTL {
		return 0
	}
	if s.leaseTTL > 0 {
		return s.leaseTTL
	}
	return time.Minute
}

type accountShareMembershipNoLeaseCacheStub struct {
	ConcurrencyCache
	acquireCalls int
	releaseCalls int
}

func (s *accountShareMembershipNoLeaseCacheStub) AcquireAccountShareMembershipSlot(context.Context, int64, int, string) (bool, error) {
	s.acquireCalls++
	return true, nil
}

func (s *accountShareMembershipNoLeaseCacheStub) ReleaseAccountShareMembershipSlot(context.Context, int64, string) error {
	s.releaseCalls++
	return nil
}

func (s *accountShareMembershipNoLeaseCacheStub) GetAccountShareMembershipConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

type accountShareRecommendationAPIKeyRepoStub struct {
	APIKeyRepository
	key   *APIKey
	err   error
	calls int
}

type accountShareJoinUserRepoStub struct {
	UserRepository
	user *User
	err  error
}

func (s *accountShareRecommendationAPIKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.key != nil {
		key := *s.key
		return &key, nil
	}
	return nil, ErrAPIKeyNotFound
}

func (s *accountShareJoinUserRepoStub) GetByID(context.Context, int64) (*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.user == nil {
		return nil, ErrUserNotFound
	}
	user := *s.user
	return &user, nil
}

type accountShareRecommendationUsageProfileRepoStub struct {
	stats     *AccountShareRecommendationUsageProfileStats
	err       error
	calls     int
	userID    int64
	model     string
	startTime time.Time
	endTime   time.Time
}

func (s *accountShareRecommendationUsageProfileRepoStub) GetAccountShareRecommendationUsageProfile(_ context.Context, userID int64, model string, startTime, endTime time.Time) (*AccountShareRecommendationUsageProfileStats, error) {
	s.calls++
	s.userID = userID
	s.model = model
	s.startTime = startTime
	s.endTime = endTime
	if s.err != nil {
		return nil, s.err
	}
	return s.stats, nil
}

func (s *accountShareModeRecoveryStub) RecoverAccountAfterSuccessfulTest(_ context.Context, accountID int64) (*SuccessfulTestRecoveryResult, error) {
	s.calls++
	s.accountID = accountID
	s.accountIDs = append(s.accountIDs, accountID)
	if s.err != nil {
		return nil, s.err
	}
	return &SuccessfulTestRecoveryResult{ClearedError: true}, nil
}

func (s *accountShareReviewSettingRepoStub) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *accountShareReviewSettingRepoStub) GetValue(context.Context, string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *accountShareReviewSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *accountShareReviewSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (s *accountShareReviewSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *accountShareReviewSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	result := make(map[string]string, len(s.values))
	for key, value := range s.values {
		result[key] = value
	}
	return result, nil
}

func (s *accountShareReviewSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func (r *accountShareModeProxyRepoStub) Create(_ context.Context, proxy *Proxy) error {
	r.createCalls++
	if proxy.ID <= 0 {
		proxy.ID = 7
	}
	r.proxy = proxy
	return nil
}

func (r *accountShareModeProxyRepoStub) Update(_ context.Context, proxy *Proxy) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	r.proxy = proxy
	return nil
}

func (r *accountShareModeProxyRepoStub) Delete(_ context.Context, id int64) error {
	r.deleteCalls++
	r.deletedID = id
	return r.deleteErr
}

func (r *accountShareModeProxyRepoStub) GetVisibleByID(_ context.Context, userID, id int64) (*Proxy, error) {
	r.getVisibleUserID = userID
	r.getVisibleID = id
	r.getVisibleCalls++
	if r.getVisibleErr != nil {
		return nil, r.getVisibleErr
	}
	if r.proxy != nil {
		return r.proxy, nil
	}
	return &Proxy{ID: 7, Name: "proxy", Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Status: StatusActive}, nil
}

func (r *accountShareModeProxyRepoStub) ListActiveVisibleWithAccountCount(context.Context, int64) ([]ProxyWithAccountCount, error) {
	if r.proxy != nil {
		return []ProxyWithAccountCount{{Proxy: *r.proxy}}, nil
	}
	return []ProxyWithAccountCount{}, nil
}

func (r *accountShareModeProxyRepoStub) FindVisibleActiveByEndpoint(context.Context, int64, string, string, int, string, string) (*Proxy, error) {
	if r.proxy != nil {
		return r.proxy, nil
	}
	return nil, ErrProxyNotFound
}

func (r *accountShareModeProxyRepoStub) CountAccountsByProxyID(_ context.Context, proxyID int64) (int64, error) {
	r.countCalls++
	if r.proxy != nil && r.proxy.ID != 0 && r.proxy.ID != proxyID {
		return 0, ErrProxyNotFound
	}
	if r.countErr != nil {
		return 0, r.countErr
	}
	return r.accountCount, nil
}

func (r *accountShareModeRepoStub) EnsureModeGroup(_ context.Context, platform string) (*Group, error) {
	r.modeGroupEnsureCalls = append(r.modeGroupEnsureCalls, platform)
	return &Group{ID: 1, Platform: platform}, nil
}

func (r *accountShareModeRepoStub) GetModeGroup(_ context.Context, platform string) (*Group, error) {
	r.modeGroupGetCalls = append(r.modeGroupGetCalls, platform)
	if r.modeGroups != nil {
		group := r.modeGroups[platform]
		if group == nil {
			return nil, ErrAccountShareModeGroupUnavailable
		}
		clone := *group
		return &clone, nil
	}
	return &Group{ID: 1, Platform: platform}, nil
}

func (r *accountShareModeRepoStub) IsModeGroup(context.Context, int64) (bool, error) {
	r.isModeCalls++
	if r.modeGroup != nil {
		return *r.modeGroup, nil
	}
	return true, nil
}

func (r *accountShareModeRepoStub) EnsureListingNameAvailable(context.Context, int64, string) error {
	return r.ensureNameErr
}

func (r *accountShareModeRepoStub) CreatePlatformListing(_ context.Context, account *Account, listing *AccountShareListing, modeGroupID int64) (*AccountShareListing, error) {
	if account == nil || listing == nil {
		return nil, ErrServiceUnavailable
	}
	accountCopy := *account
	if accountCopy.ID <= 0 {
		accountCopy.ID = 101
	}
	listingCopy := *listing
	if listingCopy.ID <= 0 {
		listingCopy.ID = 201
	}
	if listingCopy.AccountID <= 0 {
		listingCopy.AccountID = accountCopy.ID
	}
	if listingCopy.Platform == "" {
		listingCopy.Platform = accountCopy.Platform
	}
	if listingCopy.AccountName == "" {
		listingCopy.AccountName = accountCopy.Name
	}
	listingCopy.AllowedModels = append([]string(nil), listing.AllowedModels...)
	r.createdAccount = &accountCopy
	r.createdListing = &listingCopy
	r.createdModeGroupID = modeGroupID
	return &listingCopy, nil
}

func (r *accountShareModeRepoStub) GetListingByID(
	_ context.Context,
	listingID int64,
	viewerUserID int64,
) (*AccountShareListing, error) {
	r.getListingIDs = append(r.getListingIDs, listingID)
	r.getListingViewerIDs = append(r.getListingViewerIDs, viewerUserID)
	if r.listing != nil {
		return r.listing, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) GetListingByAccountID(context.Context, int64) (*AccountShareListing, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) ListListings(_ context.Context, _ int64, filters AccountShareListingFilters, params pagination.PaginationParams) ([]AccountShareListing, *pagination.PaginationResult, error) {
	r.listFilters = filters
	r.listPages = append(r.listPages, params.Page)
	r.listParams = append(r.listParams, params)
	if r.listingsByPage != nil {
		page := params.Page
		if page < 1 {
			page = 1
		}
		items := append([]AccountShareListing(nil), r.listingsByPage[page]...)
		totalPages := 0
		for pageNumber := range r.listingsByPage {
			if pageNumber > totalPages {
				totalPages = pageNumber
			}
		}
		if totalPages == 0 {
			totalPages = 1
		}
		return items, &pagination.PaginationResult{
			Total:    int64(totalPages * params.Limit()),
			Page:     page,
			PageSize: params.Limit(),
			Pages:    totalPages,
		}, nil
	}
	return nil, &pagination.PaginationResult{}, nil
}

func (r *accountShareModeRepoStub) GetMySpendSummary(_ context.Context, query AccountShareMySpendQuery) (*AccountShareMySpendSummary, error) {
	r.spendQuery = query
	if r.spendErr != nil {
		return nil, r.spendErr
	}
	if r.spendSummary != nil {
		summary := *r.spendSummary
		return &summary, nil
	}
	return &AccountShareMySpendSummary{
		Range:          query.Range,
		StartTime:      query.StartTime,
		EndTime:        query.EndTime,
		Listing:        AccountShareMySpendListing{ID: query.ListingID},
		ModelBreakdown: []AccountShareMySpendModelBreakdown{},
	}, nil
}

func TestListRoomAccountsForwardsAdministratorPermission(t *testing.T) {
	repo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		roomAccounts: []AccountShareRoomAccount{
			{AccountID: 10, AccountName: "room-account"},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	accounts, err := svc.ListRoomAccounts(context.Background(), 99, true, 700)

	require.NoError(t, err)
	require.Equal(t, int64(700), repo.roomAccountsListingID)
	require.Equal(t, int64(99), repo.roomAccountsViewerUserID)
	require.True(t, repo.roomAccountsViewerIsAdmin)
	require.Equal(t, repo.roomAccounts, accounts)
}

func TestAttachRoomAccountsUsesOneAtomicRepositoryCallAndReturnsOnlySuccesses(t *testing.T) {
	repo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	result, err := svc.AttachRoomAccounts(context.Background(), BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{11, 10, 11, 0},
		OwnerUserID:    42,
		IdempotencyKey: " attach-atomic ",
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.attachBatchCalls)
	require.Equal(t, []int64{11, 10}, repo.attachBatchInput.AccountIDs)
	require.Equal(t, "attach-atomic", repo.attachBatchInput.IdempotencyKey)
	require.Equal(t, 2, result.Success)
	require.Zero(t, result.Failed)
	require.Equal(t, []int64{11, 10}, result.SuccessIDs)
	require.Empty(t, result.FailedIDs)
	require.Equal(t, []BulkUpdateAccountResult{
		{AccountID: 11, Success: true},
		{AccountID: 10, Success: true},
	}, result.Results)
}

func TestAttachRoomAccountsAtomicFailureReturnsErrorWithoutPartialResult(t *testing.T) {
	atomicErr := ErrAccountShareRoomLevelMismatch
	repo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		attachBatchErr:           atomicErr,
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	result, err := svc.AttachRoomAccounts(context.Background(), BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{10, 11},
		OwnerUserID:    42,
		IdempotencyKey: "attach-atomic-failure",
	})

	require.ErrorIs(t, err, atomicErr)
	require.Nil(t, result)
	require.Equal(t, 1, repo.attachBatchCalls)
}

func TestDetachRoomAccountsUsesOneAtomicRepositoryCall(t *testing.T) {
	repo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		detachBatchBilling: &AccountShareSeatBillingResult{
			DebitUserIDs:         []int64{50},
			CreditUserIDs:        []int64{42},
			EndedConsumerUserIDs: []int64{50},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	result, err := svc.DetachRoomAccounts(context.Background(), BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{11, 10},
		OwnerUserID:    42,
		IdempotencyKey: "detach-atomic",
	})

	require.NoError(t, err)
	require.Equal(t, 1, repo.detachBatchCalls)
	require.Equal(t, []int64{11, 10}, repo.detachBatchInput.AccountIDs)
	require.Equal(t, 2, result.Success)
	require.Zero(t, result.Failed)
	require.Empty(t, result.FailedIDs)
}

func TestMutateRoomAccountsRejectsBlankIdempotencyKeyBeforeRepositoryCall(t *testing.T) {
	repo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	result, err := svc.AttachRoomAccounts(context.Background(), BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{10},
		OwnerUserID:    42,
		IdempotencyKey: "   ",
	})

	require.ErrorIs(t, err, ErrIdempotencyKeyRequired)
	require.Nil(t, result)
	require.Zero(t, repo.attachBatchCalls)
}

func TestCreateRoomFromOwnedAccountFailsClosedWithoutConcurrencyService(t *testing.T) {
	ownerUserID := int64(42)
	modeRepo := &accountShareModeRepoStub{}
	roomRepo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: modeRepo,
	}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account: &Account{
			ID:           70,
			Name:         "owned-account",
			Platform:     PlatformAnthropic,
			AccountLevel: AccountLevelPro,
			OwnerUserID:  &ownerUserID,
			Status:       StatusActive,
			Schedulable:  true,
			Concurrency:  5,
		},
	}
	svc := NewAccountShareModeService(roomRepo, accountRepo, nil, nil, nil, nil)

	listing, err := svc.CreateRoomFromOwnedAccount(
		context.Background(),
		ownerUserID,
		CreateAccountShareRoomInput{
			AccountID:          70,
			IdempotencyKey:     "create-room-with-runtime-fence",
			RoomName:           "room-a",
			SeatLimit:          1,
			RateMultiplier:     1,
			AllowedModels:      []string{"claude-sonnet-4-20250514"},
			PerUserConcurrency: 1,
		},
	)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrServiceUnavailable)
	require.Equal(t, 1, accountRepo.calls)
	require.Empty(t, modeRepo.modeGroupEnsureCalls)
}

func TestCreateRoomFromOwnedAccountRejectsUnsupportedModelBeforeRuntimeMutation(t *testing.T) {
	ownerUserID := int64(42)
	modeRepo := &accountShareModeRepoStub{}
	roomRepo := &accountShareRoomRepoStub{accountShareModeRepoStub: modeRepo}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account: &Account{
			ID:           70,
			Name:         "owned-account",
			Platform:     PlatformOpenAI,
			AccountLevel: AccountLevelPro,
			OwnerUserID:  &ownerUserID,
			Status:       StatusActive,
			Schedulable:  true,
			Concurrency:  5,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"},
			},
		},
	}
	svc := NewAccountShareModeService(roomRepo, accountRepo, nil, nil, nil, nil)

	listing, err := svc.CreateRoomFromOwnedAccount(
		context.Background(),
		ownerUserID,
		CreateAccountShareRoomInput{
			AccountID:          70,
			IdempotencyKey:     "unsupported-room-model",
			RoomName:           "room-a",
			SeatLimit:          1,
			RateMultiplier:     1,
			AllowedModels:      []string{"gpt-5.4"},
			PerUserConcurrency: 1,
		},
	)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareModeUnsupportedModel)
	require.Equal(t, 1, accountRepo.calls)
	require.Empty(t, modeRepo.modeGroupEnsureCalls)
}

func (r *accountShareModeRepoStub) BeginListingEdit(_ context.Context, _ int64, actorIsAdmin bool, _ int64, input BeginAccountShareListingEditInput) (*AccountShareListing, error) {
	r.beginActorIsAdmin = actorIsAdmin
	r.beginInput = input
	if r.beginErr != nil {
		return nil, r.beginErr
	}
	if r.beginListing != nil {
		return r.beginListing, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) ReleaseListingEdit(context.Context, int64, bool, int64, string) (*AccountShareListing, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) UpdateListing(_ context.Context, _ int64, actorIsAdmin bool, _ int64, input UpdateAccountShareListingInput) (*AccountShareListing, error) {
	r.updateAdmin = actorIsAdmin
	r.updateCalls++
	r.updateInput = input
	if r.updateListing != nil {
		return r.updateListing, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) EnsureListingRevisionTerms(_ context.Context, listingID int64) (*AccountShareListingTermsSnapshot, error) {
	if r.revisionTermsErr != nil {
		return nil, r.revisionTermsErr
	}
	if r.revisionTerms != nil {
		terms := *r.revisionTerms
		terms.AllowedModels = append([]string(nil), r.revisionTerms.AllowedModels...)
		if r.listing != nil && r.listing.ID == listingID {
			revisionID := terms.ListingRevisionID
			r.listing.CurrentRevisionID = &revisionID
			r.listing.RowVersion = terms.RowVersion
		}
		return &terms, nil
	}
	if r.listing == nil || r.listing.ID != listingID || r.listing.CurrentRevisionID == nil {
		return nil, ErrAccountShareListingNotFound
	}
	terms := accountShareJoinTermsFromListing(r.listing, *r.listing.CurrentRevisionID)
	return &terms, nil
}

func (r *accountShareModeRepoStub) JoinListing(_ context.Context, input AccountShareJoinRepositoryInput) (*AccountShareMembership, error) {
	r.joinInput = input
	if r.joinErr != nil {
		return nil, r.joinErr
	}
	if r.joinMembership != nil {
		membership := *r.joinMembership
		return &membership, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) GetMembershipForEnd(_ context.Context, consumerUserID int64, membershipID int64) (*AccountShareMembership, error) {
	if r.endSnapshot != nil {
		snapshot := *r.endSnapshot
		return &snapshot, nil
	}
	if r.endMembership != nil {
		snapshot := *r.endMembership
		if snapshot.ConsumerUserID == 0 {
			snapshot.ConsumerUserID = consumerUserID
		}
		if snapshot.ID == 0 {
			snapshot.ID = membershipID
		}
		if snapshot.Status == "" {
			snapshot.Status = AccountShareMembershipStatusQueued
		}
		if snapshot.UpdatedAt.IsZero() {
			snapshot.UpdatedAt = time.Now().UTC()
		}
		return &snapshot, nil
	}
	return nil, ErrAccountShareMembershipNotFound
}

func (r *accountShareModeRepoStub) BeginMembershipEnd(_ context.Context, input BeginAccountShareMembershipEndInput) (*AccountShareMembership, *AccountShareSeatBillingResult, error) {
	r.endCalls++
	r.endInput = input
	if r.endErr != nil {
		return nil, nil, r.endErr
	}
	if r.endMembership != nil {
		membership := *r.endMembership
		if membership.Status == AccountShareMembershipStatusEnding && membership.EndingOperationID == "" {
			membership.EndingOperationID = input.OperationID
		}
		return &membership, r.endBilling, nil
	}
	return nil, nil, ErrAccountShareMembershipNotFound
}

func (r *accountShareModeRepoStub) FinalizeMembershipEnd(_ context.Context, membershipID int64, operationID string) (*AccountShareMembership, *AccountShareSeatBillingResult, bool, error) {
	r.finalizeCalls++
	r.finalizeOperationID = operationID
	if r.finalizeErr != nil {
		return nil, nil, false, r.finalizeErr
	}
	if r.finalizeMembership != nil {
		membership := *r.finalizeMembership
		if membership.EndingOperationID == "" {
			membership.EndingOperationID = operationID
		}
		return &membership, r.finalizeBilling, r.finalizeDone, nil
	}
	return &AccountShareMembership{
		ID:                membershipID,
		Status:            AccountShareMembershipStatusEnding,
		EndingOperationID: operationID,
	}, nil, false, nil
}

func (r *accountShareModeRepoStub) ListEndingMembershipCandidates(context.Context, int) ([]AccountShareEndingMembershipCandidate, error) {
	return append([]AccountShareEndingMembershipCandidate(nil), r.endingCandidates...), r.endingCandidatesErr
}

func (r *accountShareModeRepoStub) UpdateMembershipIdleTimeout(context.Context, int64, int64, int) (*AccountShareMembership, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) SubmitReview(_ context.Context, _ int64, _ int64, input SubmitAccountShareReviewInput) (*AccountShareReview, error) {
	r.submitReviewCalls++
	r.submitReviewInput = input
	if r.submitReviewErr != nil {
		return nil, r.submitReviewErr
	}
	if r.submitReview != nil {
		return r.submitReview, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) ListListingReviews(context.Context, int64, bool, int64, pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *accountShareModeRepoStub) ListOwnerReviews(context.Context, int64, int64, pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error) {
	return nil, nil, nil
}

func (r *accountShareModeRepoStub) ClaimPendingReviewModerations(context.Context, time.Time, int) ([]AccountShareReview, error) {
	return nil, nil
}

func (r *accountShareModeRepoStub) CompleteReviewModeration(context.Context, int64, AccountShareReviewModerationResult) error {
	return nil
}

func (r *accountShareModeRepoStub) FailReviewModeration(context.Context, int64, string, time.Time, int) error {
	return nil
}

func (r *accountShareModeRepoStub) ListMembershipQueue(context.Context, int64, int64) ([]AccountShareMembership, error) {
	return nil, nil
}

func (r *accountShareModeRepoStub) ReorderMembershipQueue(context.Context, int64, int64, []int64) ([]AccountShareMembership, error) {
	return nil, ErrAccountShareQueueInvalid
}

func (r *accountShareModeRepoStub) TouchMembershipLastRequest(_ context.Context, _ int64, at time.Time) error {
	r.touchCalls++
	r.touchTimes = append(r.touchTimes, at)
	if r.touchErr != nil {
		return r.touchErr
	}
	if r.touchSignal != nil {
		select {
		case r.touchSignal <- at:
		default:
		}
	}
	return nil
}

func (r *accountShareModeRepoStub) ListIdleMembershipCandidates(context.Context, time.Time, AccountShareIdleMembershipFilter, int) ([]AccountShareIdleMembershipCandidate, error) {
	return nil, nil
}

func (r *accountShareModeRepoStub) EndIdleMembership(context.Context, int64, time.Time) (*AccountShareMembership, *AccountShareSeatBillingResult, error) {
	if r.endMembership != nil {
		return r.endMembership, accountShareModeStubBillingResult(r.endMembership), nil
	}
	return nil, nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) ProcessUnavailableMemberships(context.Context, time.Time, int) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareModeRepoStub) ListRecoverableUnavailableMembershipIDs(context.Context, time.Time, int) ([]int64, error) {
	return append([]int64(nil), r.recoverableIDs...), nil
}

func (r *accountShareModeRepoStub) SuspendRecoverableUnavailableMembership(context.Context, int64, time.Time) (*AccountShareMembership, *AccountShareSeatBillingResult, error) {
	r.recoverableCalls++
	return r.recoverableSuspend, accountShareModeStubBillingResult(r.recoverableSuspend), nil
}

func (r *accountShareModeRepoStub) DisablePermanentlyUnavailableListings(context.Context, time.Time, int) (*AccountShareListingMaintenanceResult, error) {
	return &AccountShareListingMaintenanceResult{}, nil
}

func (r *accountShareModeRepoStub) EndUnavailableAccountMemberships(context.Context, int64, time.Time, int) (*AccountShareSeatBillingResult, error) {
	r.unavailableCalls++
	return &AccountShareSeatBillingResult{EndedConsumerUserIDs: []int64{20}}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatBilling(context.Context, time.Time, int) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatWaiverCompensations(_ context.Context, _ time.Time, limit int) (*AccountShareSeatBillingResult, error) {
	r.waiverCompCalls++
	r.waiverCompLimit = limit
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatBillingForJoin(context.Context, time.Time, int64, int64, int64) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatBillingForRequest(context.Context, time.Time, int64, int64) (*AccountShareSeatBillingResult, error) {
	r.requestBillingCalls++
	if r.requestBillingErr != nil {
		return nil, r.requestBillingErr
	}
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareModeRepoStub) GetActiveMembershipForAPIKey(context.Context, int64) (*AccountShareMembership, *AccountShareListing, error) {
	return nil, nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) GetActiveMembershipForRequest(context.Context, int64, int64, int64) (*AccountShareMembership, *AccountShareListing, error) {
	r.bindingCalls++
	if len(r.bindingResults) > 0 {
		result := r.bindingResults[0]
		r.bindingResults = r.bindingResults[1:]
		return result.membership, result.listing, result.err
	}
	if r.membership == nil || r.listing == nil {
		return nil, nil, ErrAccountShareListingNotFound
	}
	return r.membership, r.listing, nil
}

func (r *accountShareModeRepoStub) ActivateNextQueuedMembershipForRequest(context.Context, int64, int64, int64, int, time.Time) (*AccountShareMembership, *AccountShareListing, error) {
	r.activationCalls++
	if len(r.bindingResults) > 0 {
		result := r.bindingResults[0]
		r.bindingResults = r.bindingResults[1:]
		return result.membership, result.listing, result.err
	}
	return nil, nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) SuspendMembershipForDispatchFailure(context.Context, int64, time.Time, time.Time) (*AccountShareMembership, *AccountShareSeatBillingResult, error) {
	r.dispatchFailureCalls++
	r.unavailableCalls++
	membership := r.membership
	if membership == nil {
		membership = &AccountShareMembership{ID: 11, ConsumerUserID: 20, APIKeyID: 30}
	}
	r.membership = nil
	r.listing = nil
	return membership, accountShareModeStubBillingResult(membership), nil
}

func accountShareModeStubBillingResult(membership *AccountShareMembership) *AccountShareSeatBillingResult {
	result := &AccountShareSeatBillingResult{}
	if membership == nil {
		return result
	}
	if membership.ConsumerUserID > 0 {
		result.DebitUserIDs = []int64{membership.ConsumerUserID}
		result.EndedConsumerUserIDs = []int64{membership.ConsumerUserID}
	}
	if membership.OwnerUserID > 0 {
		result.CreditUserIDs = []int64{membership.OwnerUserID}
	}
	return result
}

func (r *accountShareModeRepoStub) ResolvePolicy(context.Context) (*AccountSharePolicy, error) {
	if r.policyErr != nil {
		return nil, r.policyErr
	}
	if r.policy == nil {
		return nil, nil
	}
	policy := *r.policy
	return &policy, nil
}

func TestAccountShareModeProcessSeatBillingDoesNotRunWaiverCompensation(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	svc.processSeatBillingOnce()

	if repo.waiverCompCalls != 0 {
		t.Fatalf("expected no waiver compensation pass from seat billing, got %d", repo.waiverCompCalls)
	}
}

func TestAccountShareModeSeatBillingDrainsGuardedBatchesAndContinuesLifecycleLeased(t *testing.T) {
	baseRepo := &accountShareModeRepoStub{}
	repo := &accountShareBillingLifecycleRepoStub{AccountShareModeRepository: baseRepo}
	billingErr := errors.New("claim durable billing intents")
	guardRepo := &accountShareBillingGuardRepoStub{}
	guardChecksAtClaim := make([]int, 0, 2)
	intentRepo := &accountShareBillingWorkerIntentRepoStub{
		claimBatches: [][]AccountShareBillingIntentWorkItem{
			accountShareBillingWorkerTestBatch(t, "worker-a", 1, 100),
		},
		claimErrors: []error{nil, billingErr},
		onClaim: func(_ int, _ ClaimAccountShareBillingIntentsInput) {
			guardChecksAtClaim = append(guardChecksAtClaim, guardRepo.renewCalls)
		},
	}
	worker := newAccountShareBillingWorkerForTest(
		t,
		intentRepo,
		&accountShareBillingWorkerUsageRepoStub{},
		"worker-a",
	)
	worker.config.BatchSize = 1
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetLifecycleContractEnabled(true)
	svc.SetBillingIntentWorker(worker)
	executor := &ClusterTaskExecutor{
		repo:          guardRepo,
		nodeState:     &ClusterNodeState{},
		clusterMode:   true,
		deploymentID:  "pixel-test",
		nodeID:        "node-a",
		bootID:        "boot-a",
		leaseDuration: time.Minute,
		renewInterval: time.Second,
	}
	guard := &ClusterLeaseGuard{
		executor:     executor,
		taskName:     accountShareSeatBillingTaskName,
		fencingToken: 1,
	}

	err := svc.processSeatBillingOnceLeased(context.Background(), guard)

	require.ErrorIs(t, err, billingErr)
	require.Equal(t, 2, intentRepo.claimCalls)
	require.Len(t, guardChecksAtClaim, 2)
	require.Greater(t, guardChecksAtClaim[0], 0)
	require.Equal(t, guardChecksAtClaim[0]+1, guardChecksAtClaim[1])
	require.Equal(t, 1, repo.endingCalls)
	require.Equal(t, 1, repo.lifecycleCalls)
}

func TestAccountShareModeRecoverableUnavailableSkipsMembershipWithActiveConcurrency(t *testing.T) {
	repo := &accountShareModeRepoStub{
		recoverableIDs:     []int64{11},
		recoverableSuspend: &AccountShareMembership{ID: 11, OwnerUserID: 7, ConsumerUserID: 9},
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 1}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}

	result, err := svc.processRecoverableUnavailableMemberships(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("processRecoverableUnavailableMemberships failed: %v", err)
	}
	if result == nil || result.Processed != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if repo.recoverableCalls != 0 {
		t.Fatalf("active long-running request must prevent suspension, calls=%d", repo.recoverableCalls)
	}
}

func TestAccountShareModeRecoverableUnavailableSuspendsAfterConcurrencyDrains(t *testing.T) {
	repo := &accountShareModeRepoStub{
		recoverableIDs:     []int64{11},
		recoverableSuspend: &AccountShareMembership{ID: 11, OwnerUserID: 7, ConsumerUserID: 9},
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 0}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}

	result, err := svc.processRecoverableUnavailableMemberships(context.Background(), time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("processRecoverableUnavailableMemberships failed: %v", err)
	}
	if repo.recoverableCalls != 1 {
		t.Fatalf("expected one suspension after concurrency drained, calls=%d", repo.recoverableCalls)
	}
	if result == nil || len(result.DebitUserIDs) != 1 || result.DebitUserIDs[0] != 9 ||
		len(result.CreditUserIDs) != 1 || result.CreditUserIDs[0] != 7 ||
		len(result.EndedConsumerUserIDs) != 1 || result.EndedConsumerUserIDs[0] != 9 {
		t.Fatalf("unexpected cache invalidation result: %#v", result)
	}
}

func TestAccountShareModeProcessSeatWaiverCompensationsUsesDedicatedBatchSize(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.taskExecutor = &ClusterTaskExecutor{}

	svc.processSeatWaiverCompensationsOnce()

	if repo.waiverCompCalls != 1 {
		t.Fatalf("expected one waiver compensation pass, got %d", repo.waiverCompCalls)
	}
	if repo.waiverCompLimit != AccountShareModeSeatWaiverCompensationBatchSize {
		t.Fatalf("waiver compensation limit = %d, want %d", repo.waiverCompLimit, AccountShareModeSeatWaiverCompensationBatchSize)
	}
}

func TestAccountShareModeSeatWaiverCompensationRequiresClusterLease(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	clusterRepo := &clusterAdminRepositoryStub{}
	cfg := testClusterRuntimeConfig()
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.taskExecutor = NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg))

	svc.processSeatWaiverCompensationsOnce()

	require.Equal(t, accountShareSeatWaiverCompensationTaskName, clusterRepo.acquiredTaskName)
	require.Zero(t, repo.waiverCompCalls)
}

func TestAccountShareModeReviewModerationRequiresClusterLease(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	clusterRepo := &clusterAdminRepositoryStub{}
	cfg := testClusterRuntimeConfig()
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetReviewModerationSettingRepository(&accountShareReviewSettingRepoStub{})
	svc.taskExecutor = NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg))

	svc.processReviewModerationOnce()

	require.Equal(t, accountShareReviewModerationTaskName, clusterRepo.acquiredTaskName)
}

func TestAccountShareModeListModeGroupsUsesReadOnlyLookup(t *testing.T) {
	repo := &accountShareModeRepoStub{modeGroups: map[string]*Group{
		PlatformOpenAI:    {ID: 101, Platform: PlatformOpenAI},
		PlatformAnthropic: {ID: 202, Platform: PlatformAnthropic},
	}}
	svc := &AccountShareModeService{repo: repo}

	groups, err := svc.ListModeGroups(context.Background())
	if err != nil {
		t.Fatalf("list mode groups failed: %v", err)
	}
	if len(groups) != 2 || groups[0].GroupID != 101 || groups[0].Platform != PlatformOpenAI || groups[1].GroupID != 202 || groups[1].Platform != PlatformAnthropic {
		t.Fatalf("unexpected mode groups: %#v", groups)
	}
	if len(repo.modeGroupGetCalls) != 2 || repo.modeGroupGetCalls[0] != PlatformOpenAI || repo.modeGroupGetCalls[1] != PlatformAnthropic {
		t.Fatalf("unexpected read-only lookup calls: %#v", repo.modeGroupGetCalls)
	}
	if len(repo.modeGroupEnsureCalls) != 0 {
		t.Fatalf("mode group listing must not ensure/write groups: %#v", repo.modeGroupEnsureCalls)
	}
}

func TestAccountShareModeListModeGroupsFailsWhenMappingMissing(t *testing.T) {
	repo := &accountShareModeRepoStub{modeGroups: map[string]*Group{
		PlatformOpenAI: {ID: 101, Platform: PlatformOpenAI},
	}}
	svc := &AccountShareModeService{repo: repo}

	groups, err := svc.ListModeGroups(context.Background())
	if !errors.Is(err, ErrAccountShareModeGroupUnavailable) {
		t.Fatalf("expected missing mode group error, got groups=%#v err=%v", groups, err)
	}
	if len(repo.modeGroupEnsureCalls) != 0 {
		t.Fatalf("missing mapping must not trigger ensure/write: %#v", repo.modeGroupEnsureCalls)
	}
}

func TestAccountShareModeExchangePreflightsDuplicateNameBeforeOAuth(t *testing.T) {
	repo := &accountShareModeRepoStub{ensureNameErr: ErrAccountShareModeDuplicateName}
	svc := &AccountShareModeService{repo: repo, proxyRepo: &accountShareModeProxyRepoStub{}}

	_, err := svc.ExchangeOpenAICodeAndCreateListing(context.Background(), 10, &OpenAIExchangeCodeInput{
		SessionID: "session",
		Code:      "code",
		State:     "state",
		ProxyID:   accountShareModeInt64Ptr(7),
	}, CreateAccountShareListingInput{
		Name:                "OpenAI共享账号",
		ProxyID:             7,
		Concurrency:         AccountShareModeDefaultAccountConcurrency,
		SeatLimit:           AccountShareModeMinSeats,
		RateMultiplier:      1,
		AllowedModels:       []string{"gpt-5"},
		PerUserConcurrency:  AccountShareModeDefaultPerUserConcurrency,
		HourlyRate:          0.2,
		Codex5hLimitPercent: AccountShareModeDefaultCodexLimitPercent,
		Codex7dLimitPercent: AccountShareModeDefaultCodexLimitPercent,
	})
	if !errors.Is(err, ErrAccountShareModeDuplicateName) {
		t.Fatalf("expected duplicate name error before OAuth exchange, got %v", err)
	}
}

func TestAccountShareModeExchangeRejectsFullProxyBeforeOAuth(t *testing.T) {
	proxyRepo := &accountShareModeProxyRepoStub{
		proxy: &Proxy{
			ID:          7,
			Name:        "full-proxy",
			Protocol:    "socks5",
			Host:        "127.0.0.1",
			Port:        1080,
			Status:      StatusActive,
			MaxAccounts: 5,
		},
		accountCount: 5,
	}
	svc := &AccountShareModeService{repo: &accountShareModeRepoStub{}, proxyRepo: proxyRepo}

	_, err := svc.ExchangeOpenAICodeAndCreateListing(context.Background(), 10, &OpenAIExchangeCodeInput{
		SessionID: "session",
		Code:      "code",
		State:     "state",
		ProxyID:   accountShareModeInt64Ptr(7),
	}, CreateAccountShareListingInput{
		Name:                "OpenAI共享账号",
		ProxyID:             7,
		Concurrency:         AccountShareModeDefaultAccountConcurrency,
		SeatLimit:           AccountShareModeMinSeats,
		RateMultiplier:      1,
		AllowedModels:       []string{"gpt-5"},
		PerUserConcurrency:  AccountShareModeDefaultPerUserConcurrency,
		HourlyRate:          0.2,
		Codex5hLimitPercent: AccountShareModeDefaultCodexLimitPercent,
		Codex7dLimitPercent: AccountShareModeDefaultCodexLimitPercent,
	})
	if infraerrors.Reason(err) != "PROXY_ACCOUNT_LIMIT_EXCEEDED" {
		t.Fatalf("expected proxy capacity error before OAuth exchange, got %v", err)
	}
	if proxyRepo.countCalls != 1 {
		t.Fatalf("expected one proxy account count check, got %d", proxyRepo.countCalls)
	}
}

func TestAccountShareModeCreateOpenAIListingStartsValidating(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	proxyRepo := &accountShareModeProxyRepoStub{
		proxy: &Proxy{
			ID:       7,
			Name:     "proxy",
			Protocol: "socks5",
			Host:     "127.0.0.1",
			Port:     1080,
			Status:   StatusActive,
		},
	}
	service := &AccountShareModeService{
		repo:                     repo,
		proxyRepo:                proxyRepo,
		openaiOAuthService:       &OpenAIOAuthService{},
		lifecycleContractEnabled: true,
	}

	created, err := service.CreateOpenAIListingFromToken(
		context.Background(),
		42,
		CreateAccountShareListingInput{
			Name:               "OpenAI共享账号",
			ProxyID:            7,
			Concurrency:        2,
			SeatLimit:          2,
			RateMultiplier:     1,
			AllowedModels:      []string{"gpt-5"},
			PerUserConcurrency: 1,
			HourlyRate:         0.2,
			TokenInfo: &OpenAITokenInfo{
				AccessToken:  "openai-access-token",
				RefreshToken: "openai-refresh-token",
				ExpiresAt:    time.Now().Add(time.Hour).Unix(),
				PlanType:     "plus",
			},
		},
	)

	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, AccountShareListingStatusValidating, created.Status)
	require.NotNil(t, repo.createdListing)
	require.Equal(t, AccountShareListingStatusValidating, repo.createdListing.Status)
}

func TestAccountShareModeCreateAnthropicListingDefaultsQuotaLimitPercents(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	proxyRepo := &accountShareModeProxyRepoStub{
		proxy: &Proxy{ID: 7, Name: "proxy", Protocol: "socks5", Host: "127.0.0.1", Port: 1080, Status: StatusActive},
	}
	svc := &AccountShareModeService{
		repo:                     repo,
		proxyRepo:                proxyRepo,
		oauthService:             &OAuthService{},
		lifecycleContractEnabled: true,
	}

	got, err := svc.CreateAnthropicListingFromToken(context.Background(), 42, CreateAccountShareListingInput{
		Name:               "Claude共享账号",
		ProxyID:            7,
		Concurrency:        2,
		SeatLimit:          2,
		RateMultiplier:     1,
		AllowedModels:      []string{"claude-opus-4-7"},
		PerUserConcurrency: 1,
		HourlyRate:         0.2,
		AnthropicTokenInfo: &TokenInfo{
			AccessToken:  "sk-ant-oat01-access",
			RefreshToken: "sk-ant-ort01-refresh",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			ExpiresAt:    time.Now().Add(time.Hour).Unix(),
		},
	})
	if err != nil {
		t.Fatalf("CreateAnthropicListingFromToken failed: %v", err)
	}
	if got.Codex5hLimitPercent != AccountShareModeDefaultCodexLimitPercent || got.Codex7dLimitPercent != AccountShareModeDefaultCodexLimitPercent {
		t.Fatalf("expected returned default codex limits, got 5h=%v 7d=%v", got.Codex5hLimitPercent, got.Codex7dLimitPercent)
	}
	if got.Anthropic5hLimitPercent != AnthropicQuotaDefaultLimitPercent || got.Anthropic7dLimitPercent != AnthropicQuotaDefaultLimitPercent {
		t.Fatalf("expected returned default anthropic limits, got 5h=%v 7d=%v", got.Anthropic5hLimitPercent, got.Anthropic7dLimitPercent)
	}
	if repo.createdListing == nil {
		t.Fatal("expected listing to be created")
	}
	require.Equal(t, AccountShareListingStatusValidating, got.Status)
	require.Equal(t, AccountShareListingStatusValidating, repo.createdListing.Status)
	if repo.createdListing.Codex5hLimitPercent != AccountShareModeDefaultCodexLimitPercent || repo.createdListing.Codex7dLimitPercent != AccountShareModeDefaultCodexLimitPercent {
		t.Fatalf("expected persisted default codex limits, got 5h=%v 7d=%v", repo.createdListing.Codex5hLimitPercent, repo.createdListing.Codex7dLimitPercent)
	}
	if repo.createdListing.Anthropic5hLimitPercent != AnthropicQuotaDefaultLimitPercent || repo.createdListing.Anthropic7dLimitPercent != AnthropicQuotaDefaultLimitPercent {
		t.Fatalf("expected persisted default anthropic limits, got 5h=%v 7d=%v", repo.createdListing.Anthropic5hLimitPercent, repo.createdListing.Anthropic7dLimitPercent)
	}
	if repo.createdAccount == nil {
		t.Fatal("expected account to be created")
	}
	if got := repo.createdAccount.Extra["anthropic_5h_limit_percent"]; got != AnthropicQuotaDefaultLimitPercent {
		t.Fatalf("expected account 5h anthropic limit extra, got %v", got)
	}
	if got := repo.createdAccount.Extra["anthropic_7d_limit_percent"]; got != AnthropicQuotaDefaultLimitPercent {
		t.Fatalf("expected account 7d anthropic limit extra, got %v", got)
	}
}

func TestAccountShareModeCreateUserProxyAssignsCurrentOwner(t *testing.T) {
	proxyRepo := &accountShareModeProxyRepoStub{}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	got, err := svc.CreateUserProxy(context.Background(), 42, CreateAccountShareProxyInput{
		Name:     " 我的代理 ",
		Protocol: " SOCKS5 ",
		Host:     " 192.168.0.1 ",
		Port:     8000,
		Username: " user ",
		Password: " pass ",
	})
	if err != nil {
		t.Fatalf("CreateUserProxy failed: %v", err)
	}
	if got.OwnerUserID == nil || *got.OwnerUserID != 42 {
		t.Fatalf("expected owner_user_id=42, got %#v", got.OwnerUserID)
	}
	if got.Name != "我的代理" {
		t.Fatalf("expected trimmed proxy name, got %q", got.Name)
	}
	if got.Protocol != "socks5" || got.Host != "192.168.0.1" || got.Username != "user" || got.Password != "pass" {
		t.Fatalf("proxy normalization mismatch: %#v", got)
	}
}

func TestAccountShareModeCreateUserProxyDoesNotAdoptPlatformProxy(t *testing.T) {
	ownerID := int64(42)
	proxyRepo := &accountShareModeProxyRepoStub{proxy: &Proxy{
		ID: 7, Name: "platform", Protocol: "http", Host: "proxy.example.com", Port: 8080,
		Status: StatusActive,
	}}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	created, err := svc.CreateUserProxy(context.Background(), ownerID, CreateAccountShareProxyInput{
		Name: "mine", Protocol: "http", Host: "proxy.example.com", Port: 8080,
	})
	if err != nil {
		t.Fatalf("CreateUserProxy failed: %v", err)
	}
	if proxyRepo.createCalls != 1 {
		t.Fatalf("expected a user-owned proxy to be created, got %d create calls", proxyRepo.createCalls)
	}
	if created.OwnerUserID == nil || *created.OwnerUserID != ownerID {
		t.Fatalf("expected owner %d, got %#v", ownerID, created.OwnerUserID)
	}
}

func TestAccountShareModeUpdateUserProxyUpdatesOwnedProxyAndPreservesProtectedFields(t *testing.T) {
	ownerID := int64(42)
	createdAt := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	proxyRepo := &accountShareModeProxyRepoStub{proxy: &Proxy{
		ID: 7, Name: "旧名称", Protocol: "http", Host: "old.example.com", Port: 8080,
		Username: "old-user", Password: "old-pass", OwnerUserID: &ownerID,
		Status: StatusActive, MaxAccounts: 3, CreatedAt: createdAt,
	}}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	password := " new-pass "
	got, err := svc.UpdateUserProxy(context.Background(), ownerID, 7, UpdateAccountShareProxyInput{
		Name: " 新名称 ", Protocol: " SOCKS5 ", Host: " proxy.example.com ", Port: 1080,
		Username: " new-user ", Password: &password,
	})
	if err != nil {
		t.Fatalf("UpdateUserProxy failed: %v", err)
	}
	if proxyRepo.updateCalls != 1 {
		t.Fatalf("expected one update call, got %d", proxyRepo.updateCalls)
	}
	if got.Name != "新名称" || got.Protocol != "socks5" || got.Host != "proxy.example.com" || got.Port != 1080 || got.Username != "new-user" || got.Password != "new-pass" {
		t.Fatalf("unexpected updated proxy: %#v", got)
	}
	if got.ID != 7 || got.OwnerUserID == nil || *got.OwnerUserID != ownerID || got.Status != StatusActive || got.MaxAccounts != 3 || !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("protected fields changed: %#v", got)
	}
}

func TestAccountShareModeUpdateUserProxyKeepsPasswordWhenOmitted(t *testing.T) {
	ownerID := int64(42)
	proxyRepo := &accountShareModeProxyRepoStub{proxy: &Proxy{
		ID: 7, Name: "proxy", Protocol: "http", Host: "old.example.com", Port: 8080,
		Password: "secret", OwnerUserID: &ownerID, Status: StatusActive,
	}}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	got, err := svc.UpdateUserProxy(context.Background(), ownerID, 7, UpdateAccountShareProxyInput{
		Name: "proxy", Protocol: "http", Host: "new.example.com", Port: 8081,
	})
	if err != nil {
		t.Fatalf("UpdateUserProxy failed: %v", err)
	}
	if got.Password != "secret" {
		t.Fatalf("expected password to be preserved, got %q", got.Password)
	}
}

func TestAccountShareModeUpdateUserProxyClearsPasswordWhenExplicitlyEmpty(t *testing.T) {
	ownerID := int64(42)
	emptyPassword := ""
	proxyRepo := &accountShareModeProxyRepoStub{proxy: &Proxy{
		ID: 7, Name: "proxy", Protocol: "http", Host: "old.example.com", Port: 8080,
		Password: "secret", OwnerUserID: &ownerID, Status: StatusActive,
	}}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	got, err := svc.UpdateUserProxy(context.Background(), ownerID, 7, UpdateAccountShareProxyInput{
		Name: "proxy", Protocol: "http", Host: "new.example.com", Port: 8081, Password: &emptyPassword,
	})
	if err != nil {
		t.Fatalf("UpdateUserProxy failed: %v", err)
	}
	if got.Password != "" {
		t.Fatalf("expected password to be cleared, got %q", got.Password)
	}
}

func TestAccountShareModeUpdateUserProxyRejectsUnownedProxy(t *testing.T) {
	ownerID := int64(42)
	otherOwnerID := int64(99)
	tests := []struct {
		name    string
		ownerID *int64
	}{
		{name: "platform proxy", ownerID: nil},
		{name: "other user proxy", ownerID: &otherOwnerID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyRepo := &accountShareModeProxyRepoStub{proxy: &Proxy{
				ID: 7, Protocol: "http", Host: "proxy.example.com", Port: 8080,
				OwnerUserID: tt.ownerID, Status: StatusActive,
			}}
			svc := &AccountShareModeService{proxyRepo: proxyRepo}
			_, err := svc.UpdateUserProxy(context.Background(), ownerID, 7, UpdateAccountShareProxyInput{
				Protocol: "http", Host: "new.example.com", Port: 8081,
			})
			if !errors.Is(err, ErrProxyNotFound) {
				t.Fatalf("expected ErrProxyNotFound, got %v", err)
			}
			if proxyRepo.updateCalls != 0 {
				t.Fatalf("unowned proxy must not be updated, got %d calls", proxyRepo.updateCalls)
			}
		})
	}
}

func TestAccountShareModeDeleteUserProxyRejectsProxyInUse(t *testing.T) {
	ownerID := int64(42)
	proxyRepo := &accountShareModeProxyRepoStub{
		proxy:        &Proxy{ID: 7, OwnerUserID: &ownerID, Status: StatusActive},
		accountCount: 1,
	}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	err := svc.DeleteUserProxy(context.Background(), ownerID, 7)
	if !errors.Is(err, ErrProxyInUse) {
		t.Fatalf("expected ErrProxyInUse, got %v", err)
	}
	if proxyRepo.deleteCalls != 0 {
		t.Fatalf("in-use proxy must not be deleted, got %d calls", proxyRepo.deleteCalls)
	}
}

func TestAccountShareModeDeleteUserProxyRejectsUnownedProxy(t *testing.T) {
	ownerID := int64(42)
	otherOwnerID := int64(99)
	tests := []struct {
		name    string
		ownerID *int64
	}{
		{name: "platform proxy", ownerID: nil},
		{name: "other user proxy", ownerID: &otherOwnerID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyRepo := &accountShareModeProxyRepoStub{
				proxy: &Proxy{ID: 7, OwnerUserID: tt.ownerID, Status: StatusActive},
			}
			svc := &AccountShareModeService{proxyRepo: proxyRepo}

			err := svc.DeleteUserProxy(context.Background(), ownerID, 7)
			if !errors.Is(err, ErrProxyNotFound) {
				t.Fatalf("expected ErrProxyNotFound, got %v", err)
			}
			if proxyRepo.countCalls != 0 || proxyRepo.deleteCalls != 0 {
				t.Fatalf("unowned proxy must not be counted or deleted, count_calls=%d delete_calls=%d", proxyRepo.countCalls, proxyRepo.deleteCalls)
			}
		})
	}
}

func TestAccountShareModeDeleteUserProxyDeletesUnusedOwnedProxy(t *testing.T) {
	ownerID := int64(42)
	proxyRepo := &accountShareModeProxyRepoStub{
		proxy: &Proxy{ID: 7, OwnerUserID: &ownerID, Status: StatusActive},
	}
	svc := &AccountShareModeService{proxyRepo: proxyRepo}

	if err := svc.DeleteUserProxy(context.Background(), ownerID, 7); err != nil {
		t.Fatalf("DeleteUserProxy failed: %v", err)
	}
	if proxyRepo.deleteCalls != 1 || proxyRepo.deletedID != 7 {
		t.Fatalf("expected proxy 7 to be deleted once, calls=%d id=%d", proxyRepo.deleteCalls, proxyRepo.deletedID)
	}
}

func TestAccountShareModeListListingsKeepsMineScopeAndAdminFlag(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}

	_, _, err := svc.ListListings(context.Background(), 42, true, AccountShareListingFilters{
		Tab:       AccountShareModeListingTabMine,
		SeatLimit: AccountShareModeMaxSeats + 1,
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if repo.listFilters.Tab != AccountShareModeListingTabMine {
		t.Fatalf("expected mine tab, got %q", repo.listFilters.Tab)
	}
	if !repo.listFilters.ViewerIsAdmin {
		t.Fatal("expected admin flag to be passed through")
	}
	if repo.listFilters.SeatLimit != 0 {
		t.Fatalf("expected invalid seat limit to normalize to 0, got %d", repo.listFilters.SeatLimit)
	}
}

func TestAccountShareModeListMembershipHistoryForwardsConsumerAndKeepsSegments(t *testing.T) {
	repo := &accountShareHistoryRepoStub{
		entries: []AccountShareMembershipHistoryEntry{
			{MembershipID: 11, ListingID: 7, RoomDeleted: true},
			{MembershipID: 12, ListingID: 7, RoomDeleted: true},
		},
		result: &pagination.PaginationResult{
			Total:    2,
			Page:     2,
			PageSize: 5,
			Pages:    1,
		},
	}
	svc := &AccountShareModeService{repo: repo}
	params := pagination.PaginationParams{Page: 2, PageSize: 5}

	entries, result, err := svc.ListMembershipHistory(context.Background(), 42, params)
	if err != nil {
		t.Fatalf("ListMembershipHistory failed: %v", err)
	}
	if repo.calls != 1 || repo.consumerUserID != 42 || repo.params != params {
		t.Fatalf(
			"unexpected repository call: calls=%d consumer=%d params=%#v",
			repo.calls,
			repo.consumerUserID,
			repo.params,
		)
	}
	if len(entries) != 2 ||
		entries[0].MembershipID != 11 ||
		entries[1].MembershipID != 12 ||
		entries[0].ListingID != entries[1].ListingID {
		t.Fatalf("history segments were not preserved: %#v", entries)
	}
	if result == nil || result.Total != 2 || result.Page != 2 || result.PageSize != 5 {
		t.Fatalf("unexpected pagination: %#v", result)
	}
}

func TestAccountShareModeListListingsNormalizesAccountLevelWithDynamicAliases(t *testing.T) {
	listings := []AccountShareListing{
		{
			ID:              1,
			AccountID:       10,
			Platform:        PlatformOpenAI,
			AccountLevel:    AccountLevelUnknown,
			AccountPlanType: "chatgptstudent",
		},
	}

	normalizeAccountShareListingsAccountLevelWithConfigs(listings, []OpenAIAccountLevelConfig{
		{Key: "student", Label: "Student", Aliases: []string{"chatgptstudent"}, Enabled: true, SortOrder: 10},
	})

	if listings[0].AccountLevel != "student" {
		t.Fatalf("expected dynamic account level student, got %q", listings[0].AccountLevel)
	}
}

func TestNormalizeListingFiltersKeepsNonCodexCLIOnlyForOpenAIOnly(t *testing.T) {
	openAI := normalizeListingFilters(AccountShareListingFilters{
		Platform:    PlatformOpenAI,
		FeatureTags: []string{AccountShareListingFeatureNonCodexCLIOnly},
	})
	if len(openAI.FeatureTags) != 1 || openAI.FeatureTags[0] != AccountShareListingFeatureNonCodexCLIOnly {
		t.Fatalf("expected OpenAI filters to keep non codex client tag, got %#v", openAI.FeatureTags)
	}

	anthropic := normalizeListingFilters(AccountShareListingFilters{
		Platform:    PlatformAnthropic,
		FeatureTags: []string{AccountShareListingFeatureNonCodexCLIOnly},
	})
	if len(anthropic.FeatureTags) != 0 {
		t.Fatalf("expected Anthropic filters to drop non codex client tag, got %#v", anthropic.FeatureTags)
	}
}

func TestAccountShareModeGetMySpendSummaryBuildsTodayRange(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}
	now := time.Date(2026, 6, 26, 15, 30, 0, 0, time.FixedZone("CST", 8*60*60))

	_, err := svc.GetMySpendSummary(context.Background(), 42, AccountShareMySpendInput{
		ListingID: 7,
		Range:     AccountShareSpendRangeToday,
		Timezone:  "Asia/Shanghai",
		Now:       now,
	})
	if err != nil {
		t.Fatalf("GetMySpendSummary failed: %v", err)
	}
	if repo.spendQuery.ListingID != 7 || repo.spendQuery.ConsumerID != 42 {
		t.Fatalf("unexpected query identity: %#v", repo.spendQuery)
	}
	if repo.spendQuery.Range != AccountShareSpendRangeToday {
		t.Fatalf("range = %q, want today", repo.spendQuery.Range)
	}
	wantStart := time.Date(2026, 6, 26, 0, 0, 0, 0, now.Location())
	if !repo.spendQuery.StartTime.Equal(wantStart) {
		t.Fatalf("start time = %s, want %s", repo.spendQuery.StartTime, wantStart)
	}
	if !repo.spendQuery.EndTime.Equal(now) {
		t.Fatalf("end time = %s, want %s", repo.spendQuery.EndTime, now)
	}
}

func TestAccountShareModeGetMySpendSummaryRejectsInvalidRange(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}

	_, err := svc.GetMySpendSummary(context.Background(), 42, AccountShareMySpendInput{
		ListingID: 7,
		Range:     "month",
	})
	if !errors.Is(err, ErrAccountShareSpendInvalidRange) {
		t.Fatalf("expected invalid range error, got %v", err)
	}
}

func TestAccountShareModeRecommendListingsRequiresAPIKey(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{}
	svc := newAccountShareRecommendationTestService(repo, apiKeyRepo)

	_, err := svc.RecommendListings(context.Background(), 42, false, AccountShareRecommendationInput{
		Platform:                  PlatformOpenAI,
		Model:                     "gpt-5.4",
		RequestCount:              1,
		ActiveHours:               1,
		InputTokensPerRequest:     100,
		OutputTokensPerRequest:    50,
		CacheReadTokensPerRequest: 0,
	})
	if !errors.Is(err, ErrAccountShareRecommendationInvalid) {
		t.Fatalf("expected invalid recommendation input, got %v", err)
	}
	if apiKeyRepo.calls != 0 {
		t.Fatalf("expected api key repository not to be called, got %d calls", apiKeyRepo.calls)
	}
}

func TestAccountShareModeRecommendListingsRejectsAPIKeyFromDifferentModeGroup(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 7, UserID: 42, GroupID: accountShareModeInt64Ptr(2)},
	}
	svc := newAccountShareRecommendationTestService(repo, apiKeyRepo)

	_, err := svc.RecommendListings(context.Background(), 42, false, AccountShareRecommendationInput{
		Platform:               PlatformOpenAI,
		Model:                  "gpt-5.4",
		APIKeyID:               7,
		RequestCount:           1,
		ActiveHours:            1,
		InputTokensPerRequest:  100,
		OutputTokensPerRequest: 50,
	})
	if !errors.Is(err, ErrAccountShareAPIKeyMustUseModeGroup) {
		t.Fatalf("expected mode group error, got %v", err)
	}
	if len(repo.listPages) != 0 {
		t.Fatalf("expected listings not to be loaded, got pages %#v", repo.listPages)
	}
}

func TestAccountShareModeRecommendListingsScansAllPagesAndKeepsTopCandidates(t *testing.T) {
	repo := &accountShareModeRepoStub{
		listingsByPage: map[int][]AccountShareListing{
			1: {{
				ID:                 1,
				OwnerUserID:        100,
				Status:             AccountShareListingStatusActive,
				Platform:           PlatformOpenAI,
				AllowedModels:      []string{"gpt-5.4"},
				SeatLimit:          2,
				ActiveSeats:        0,
				RateMultiplier:     8,
				PerUserConcurrency: 1,
				AccountConcurrency: 5,
			}},
			2: {{
				ID:                 2,
				OwnerUserID:        101,
				Status:             AccountShareListingStatusActive,
				Platform:           PlatformOpenAI,
				AllowedModels:      []string{"gpt-5.4"},
				SeatLimit:          2,
				ActiveSeats:        0,
				RateMultiplier:     1,
				PerUserConcurrency: 5,
				AccountConcurrency: 20,
				RatingCount:        3,
				RatingAvg:          9,
			}},
		},
	}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 7, UserID: 42, GroupID: accountShareModeInt64Ptr(1)},
	}
	svc := newAccountShareRecommendationTestService(repo, apiKeyRepo)

	got, err := svc.RecommendListings(context.Background(), 42, false, AccountShareRecommendationInput{
		Platform:               PlatformOpenAI,
		Model:                  "gpt-5.4",
		APIKeyID:               7,
		RequestCount:           100,
		ActiveHours:            2,
		InputTokensPerRequest:  1000,
		OutputTokensPerRequest: 500,
		Limit:                  1,
	})
	if err != nil {
		t.Fatalf("RecommendListings failed: %v", err)
	}
	if got.CandidateCount != 2 {
		t.Fatalf("expected both pages to be evaluated, got candidate_count=%d", got.CandidateCount)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected top 1 candidate, got %d", len(got.Items))
	}
	if got.Items[0].Listing.ID != 2 {
		t.Fatalf("expected second page listing to win, got listing %d", got.Items[0].Listing.ID)
	}
	if got.Recommended == nil || got.Recommended.Listing.ID != 2 {
		t.Fatalf("expected recommended listing 2, got %#v", got.Recommended)
	}
	if len(repo.listPages) != 2 || repo.listPages[0] != 1 || repo.listPages[1] != 2 {
		t.Fatalf("expected pages 1 and 2 to be loaded, got %#v", repo.listPages)
	}
	if !repo.listFilters.SkipTotal {
		t.Fatal("expected recommendation listing query to skip total count")
	}
	if len(repo.listParams) == 0 || repo.listParams[0].PageSize != AccountShareRecommendationPageSize {
		t.Fatalf("expected recommendation page size %d, got %#v", AccountShareRecommendationPageSize, repo.listParams)
	}
}

func TestAccountShareModeRecommendListingsRanksByEstimatedCostBeforeQuality(t *testing.T) {
	repo := &accountShareModeRepoStub{
		listingsByPage: map[int][]AccountShareListing{
			1: {
				{
					ID:                 1,
					AccountID:          101,
					OwnerUserID:        100,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          12,
					ActiveSeats:        0,
					RateMultiplier:     4,
					HourlyRate:         0,
					PerUserConcurrency: 20,
					AccountConcurrency: 50,
					RatingCount:        20,
					RatingAvg:          10,
				},
				{
					ID:                 2,
					AccountID:          102,
					OwnerUserID:        101,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          2,
					ActiveSeats:        1,
					RateMultiplier:     1,
					HourlyRate:         0,
					PerUserConcurrency: 1,
					AccountConcurrency: 2,
					RatingCount:        0,
					RatingAvg:          0,
				},
			},
		},
	}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 7, UserID: 42, GroupID: accountShareModeInt64Ptr(1)},
	}
	svc := newAccountShareRecommendationTestService(repo, apiKeyRepo)

	got, err := svc.RecommendListings(context.Background(), 42, false, AccountShareRecommendationInput{
		Platform:               PlatformOpenAI,
		Model:                  "gpt-5.4",
		APIKeyID:               7,
		RequestCount:           100,
		ActiveHours:            2,
		InputTokensPerRequest:  1000,
		OutputTokensPerRequest: 500,
		Limit:                  2,
	})
	if err != nil {
		t.Fatalf("RecommendListings failed: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected two candidates, got %d", len(got.Items))
	}
	if got.Items[0].Listing.ID != 2 {
		t.Fatalf("expected lower estimated cost listing to rank first, got listing %d", got.Items[0].Listing.ID)
	}
	if got.Items[0].Estimate.TotalCost >= got.Items[1].Estimate.TotalCost {
		t.Fatalf("expected first candidate to be cheaper: first=%f second=%f", got.Items[0].Estimate.TotalCost, got.Items[1].Estimate.TotalCost)
	}
	if got.Items[0].ScoreBreakdown.CostSavingScore <= 0 {
		t.Fatalf("expected score breakdown to include cost saving score, got %#v", got.Items[0].ScoreBreakdown)
	}
	if got.Items[0].ScoreBreakdown.OverallScore != got.Items[0].Score {
		t.Fatalf("expected candidate score to mirror overall score, score=%f breakdown=%#v", got.Items[0].Score, got.Items[0].ScoreBreakdown)
	}
	if !accountShareTestContainsString(got.Items[0].Tags, "最省额度") {
		t.Fatalf("expected cheapest candidate to receive cost-saving tag, got %#v", got.Items[0].Tags)
	}
	if got.Recommended == nil || got.Recommended.Listing.ID != 2 {
		t.Fatalf("expected recommended listing 2, got %#v", got.Recommended)
	}
}

func TestAccountShareModeRecommendListingsAddsSmartLabels(t *testing.T) {
	repo := &accountShareModeRepoStub{
		listingsByPage: map[int][]AccountShareListing{
			1: {
				{
					ID:                 1,
					AccountID:          101,
					OwnerUserID:        100,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          2,
					ActiveSeats:        1,
					RateMultiplier:     1,
					HourlyRate:         0,
					PerUserConcurrency: 1,
					AccountConcurrency: 2,
				},
				{
					ID:                 2,
					AccountID:          102,
					OwnerUserID:        101,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          12,
					ActiveSeats:        0,
					RateMultiplier:     1,
					HourlyRate:         0.01,
					PerUserConcurrency: 12,
					AccountConcurrency: 30,
					RatingCount:        20,
					RatingAvg:          9.8,
				},
			},
		},
	}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 7, UserID: 42, GroupID: accountShareModeInt64Ptr(1)},
	}
	svc := newAccountShareRecommendationTestService(repo, apiKeyRepo)

	got, err := svc.RecommendListings(context.Background(), 42, false, AccountShareRecommendationInput{
		Platform:               PlatformOpenAI,
		Model:                  "gpt-5.4",
		APIKeyID:               7,
		RequestCount:           100,
		ActiveHours:            2,
		InputTokensPerRequest:  1000,
		OutputTokensPerRequest: 500,
		Limit:                  2,
	})
	if err != nil {
		t.Fatalf("RecommendListings failed: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("expected two candidates, got %d", len(got.Items))
	}
	if got.Items[0].Listing.ID != 1 {
		t.Fatalf("expected cheapest listing to remain first, got listing %d", got.Items[0].Listing.ID)
	}
	if !accountShareTestContainsString(got.Items[0].Tags, "最省额度") {
		t.Fatalf("expected cheapest listing to receive cost-saving tag, got %#v", got.Items[0].Tags)
	}
	var stableCandidate *AccountShareRecommendationCandidate
	for i := range got.Items {
		if got.Items[i].Listing.ID == 2 {
			stableCandidate = &got.Items[i]
			break
		}
	}
	if stableCandidate == nil {
		t.Fatal("expected stable candidate to be returned")
	}
	if !accountShareTestContainsString(stableCandidate.Tags, "最稳妥") {
		t.Fatalf("expected stable candidate to receive stability tag, got %#v", stableCandidate.Tags)
	}
	if !accountShareTestContainsString(stableCandidate.Tags, "性价比最高") {
		t.Fatalf("expected best overall candidate to receive value tag, got %#v", stableCandidate.Tags)
	}
	if stableCandidate.ScoreBreakdown.OverallScore <= got.Items[0].ScoreBreakdown.OverallScore {
		t.Fatalf("expected stable candidate to have higher overall score: stable=%#v cheapest=%#v", stableCandidate.ScoreBreakdown, got.Items[0].ScoreBreakdown)
	}
}

func TestAccountShareRecommendationSelectCandidatesKeepsQualityOutliersWithinLimit(t *testing.T) {
	candidates := []AccountShareRecommendationCandidate{
		accountShareRecommendationTestCandidate(1, 0.10, 56, 56, 72, 78),
		accountShareRecommendationTestCandidate(2, 0.11, 55, 55, 70, 76),
		accountShareRecommendationTestCandidate(3, 0.12, 54, 54, 68, 74),
		accountShareRecommendationTestCandidate(4, 0.13, 53, 53, 66, 72),
		accountShareRecommendationTestCandidate(5, 0.14, 52, 52, 64, 70),
		accountShareRecommendationTestCandidate(6, 0.15, 51, 51, 62, 68),
		accountShareRecommendationTestCandidate(7, 0.19, 96, 95, 98, 97),
	}

	selected := accountShareRecommendationSelectCandidates(candidates, 5)

	if len(selected) != 5 {
		t.Fatalf("expected selector to respect limit, got %d candidates", len(selected))
	}
	if !accountShareRecommendationTestContainsListing(selected, 7) {
		t.Fatalf("expected higher quality outlier to remain selectable, got listing IDs %#v", accountShareRecommendationTestListingIDs(selected))
	}
	if accountShareRecommendationTestContainsListing(selected, 5) || accountShareRecommendationTestContainsListing(selected, 6) {
		t.Fatalf("expected lower quality filler candidates to be displaced, got listing IDs %#v", accountShareRecommendationTestListingIDs(selected))
	}
	for i := 1; i < len(selected); i++ {
		if selected[i-1].Estimate.TotalCost > selected[i].Estimate.TotalCost {
			t.Fatalf("expected final candidates to stay sorted by estimated cost, got listing IDs %#v", accountShareRecommendationTestListingIDs(selected))
		}
	}
}

func TestAccountShareModeRecommendListingsDeduplicatesSameAccountIdentity(t *testing.T) {
	identityID := int64(88)
	repo := &accountShareModeRepoStub{
		listingsByPage: map[int][]AccountShareListing{
			1: {
				{
					ID:                 1,
					AccountID:          101,
					AccountIdentityID:  &identityID,
					OwnerUserID:        100,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          2,
					ActiveSeats:        0,
					RateMultiplier:     5,
					HourlyRate:         2,
					PerUserConcurrency: 1,
					AccountConcurrency: 5,
				},
				{
					ID:                 2,
					AccountID:          102,
					AccountIdentityID:  &identityID,
					OwnerUserID:        101,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          2,
					ActiveSeats:        0,
					RateMultiplier:     1,
					HourlyRate:         0,
					PerUserConcurrency: 5,
					AccountConcurrency: 20,
					RatingCount:        3,
					RatingAvg:          9,
				},
			},
		},
	}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 7, UserID: 42, GroupID: accountShareModeInt64Ptr(1)},
	}
	svc := newAccountShareRecommendationTestService(repo, apiKeyRepo)

	got, err := svc.RecommendListings(context.Background(), 42, false, AccountShareRecommendationInput{
		Platform:               PlatformOpenAI,
		Model:                  "gpt-5.4",
		APIKeyID:               7,
		RequestCount:           100,
		ActiveHours:            2,
		InputTokensPerRequest:  1000,
		OutputTokensPerRequest: 500,
		Limit:                  5,
	})
	if err != nil {
		t.Fatalf("RecommendListings failed: %v", err)
	}
	if got.CandidateCount != 1 {
		t.Fatalf("expected one unique candidate, got candidate_count=%d", got.CandidateCount)
	}
	if len(got.Items) != 1 {
		t.Fatalf("expected one visible recommendation, got %d", len(got.Items))
	}
	if got.Items[0].Listing.ID != 2 {
		t.Fatalf("expected better duplicate listing to win, got listing %d", got.Items[0].Listing.ID)
	}
	if got.Recommended == nil || got.Recommended.Listing.ID != 2 {
		t.Fatalf("expected recommended listing 2, got %#v", got.Recommended)
	}
}

func TestAccountShareModeGetRecommendationUsageProfileBuildsDailyAverages(t *testing.T) {
	repo := &accountShareRecommendationUsageProfileRepoStub{
		stats: &AccountShareRecommendationUsageProfileStats{
			TotalRequests:            100,
			TotalInputTokens:         1001,
			TotalOutputTokens:        402,
			TotalCacheCreationTokens: 49,
			TotalCacheReadTokens:     250,
			ActiveHourBuckets:        7,
			ModelMatched:             true,
		},
	}
	svc := &AccountShareModeService{usageProfileRepo: repo}

	profile, err := svc.GetRecommendationUsageProfile(context.Background(), 42, AccountShareRecommendationUsageProfileInput{
		Platform: PlatformOpenAI,
		Model:    "gpt-5.5",
		Days:     3,
	})
	if err != nil {
		t.Fatalf("GetRecommendationUsageProfile failed: %v", err)
	}
	if repo.calls != 1 || repo.userID != 42 || repo.model != "gpt-5.5" {
		t.Fatalf("unexpected repo call: calls=%d user=%d model=%q", repo.calls, repo.userID, repo.model)
	}
	if profile.RequestCount != 34 {
		t.Fatalf("RequestCount = %d, want 34", profile.RequestCount)
	}
	if profile.ActiveHours != 3 {
		t.Fatalf("ActiveHours = %v, want 3", profile.ActiveHours)
	}
	if profile.InputTokensPerRequest != 11 || profile.OutputTokensPerRequest != 5 || profile.CacheCreationTokensPerRequest != 1 || profile.CacheReadTokensPerRequest != 3 {
		t.Fatalf("unexpected per-request tokens: %#v", profile)
	}
	if !profile.HasHistory || !profile.ModelMatched || profile.UsedModelFallback {
		t.Fatalf("unexpected profile flags: %#v", profile)
	}
	if !profile.EndTime.After(profile.StartTime) {
		t.Fatalf("expected valid time range: start=%s end=%s", profile.StartTime, profile.EndTime)
	}
}

func TestAccountShareModeUpdateListingRejectsLifecycleStatusForAllRoles(t *testing.T) {
	for _, actorIsAdmin := range []bool{false, true} {
		role := "owner"
		if actorIsAdmin {
			role = "admin"
		}
		t.Run(role, func(t *testing.T) {
			repo := &accountShareModeRepoStub{}
			svc := &AccountShareModeService{repo: repo}
			status := AccountShareListingStatusPaused
			expectedVersion := int64(1)

			_, err := svc.UpdateListing(
				context.Background(),
				42,
				actorIsAdmin,
				7,
				UpdateAccountShareListingInput{
					Status:          &status,
					ExpectedVersion: &expectedVersion,
				},
			)

			if !errors.Is(err, ErrAccountShareRoomLifecycleCommandRequired) {
				t.Fatalf("expected lifecycle command rejection, got %v", err)
			}
			if repo.updateCalls != 0 {
				t.Fatalf("generic PATCH must not persist lifecycle status, got %d repository calls", repo.updateCalls)
			}
		})
	}
}

func TestAccountShareModeUpdateListingRejectsRoomLevelAccountConcurrencyEdit(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}
	concurrency := AccountShareModeMaxAccountConcurrency + 1
	expectedVersion := int64(1)

	_, err := svc.UpdateListing(context.Background(), 42, true, 7, UpdateAccountShareListingInput{Concurrency: &concurrency, EditSessionID: "edit-session", ExpectedVersion: &expectedVersion})
	if !errors.Is(err, ErrAccountShareRoomAccountConfigUnsupported) {
		t.Fatalf("expected room-level account config rejection, got %v", err)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected repository not to be called, got %d calls", repo.updateCalls)
	}
}

func TestAccountShareModeUpdateListingOwnerPermissions(t *testing.T) {
	repo := &accountShareModeRepoStub{
		updateListing: &AccountShareListing{ID: 7, AccountID: 9, OwnerUserID: 42},
	}
	svc := &AccountShareModeService{repo: repo}
	models := []string{" gpt-5.5 ", "", "gpt-5.4", "gpt-5.5"}
	expectedVersion := int64(1)

	_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{AllowedModels: &models})
	if !errors.Is(err, ErrAccountShareExpectedVersionRequired) {
		t.Fatalf("expected missing expected_version to be rejected, got %v", err)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected missing version to skip repository, got %d calls", repo.updateCalls)
	}

	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		AllowedModels:   &models,
		ExpectedVersion: &expectedVersion,
	})
	if !errors.Is(err, ErrAccountShareUpdateReasonRequired) {
		t.Fatalf("expected update reason to be required, got %v", err)
	}

	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		AllowedModels:   &models,
		ExpectedVersion: &expectedVersion,
		Reason:          "调整可用模型",
	})
	if !errors.Is(err, ErrAccountShareEditSessionRequired) {
		t.Fatalf("expected allowed_models to require an edit session, got %v", err)
	}

	sessionID := "edit-session-1"
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		AllowedModels:   &models,
		EditSessionID:   sessionID,
		ExpectedVersion: &expectedVersion,
		Reason:          "调整可用模型",
	})
	if err != nil {
		t.Fatalf("expected owner model update with edit session to pass, got %v", err)
	}
	if repo.updateCalls != 1 || repo.updateAdmin {
		t.Fatalf("expected one non-admin repository update, calls=%d admin=%t", repo.updateCalls, repo.updateAdmin)
	}
	got := strings.Join(*repo.updateInput.AllowedModels, ",")
	if got != "gpt-5.5,gpt-5.4" {
		t.Fatalf("normalized models = %q", got)
	}

	name := "共享账号一"
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Name: &name, ExpectedVersion: &expectedVersion})
	if !errors.Is(err, ErrAccountShareUpdateReasonRequired) {
		t.Fatalf("expected room-name update reason to be required, got %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("expected missing reason to skip repository, got %d calls", repo.updateCalls)
	}

	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		Reason:          "名称更清晰",
	})
	if err != nil {
		t.Fatalf("expected audited room-name hot update to pass without edit session, got %v", err)
	}
	if repo.updateCalls != 2 {
		t.Fatalf("expected repository update twice, got %d", repo.updateCalls)
	}
	if repo.updateInput.Name == nil || *repo.updateInput.Name != name {
		t.Fatalf("expected trimmed name in update input, got %#v", repo.updateInput.Name)
	}

	status := AccountShareListingStatusPaused
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status, ExpectedVersion: &expectedVersion})
	if !errors.Is(err, ErrAccountShareRoomLifecycleCommandRequired) {
		t.Fatalf("expected status PATCH to require a lifecycle command, got %v", err)
	}
	if repo.updateCalls != 2 {
		t.Fatalf("expected rejected update to skip repository, got %d calls", repo.updateCalls)
	}

	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		Name:            &name,
		ExpectedVersion: &expectedVersion,
		ForceActiveEdit: true,
		Reason:          "owner cannot force",
		Confirmed:       true,
	})
	if !errors.Is(err, ErrAccountShareForceAdminRequired) {
		t.Fatalf("expected owner forced edit to be rejected, got %v", err)
	}
}

func TestAccountShareModeUpdateListingAdminForceRequiresReasonAndConfirmation(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}
	expectedVersion := int64(3)
	seatLimit := 8

	_, err := svc.UpdateListing(context.Background(), 42, true, 7, UpdateAccountShareListingInput{
		SeatLimit:       &seatLimit,
		EditSessionID:   "admin-edit",
		ExpectedVersion: &expectedVersion,
		ForceActiveEdit: true,
		Confirmed:       true,
	})
	if !errors.Is(err, ErrAccountShareForceReasonRequired) {
		t.Fatalf("expected force reason error, got %v", err)
	}

	_, err = svc.UpdateListing(context.Background(), 42, true, 7, UpdateAccountShareListingInput{
		SeatLimit:       &seatLimit,
		EditSessionID:   "admin-edit",
		ExpectedVersion: &expectedVersion,
		ForceActiveEdit: true,
		Reason:          "risk accepted",
	})
	if !errors.Is(err, ErrAccountShareForceConfirmationRequired) {
		t.Fatalf("expected force confirmation error, got %v", err)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected invalid force requests to skip repository, got %d calls", repo.updateCalls)
	}
}

func TestAccountShareModeBeginListingEditAttachesOwnerProxySnapshot(t *testing.T) {
	ownerUserID := int64(42)
	proxyID := int64(77)
	now := time.Now().UTC()
	repo := &accountShareModeRepoStub{
		beginListing: &AccountShareListing{
			ID:          7,
			AccountID:   9,
			OwnerUserID: ownerUserID,
			ProxyID:     &proxyID,
		},
	}
	proxyRepo := &accountShareModeProxyRepoStub{
		proxy: &Proxy{
			ID:          proxyID,
			Name:        "owner-proxy",
			Protocol:    "socks5",
			Host:        "203.0.113.10",
			Port:        1080,
			Username:    "proxy-user",
			Password:    "secret",
			OwnerUserID: &ownerUserID,
			Status:      StatusActive,
			MaxAccounts: 2,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	}
	svc := &AccountShareModeService{repo: repo, proxyRepo: proxyRepo}

	got, err := svc.BeginListingEdit(context.Background(), 100, true, 7, "edit-session", true)
	if err != nil {
		t.Fatalf("BeginListingEdit failed: %v", err)
	}
	if !repo.beginActorIsAdmin {
		t.Fatal("expected admin flag to pass through")
	}
	if repo.beginInput.SessionID != "edit-session" {
		t.Fatalf("unexpected edit session: %q", repo.beginInput.SessionID)
	}
	if !repo.beginInput.Force {
		t.Fatal("expected admin force edit to pass through")
	}
	if proxyRepo.getVisibleCalls != 1 {
		t.Fatalf("expected proxy lookup once, got %d", proxyRepo.getVisibleCalls)
	}
	if proxyRepo.getVisibleUserID != ownerUserID {
		t.Fatalf("expected proxy lookup by owner user %d, got %d", ownerUserID, proxyRepo.getVisibleUserID)
	}
	if proxyRepo.getVisibleID != proxyID {
		t.Fatalf("expected proxy lookup id %d, got %d", proxyID, proxyRepo.getVisibleID)
	}
	if got.Proxy == nil {
		t.Fatal("expected listing proxy snapshot")
	}
	if got.Proxy.ID != proxyID || got.Proxy.Name != "owner-proxy" || got.Proxy.Host != "203.0.113.10" {
		t.Fatalf("unexpected proxy snapshot: %#v", got.Proxy)
	}
}

func TestAccountShareModeBeginListingEditFailsClosedWhenRuntimeUnavailable(t *testing.T) {
	repo := &accountShareEditRuntimeRepoStub{
		state: &AccountShareRoomManagementState{
			ListingID:       7,
			OwnerUserID:     42,
			LifecycleStatus: AccountShareListingStatusPaused,
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	listing, err := svc.BeginListingEdit(context.Background(), 42, false, 7, "edit-session", false)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareRuntimeDependencyUnavailable)
	require.Equal(t, 1, repo.stateCalls)
	require.Zero(t, repo.beginCalls)
}

func TestAccountShareModeBeginListingEditRejectsInFlightRuntime(t *testing.T) {
	repo := &accountShareEditRuntimeRepoStub{
		state: &AccountShareRoomManagementState{
			ListingID:            7,
			OwnerUserID:          42,
			LifecycleStatus:      AccountShareListingStatusPaused,
			RuntimeMembershipIDs: []int64{70},
		},
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 1}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetRuntimeDependencies(NewConcurrencyService(cache), nil, nil, nil)

	listing, err := svc.BeginListingEdit(context.Background(), 42, false, 7, "edit-session", false)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareListingInUse)
	require.Equal(t, 1, repo.stateCalls)
	require.Zero(t, repo.beginCalls)
}

func TestAccountShareModeBeginListingEditRenewalIgnoresOwnValidSessionBlocker(t *testing.T) {
	repo := &accountShareEditRuntimeRepoStub{
		state: &AccountShareRoomManagementState{
			ListingID:       7,
			OwnerUserID:     42,
			LifecycleStatus: AccountShareListingStatusPaused,
			Blockers: AccountShareRoomBlockers{
				ValidEditSession: true,
			},
		},
	}
	cache := &accountShareMembershipConcurrencyCacheStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetRuntimeDependencies(NewConcurrencyService(cache), nil, nil, nil)

	listing, err := svc.BeginListingEdit(context.Background(), 42, false, 7, "edit-session", false)

	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Equal(t, "edit-session", listing.EditSessionID)
	require.Equal(t, 1, repo.stateCalls)
	require.Equal(t, 1, repo.beginCalls)
}

func TestAccountShareModeBeginListingEditAdminForceBypassesRuntimeBlockers(t *testing.T) {
	repo := &accountShareEditRuntimeRepoStub{
		state: &AccountShareRoomManagementState{
			ListingID: 7,
			Blockers: AccountShareRoomBlockers{
				InFlightRequestCount:      1,
				PendingBillingIntentCount: 1,
			},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	listing, err := svc.BeginListingEdit(context.Background(), 9, true, 7, "admin-edit", true)

	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Zero(t, repo.stateCalls)
	require.Equal(t, 1, repo.beginCalls)
}

func TestAccountShareModeListingConfigRejectsNegativeWaiverMinimum(t *testing.T) {
	err := validateAccountShareListingConfig(
		AccountShareModeMinSeats,
		1,
		[]string{"gpt-5"},
		AccountShareModeDefaultPerUserConcurrency,
		AccountShareModeDefaultPerUserConcurrency*AccountShareModeMinSeats,
		0.2,
		-0.01,
		0,
		AccountShareModeDefaultCodexLimitPercent,
		AccountShareModeDefaultCodexLimitPercent,
	)
	if !errors.Is(err, ErrAccountShareModeInvalidWaiverMinimum) {
		t.Fatalf("expected invalid waiver minimum, got %v", err)
	}
}

func TestAccountShareModeListingConfigAcceptsSeatAndConcurrencyIndependently(t *testing.T) {
	err := validateAccountShareListingConfig(
		AccountShareModeMaxSeats,
		1,
		[]string{"gpt-5"},
		AccountShareModeMaxPerUserConcurrency,
		1,
		0.2,
		0,
		0,
		AccountShareModeDefaultCodexLimitPercent,
		AccountShareModeDefaultCodexLimitPercent,
	)
	if err != nil {
		t.Fatalf("seat limit must not be derived from account concurrency, got %v", err)
	}
}

func TestAccountShareRoomQueueLimit(t *testing.T) {
	tests := []struct {
		name      string
		seatLimit int
		want      int
	}{
		{name: "one seat keeps minimum", seatLimit: 1, want: 20},
		{name: "two seats keeps minimum", seatLimit: 2, want: 20},
		{name: "three seats scales", seatLimit: 3, want: 30},
		{name: "ten seats reaches maximum", seatLimit: 10, want: 100},
		{name: "fifteen seats stays capped", seatLimit: 15, want: 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AccountShareRoomQueueLimit(tt.seatLimit); got != tt.want {
				t.Fatalf("AccountShareRoomQueueLimit(%d) = %d, want %d", tt.seatLimit, got, tt.want)
			}
		})
	}
}

func TestAccountShareModeListingConfigSeatBounds(t *testing.T) {
	for _, seatLimit := range []int{AccountShareModeMinSeats, AccountShareModeMaxSeats} {
		err := validateAccountShareListingConfig(
			seatLimit,
			1,
			[]string{"gpt-5"},
			1,
			1,
			0.2,
			0,
			0,
			AccountShareModeDefaultCodexLimitPercent,
			AccountShareModeDefaultCodexLimitPercent,
		)
		if err != nil {
			t.Fatalf("expected seat_limit=%d to be valid, got %v", seatLimit, err)
		}
	}

	for _, seatLimit := range []int{AccountShareModeMinSeats - 1, AccountShareModeMaxSeats + 1} {
		err := validateAccountShareListingConfig(
			seatLimit,
			1,
			[]string{"gpt-5"},
			1,
			1,
			0.2,
			0,
			0,
			AccountShareModeDefaultCodexLimitPercent,
			AccountShareModeDefaultCodexLimitPercent,
		)
		if !errors.Is(err, ErrAccountShareModeInvalidSeats) {
			t.Fatalf("expected seat_limit=%d to be rejected, got %v", seatLimit, err)
		}
	}
}

func TestAccountShareModeListingConfigRejectsAccountConcurrencyAboveLimit(t *testing.T) {
	err := validateAccountShareListingConfig(
		AccountShareModeMinSeats,
		1,
		[]string{"gpt-5"},
		1,
		AccountShareModeMaxAccountConcurrency+1,
		0.2,
		0,
		0,
		AccountShareModeDefaultCodexLimitPercent,
		AccountShareModeDefaultCodexLimitPercent,
	)
	if !errors.Is(err, ErrAccountShareModeInvalidConcurrency) {
		t.Fatalf("expected invalid concurrency, got %v", err)
	}
}

func TestDefaultAccountShareModeAllowedModels(t *testing.T) {
	got := DefaultAccountShareModeAllowedModels()
	if strings.Join(got, ",") != "gpt-5.5,gpt-5.4,gpt-5.4-mini,codex-auto-review" {
		t.Fatalf("unexpected default models: %#v", got)
	}
	got[0] = "changed"
	again := DefaultAccountShareModeAllowedModels()
	if again[0] != "gpt-5.5" {
		t.Fatal("default model slice must not expose mutable backing array")
	}

	anthropic := DefaultAccountShareModeAllowedModelsForPlatform(PlatformAnthropic)
	if !slices.Contains(anthropic, "claude-sonnet-5") {
		t.Fatalf("anthropic defaults must include claude-sonnet-5: %#v", anthropic)
	}
}

func TestAccountShareModeEndMembershipRequiresConfirmationToken(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	_, err := svc.EndMembership(context.Background(), 42, 7, "")
	if !errors.Is(err, ErrAccountShareEndTokenRequired) {
		t.Fatalf("expected token required error, got %v", err)
	}
	if repo.endCalls != 0 {
		t.Fatalf("expected repository not called without token, got %d", repo.endCalls)
	}
}

func TestAccountShareModeJoinListingRejectsZeroIdleTimeout(t *testing.T) {
	svc := &AccountShareModeService{}

	_, err := svc.JoinListing(context.Background(), 1, 2, 3, 0)
	if !errors.Is(err, ErrAccountShareModeInvalidIdleTimeout) {
		t.Fatalf("expected invalid idle timeout, got %v", err)
	}
}

func TestAccountShareModeJoinListingRejectsUnavailableAPIKey(t *testing.T) {
	groupID := int64(1)
	tests := []struct {
		name string
		key  *APIKey
		want error
	}{
		{name: "disabled", key: &APIKey{ID: 3, UserID: 1, GroupID: &groupID, Status: StatusAPIKeyDisabled}, want: ErrAPIKeyInactive},
		{name: "expired status", key: &APIKey{ID: 3, UserID: 1, GroupID: &groupID, Status: StatusAPIKeyExpired}, want: ErrAPIKeyExpired},
		{name: "quota status", key: &APIKey{ID: 3, UserID: 1, GroupID: &groupID, Status: StatusAPIKeyQuotaExhausted}, want: ErrAPIKeyQuotaExhausted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &accountShareModeRepoStub{}
			svc := &AccountShareModeService{
				repo:       repo,
				apiKeyRepo: &accountShareRecommendationAPIKeyRepoStub{key: tt.key},
				userRepo:   &accountShareJoinUserRepoStub{},
			}
			_, err := svc.CreateJoinIntent(context.Background(), 1, 2, CreateAccountShareJoinIntentInput{
				APIKeyID:           3,
				IdleTimeoutMinutes: 10,
				AcceptQueue:        true,
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
		})
	}
}

func TestAccountShareModeJoinIntentRejectsMembershipEnding(t *testing.T) {
	groupID := int64(1)
	revisionID := int64(91)
	listing := &AccountShareListing{
		ID:                 2,
		RowVersion:         7,
		CurrentRevisionID:  &revisionID,
		AccountID:          10,
		RoomName:           "ending-room",
		Platform:           PlatformOpenAI,
		OwnerUserID:        42,
		Status:             AccountShareListingStatusActive,
		SeatLimit:          3,
		QueueStatus:        AccountShareMembershipStatusEnding,
		AccountStatus:      StatusActive,
		AccountSchedulable: true,
	}
	repo := &accountShareModeRepoStub{listing: listing}
	svc := &AccountShareModeService{
		repo: repo,
		apiKeyRepo: &accountShareRecommendationAPIKeyRepoStub{key: &APIKey{
			ID:      3,
			UserID:  1,
			Key:     "sk-account-share",
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
		}},
		userRepo: &accountShareJoinUserRepoStub{user: &User{ID: 1, Balance: 100}},
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	_, err := svc.CreateJoinIntent(context.Background(), 1, listing.ID, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})
	require.ErrorIs(t, err, ErrAccountShareMembershipEnding)
}

func TestAccountShareModeJoinIntentBindsAcceptedTermsToFinalJoin(t *testing.T) {
	groupID := int64(1)
	revisionID := int64(91)
	listing := &AccountShareListing{
		ID:                      2,
		RowVersion:              7,
		CurrentRevisionID:       &revisionID,
		AccountID:               10,
		RoomName:                "stable-room",
		Platform:                PlatformOpenAI,
		OwnerUserID:             42,
		Status:                  AccountShareListingStatusActive,
		SeatLimit:               3,
		ActiveSeats:             1,
		RateMultiplier:          0.75,
		AllowedModels:           []string{"gpt-5.5"},
		PerUserConcurrency:      2,
		HourlyRate:              0.3,
		HourlyFeeWaiverMinimum:  0.1,
		MinBalanceRequired:      1,
		CodexCLIOnly:            true,
		Codex5hLimitPercent:     90,
		Codex7dLimitPercent:     80,
		AccountStatus:           StatusActive,
		AccountSchedulable:      true,
		Anthropic5hLimitPercent: 0,
		Anthropic7dLimitPercent: 0,
	}
	repo := &accountShareModeRepoStub{
		listing:        listing,
		joinMembership: &AccountShareMembership{ID: 300, ListingID: listing.ID, ConsumerUserID: 1, APIKeyID: 3, Status: AccountShareMembershipStatusActive},
	}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{key: &APIKey{
		ID:      3,
		UserID:  1,
		Key:     "sk-account-share",
		GroupID: &groupID,
		Status:  StatusAPIKeyActive,
	}}
	svc := &AccountShareModeService{
		repo:       repo,
		apiKeyRepo: apiKeyRepo,
		userRepo:   &accountShareJoinUserRepoStub{user: &User{ID: 1, Balance: 100}},
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateJoinIntent(context.Background(), 1, listing.ID, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})
	require.NoError(t, err)
	require.Equal(t, listing.RowVersion, intent.ExpectedVersion)
	require.Equal(t, revisionID, intent.ExpectedRevisionID)
	require.NotNil(t, intent.Terms)
	require.Equal(t, listing.HourlyRate, intent.Terms.HourlyRate)
	require.Equal(t, listing.AllowedModels, intent.Terms.AllowedModels)

	membership, err := svc.CompleteJoinListing(context.Background(), 1, listing.ID, CompleteAccountShareJoinInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		IntentToken:        intent.Token,
		ExpectedVersion:    intent.ExpectedVersion,
		ExpectedRevisionID: intent.ExpectedRevisionID,
		AcceptQueue:        intent.AcceptQueue,
	})
	require.NoError(t, err)
	require.Equal(t, int64(300), membership.ID)
	require.Equal(t, listing.RowVersion, repo.joinInput.ExpectedVersion)
	require.Equal(t, revisionID, repo.joinInput.ExpectedRevisionID)
	require.Equal(t, intent.AcceptQueue, repo.joinInput.AcceptQueue)
	require.Equal(t, listing.HourlyRate, repo.joinInput.AcceptedTerms.HourlyRate)
	require.NotEmpty(t, repo.joinInput.IntentNonce)
	require.False(t, repo.joinInput.IntentIssuedAt.IsZero())
}

func TestAccountShareModeJoinIntentMaterializesLegacyRevisionBeforeSigning(t *testing.T) {
	groupID := int64(1)
	listing := &AccountShareListing{
		ID:                      2,
		RowVersion:              1,
		AccountID:               10,
		RoomName:                "legacy-room",
		Platform:                PlatformOpenAI,
		OwnerUserID:             42,
		Status:                  AccountShareListingStatusActive,
		SeatLimit:               3,
		RateMultiplier:          0.75,
		AllowedModels:           []string{"gpt-5.5"},
		PerUserConcurrency:      2,
		HourlyRate:              0.3,
		HourlyFeeWaiverMinimum:  0.1,
		MinBalanceRequired:      1,
		CodexCLIOnly:            true,
		Codex5hLimitPercent:     90,
		Codex7dLimitPercent:     80,
		Anthropic5hLimitPercent: 90,
		Anthropic7dLimitPercent: 80,
		AccountStatus:           StatusActive,
		AccountSchedulable:      true,
	}
	revisionTerms := accountShareJoinTermsFromListing(listing, 91)
	repo := &accountShareModeRepoStub{
		listing:       listing,
		revisionTerms: &revisionTerms,
	}
	svc := &AccountShareModeService{
		repo: repo,
		apiKeyRepo: &accountShareRecommendationAPIKeyRepoStub{key: &APIKey{
			ID:      3,
			UserID:  1,
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
		}},
		userRepo: &accountShareJoinUserRepoStub{user: &User{ID: 1, Balance: 100}},
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateJoinIntent(context.Background(), 1, listing.ID, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})

	require.NoError(t, err)
	require.Equal(t, int64(91), intent.ExpectedRevisionID)
	require.Equal(t, int64(1), intent.ExpectedVersion)
	require.NotNil(t, listing.CurrentRevisionID)
	require.Equal(t, int64(91), *listing.CurrentRevisionID)
	require.Equal(t, float64(90), intent.Terms.Anthropic5hLimitPercent)
	require.Equal(t, float64(80), intent.Terms.Anthropic7dLimitPercent)
}

func TestAccountShareModeJoinIntentRejectsTermsChangedAfterConfirmation(t *testing.T) {
	groupID := int64(1)
	revisionID := int64(91)
	listing := &AccountShareListing{
		ID:                  2,
		RowVersion:          7,
		CurrentRevisionID:   &revisionID,
		AccountID:           10,
		RoomName:            "stable-room",
		Platform:            PlatformOpenAI,
		OwnerUserID:         42,
		Status:              AccountShareListingStatusActive,
		SeatLimit:           3,
		RateMultiplier:      0.75,
		AllowedModels:       []string{"gpt-5.5"},
		PerUserConcurrency:  2,
		HourlyRate:          0.3,
		MinBalanceRequired:  1,
		AccountStatus:       StatusActive,
		AccountSchedulable:  true,
		Codex5hLimitPercent: 90,
		Codex7dLimitPercent: 80,
	}
	repo := &accountShareModeRepoStub{listing: listing}
	svc := &AccountShareModeService{
		repo: repo,
		apiKeyRepo: &accountShareRecommendationAPIKeyRepoStub{key: &APIKey{
			ID:      3,
			UserID:  1,
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
		}},
		userRepo: &accountShareJoinUserRepoStub{user: &User{ID: 1, Balance: 100}},
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))
	intent, err := svc.CreateJoinIntent(context.Background(), 1, listing.ID, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})
	require.NoError(t, err)

	listing.RowVersion++
	listing.HourlyRate = 0.5
	nextRevisionID := revisionID + 1
	listing.CurrentRevisionID = &nextRevisionID
	_, err = svc.CompleteJoinListing(context.Background(), 1, listing.ID, CompleteAccountShareJoinInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		IntentToken:        intent.Token,
		ExpectedVersion:    intent.ExpectedVersion,
		ExpectedRevisionID: intent.ExpectedRevisionID,
		AcceptQueue:        true,
	})
	require.ErrorIs(t, err, ErrAccountShareJoinTermsChanged)
	require.Zero(t, repo.joinInput.ListingID)
}

func TestAccountShareModeJoinIntentRejectsTamperedTokenAndQueueFlag(t *testing.T) {
	groupID := int64(1)
	revisionID := int64(91)
	listing := &AccountShareListing{
		ID:                  2,
		RowVersion:          7,
		CurrentRevisionID:   &revisionID,
		AccountID:           10,
		RoomName:            "stable-room",
		Platform:            PlatformOpenAI,
		OwnerUserID:         42,
		Status:              AccountShareListingStatusActive,
		SeatLimit:           3,
		AllowedModels:       []string{"gpt-5.5"},
		PerUserConcurrency:  2,
		MinBalanceRequired:  1,
		AccountStatus:       StatusActive,
		AccountSchedulable:  true,
		Codex5hLimitPercent: 90,
		Codex7dLimitPercent: 80,
	}
	repo := &accountShareModeRepoStub{listing: listing}
	svc := &AccountShareModeService{
		repo: repo,
		apiKeyRepo: &accountShareRecommendationAPIKeyRepoStub{key: &APIKey{
			ID:      3,
			UserID:  1,
			GroupID: &groupID,
			Status:  StatusAPIKeyActive,
		}},
		userRepo: &accountShareJoinUserRepoStub{user: &User{ID: 1, Balance: 100}},
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))
	intent, err := svc.CreateJoinIntent(context.Background(), 1, listing.ID, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})
	require.NoError(t, err)

	_, err = svc.CompleteJoinListing(context.Background(), 1, listing.ID, CompleteAccountShareJoinInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		IntentToken:        intent.Token + "tampered",
		ExpectedVersion:    intent.ExpectedVersion,
		ExpectedRevisionID: intent.ExpectedRevisionID,
		AcceptQueue:        true,
	})
	require.ErrorIs(t, err, ErrAccountShareJoinIntentInvalid)

	_, err = svc.CompleteJoinListing(context.Background(), 1, listing.ID, CompleteAccountShareJoinInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		IntentToken:        intent.Token,
		ExpectedVersion:    intent.ExpectedVersion,
		ExpectedRevisionID: intent.ExpectedRevisionID,
		AcceptQueue:        false,
	})
	require.ErrorIs(t, err, ErrAccountShareJoinIntentInvalid)
	require.Zero(t, repo.joinInput.ListingID)
}

func TestAccountShareModeUpdateMembershipIdleTimeoutRejectsZeroIdleTimeout(t *testing.T) {
	svc := &AccountShareModeService{}

	_, err := svc.UpdateMembershipIdleTimeout(context.Background(), 1, 2, 0)
	if !errors.Is(err, ErrAccountShareModeInvalidIdleTimeout) {
		t.Fatalf("expected invalid idle timeout, got %v", err)
	}
}

func TestAccountShareModeSubmitReviewRejectsCommentWithoutModerationConfig(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{
		repo:              repo,
		reviewSettingRepo: &accountShareReviewSettingRepoStub{values: map[string]string{}},
	}

	_, err := svc.SubmitReview(context.Background(), 10, 20, SubmitAccountShareReviewInput{
		Score:   8,
		Comment: "  使用稳定  ",
	})
	if !errors.Is(err, ErrAccountShareCommentReviewUnavailable) {
		t.Fatalf("expected moderation unavailable, got %v", err)
	}
	if repo.submitReviewCalls != 0 {
		t.Fatalf("expected repository not called, got %d", repo.submitReviewCalls)
	}
}

func TestAccountShareModeSubmitReviewAllowsCommentWithModerationConfig(t *testing.T) {
	repo := &accountShareModeRepoStub{
		submitReview: &AccountShareReview{ID: 3, Score: 9, Comment: "使用稳定"},
	}
	svc := &AccountShareModeService{
		repo: repo,
		reviewSettingRepo: &accountShareReviewSettingRepoStub{values: map[string]string{
			SettingKeyAccountShareCommentReviewEnabled: "true",
			SettingKeyAccountShareCommentReviewURL:     "https://api.example.com/v1/chat/completions",
			SettingKeyAccountShareCommentReviewAPIKey:  "review-key",
			SettingKeyAccountShareCommentReviewModel:   "review-model",
		}},
	}

	review, err := svc.SubmitReview(context.Background(), 10, 20, SubmitAccountShareReviewInput{
		Score:   9,
		Comment: "  使用稳定  ",
	})
	if err != nil {
		t.Fatalf("SubmitReview failed: %v", err)
	}
	if review == nil || review.ID != 3 {
		t.Fatalf("unexpected review: %#v", review)
	}
	if repo.submitReviewCalls != 1 {
		t.Fatalf("expected repository called once, got %d", repo.submitReviewCalls)
	}
	if repo.submitReviewInput.Comment != "使用稳定" {
		t.Fatalf("expected trimmed comment, got %q", repo.submitReviewInput.Comment)
	}
}

func TestAccountShareModeReviewModerationAcceptsStrictPassDecision(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer review-key" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"decision":"pass","reason":""}`}},
			},
		})
	}))
	defer server.Close()

	svc := &AccountShareModeService{reviewHTTPClient: server.Client()}
	result, err := svc.callAccountShareCommentReviewModel(context.Background(), accountShareCommentReviewConfig{
		Enabled: true,
		URL:     server.URL,
		APIKey:  "review-key",
		Model:   "review-model",
	}, &AccountShareReview{Score: 9, Comment: "使用稳定", Platform: PlatformOpenAI, AccountName: "账号A"})
	if err != nil {
		t.Fatalf("call moderation model failed: %v", err)
	}
	if !result.Passed || result.RejectReason != "" {
		t.Fatalf("unexpected moderation result: %#v", result)
	}
	if result.ModelSnapshot != "review-model" || result.URLSnapshot != server.URL {
		t.Fatalf("unexpected moderation snapshots: %#v", result)
	}
}

func TestAccountShareModeReviewModerationRejectRequiresReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `{"decision":"reject","reason":""}`}},
			},
		})
	}))
	defer server.Close()

	svc := &AccountShareModeService{reviewHTTPClient: server.Client()}
	_, err := svc.callAccountShareCommentReviewModel(context.Background(), accountShareCommentReviewConfig{
		Enabled: true,
		URL:     server.URL,
		APIKey:  "review-key",
		Model:   "review-model",
	}, &AccountShareReview{Score: 1, Comment: "广告", Platform: PlatformOpenAI, AccountName: "账号A"})
	if err == nil || !strings.Contains(err.Error(), "reject decision reason is required") {
		t.Fatalf("expected reject reason error, got %v", err)
	}
}

func TestAccountShareModeEndMembershipAcceptsIssuedConfirmationToken(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 0, 0, 123000000, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             7,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusQueued,
			UpdatedAt:      updatedAt,
		},
		endMembership: &AccountShareMembership{
			ID:             7,
			ConsumerUserID: 42,
			OwnerUserID:    100,
			APIKeyID:       0,
			Status:         AccountShareMembershipStatusEnded,
			UpdatedAt:      updatedAt.Add(time.Second),
		},
	}
	svc := &AccountShareModeService{repo: repo}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 7)
	if err != nil {
		t.Fatalf("CreateEndMembershipToken failed: %v", err)
	}
	membership, err := svc.EndMembership(context.Background(), 42, 7, intent.Token)
	if err != nil {
		t.Fatalf("EndMembership failed: %v", err)
	}
	if membership == nil || membership.ID != 7 {
		t.Fatalf("unexpected membership: %#v", membership)
	}
	if repo.endCalls != 1 {
		t.Fatalf("expected repository called once, got %d", repo.endCalls)
	}
	if repo.finalizeCalls != 0 {
		t.Fatalf("queued membership must not enter active finalizer, got %d calls", repo.finalizeCalls)
	}
	if repo.endInput.ExpectedMembershipStatus != AccountShareMembershipStatusQueued ||
		!repo.endInput.ExpectedUpdatedAt.Equal(updatedAt) ||
		repo.endInput.OperationID == "" {
		t.Fatalf("end snapshot was not bound to repository input: %#v", repo.endInput)
	}
}

func TestAccountShareModeEndMembershipActiveWithoutLeaseFinalizes(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 5, 0, 456000000, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             71,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      updatedAt,
		},
		endMembership: &AccountShareMembership{
			ID:             71,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnding,
			UpdatedAt:      updatedAt.Add(time.Second),
		},
		finalizeMembership: &AccountShareMembership{
			ID:             71,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnded,
		},
		finalizeDone: true,
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 0}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 71)
	require.NoError(t, err)
	membership, err := svc.EndMembership(context.Background(), 42, 71, intent.Token)
	require.NoError(t, err)
	require.NotNil(t, membership)
	require.Equal(t, AccountShareMembershipStatusEnded, membership.Status)
	require.Equal(t, 1, repo.finalizeCalls)
	require.NotEmpty(t, repo.endInput.OperationID)
	require.Equal(t, repo.endInput.OperationID, repo.finalizeOperationID)
}

func TestAccountShareModeEndMembershipActiveLeaseReturnsEnding(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 10, 0, 0, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             72,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      updatedAt,
		},
		endMembership: &AccountShareMembership{
			ID:             72,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnding,
		},
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 1}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 72)
	require.NoError(t, err)
	membership, err := svc.EndMembership(context.Background(), 42, 72, intent.Token)
	require.NoError(t, err)
	require.NotNil(t, membership)
	require.Equal(t, AccountShareMembershipStatusEnding, membership.Status)
	require.Equal(t, 0, repo.finalizeCalls)
}

func TestAccountShareModeEndMembershipUnknownLeaseFailsClosed(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 15, 0, 0, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             73,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      updatedAt,
		},
		endMembership: &AccountShareMembership{
			ID:             73,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnding,
		},
	}
	cache := &accountShareMembershipConcurrencyCacheStub{currentErr: errors.New("redis unavailable")}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 73)
	require.NoError(t, err)
	membership, err := svc.EndMembership(context.Background(), 42, 73, intent.Token)
	require.NoError(t, err)
	require.NotNil(t, membership)
	require.Equal(t, AccountShareMembershipStatusEnding, membership.Status)
	require.Equal(t, 0, repo.finalizeCalls)
}

func TestAccountShareModeEndMembershipPendingIntentStaysEnding(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 20, 0, 0, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             74,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      updatedAt,
		},
		endMembership: &AccountShareMembership{
			ID:             74,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnding,
		},
		finalizeMembership: &AccountShareMembership{
			ID:               74,
			ConsumerUserID:   42,
			Status:           AccountShareMembershipStatusEnding,
			SettlementStatus: "pending",
		},
		finalizeDone: false,
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 0}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 74)
	require.NoError(t, err)
	membership, err := svc.EndMembership(context.Background(), 42, 74, intent.Token)
	require.NoError(t, err)
	require.NotNil(t, membership)
	require.Equal(t, AccountShareMembershipStatusEnding, membership.Status)
	require.Equal(t, "pending", membership.SettlementStatus)
	require.Equal(t, 1, repo.finalizeCalls)
}

func TestAccountShareModeEndMembershipStaleSnapshotIsRejected(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 25, 0, 0, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             75,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      updatedAt,
		},
		endErr: ErrAccountShareEndStateConflict,
	}
	svc := &AccountShareModeService{repo: repo}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 75)
	require.NoError(t, err)
	_, err = svc.EndMembership(context.Background(), 42, 75, intent.Token)
	require.ErrorIs(t, err, ErrAccountShareEndStateConflict)
	require.Equal(t, 1, repo.endCalls)
	require.Equal(t, 0, repo.finalizeCalls)
}

func TestAccountShareModeEndMembershipTokenReplayUsesSameOperation(t *testing.T) {
	updatedAt := time.Date(2026, 7, 27, 6, 30, 0, 0, time.UTC)
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             76,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      updatedAt,
		},
		endMembership: &AccountShareMembership{
			ID:             76,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnding,
		},
		finalizeMembership: &AccountShareMembership{
			ID:             76,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnded,
		},
		finalizeDone: true,
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 0}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 76)
	require.NoError(t, err)
	claims, err := svc.validateEndMembershipToken(intent.Token, 42, 76, time.Now().UTC())
	require.NoError(t, err)
	require.NotEmpty(t, claims.Nonce)
	require.NotEmpty(t, claims.OperationID)

	first, err := svc.EndMembership(context.Background(), 42, 76, intent.Token)
	require.NoError(t, err)
	second, err := svc.EndMembership(context.Background(), 42, 76, intent.Token)
	require.NoError(t, err)
	require.Equal(t, AccountShareMembershipStatusEnded, first.Status)
	require.Equal(t, AccountShareMembershipStatusEnded, second.Status)
	require.Equal(t, 2, repo.endCalls)
	require.Equal(t, claims.OperationID, repo.endInput.OperationID)
	require.Equal(t, claims.OperationID, repo.finalizeOperationID)
}

func TestAccountShareModeCreateEndTokenRejectsEndedMembership(t *testing.T) {
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             77,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnded,
			UpdatedAt:      time.Now().UTC(),
		},
	}
	svc := &AccountShareModeService{repo: repo}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	_, err := svc.CreateEndMembershipToken(context.Background(), 42, 77)
	require.ErrorIs(t, err, ErrAccountShareEndStateConflict)
	require.Equal(t, 0, repo.endCalls)
}

func TestAccountShareModeEndingWorkerFinalizesAfterLeaseDrains(t *testing.T) {
	operationID := "1649195d-41e1-48ff-b71e-ddde7e0f2ed8"
	repo := &accountShareModeRepoStub{
		endingCandidates: []AccountShareEndingMembershipCandidate{{
			MembershipID: 78,
			OperationID:  operationID,
		}},
		finalizeMembership: &AccountShareMembership{
			ID:             78,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusEnded,
		},
		finalizeDone: true,
	}
	cache := &accountShareMembershipConcurrencyCacheStub{current: 0}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}

	svc.processEndingMembershipsOnce(context.Background())

	require.Equal(t, 1, repo.finalizeCalls)
	require.Equal(t, operationID, repo.finalizeOperationID)
}

func TestAccountShareModeResolveBindingUsesRequestContextCache(t *testing.T) {
	repo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{ID: 11, AccountID: 99, ConsumerUserID: 20, APIKeyID: 30},
		listing:    &AccountShareListing{ID: 12, AccountID: 99, OwnerUserID: 40, Status: AccountShareListingStatusActive},
	}
	svc := &AccountShareModeService{repo: repo}
	selectionCtx := WithAccountShareModeRequest(context.Background(), 20, 30)

	if _, _, err := svc.ResolveActiveBindingForRequest(selectionCtx, 20, 30, 50); err != nil {
		t.Fatalf("first resolve failed: %v", err)
	}
	taskCtx := WithAccountShareModeRequestFromContext(context.Background(), selectionCtx)
	if _, _, err := svc.ResolveActiveBindingForRequest(taskCtx, 20, 30, 50); err != nil {
		t.Fatalf("second resolve failed: %v", err)
	}
	if repo.isModeCalls != 1 {
		t.Fatalf("expected mode group check once, got %d", repo.isModeCalls)
	}
	if repo.bindingCalls != 1 {
		t.Fatalf("expected binding query once, got %d", repo.bindingCalls)
	}
}

func TestAccountShareModeResolveBindingRefreshesExpiredSeatBeforeActivatingQueue(t *testing.T) {
	repo := &accountShareModeRepoStub{
		bindingResults: []accountShareModeBindingResult{
			{err: ErrAccountShareListingNotFound},
			{
				membership: &AccountShareMembership{ID: 11, AccountID: 99, ConsumerUserID: 20, APIKeyID: 30},
				listing:    &AccountShareListing{ID: 12, AccountID: 99, OwnerUserID: 40, Status: AccountShareListingStatusActive},
			},
		},
	}
	svc := &AccountShareModeService{repo: repo}
	selectionCtx := WithAccountShareModeRequest(context.Background(), 20, 30)

	membership, listing, err := svc.ResolveActiveBindingForRequest(selectionCtx, 20, 30, 50)
	if err != nil {
		t.Fatalf("resolve after seat billing catch-up failed: %v", err)
	}
	if membership == nil || membership.ID != 11 || listing == nil || listing.ID != 12 {
		t.Fatalf("unexpected binding after catch-up: membership=%#v listing=%#v", membership, listing)
	}
	if repo.requestBillingCalls != 1 {
		t.Fatalf("expected one request billing catch-up, got %d", repo.requestBillingCalls)
	}
	if repo.bindingCalls != 2 {
		t.Fatalf("expected active binding to be queried again after billing catch-up, got %d", repo.bindingCalls)
	}
	if repo.activationCalls != 0 {
		t.Fatalf("expected renewed active binding to avoid queued activation, got %d", repo.activationCalls)
	}

	taskCtx := WithAccountShareModeRequestFromContext(context.Background(), selectionCtx)
	if _, _, err := svc.ResolveActiveBindingForRequest(taskCtx, 20, 30, 50); err != nil {
		t.Fatalf("cached resolve failed: %v", err)
	}
	if repo.requestBillingCalls != 1 {
		t.Fatalf("expected cached resolve to avoid extra billing catch-up, got %d", repo.requestBillingCalls)
	}
	if repo.bindingCalls != 2 {
		t.Fatalf("expected cached resolve to avoid extra binding query, got %d", repo.bindingCalls)
	}
	if repo.activationCalls != 0 {
		t.Fatalf("expected cached resolve to avoid extra activation, got %d", repo.activationCalls)
	}
}

func TestAccountShareModeResolveBindingRecoversConcurrentActivationWinner(t *testing.T) {
	membership := &AccountShareMembership{ID: 11, AccountID: 99, ConsumerUserID: 20, APIKeyID: 30}
	listing := &AccountShareListing{ID: 12, AccountID: 99, OwnerUserID: 40, Status: AccountShareListingStatusActive}
	repo := &accountShareModeRepoStub{
		bindingResults: []accountShareModeBindingResult{
			{err: ErrAccountShareListingNotFound},
			{err: ErrAccountShareListingNotFound},
			{err: ErrAccountShareListingNotFound},
			{err: ErrAccountShareAPIKeyAlreadyBound},
			{membership: membership, listing: listing},
		},
	}
	svc := &AccountShareModeService{repo: repo}
	gotMembership, gotListing, err := svc.ResolveActiveBindingForRequest(WithAccountShareModeRequest(context.Background(), 20, 30), 20, 30, 50)
	if err != nil {
		t.Fatalf("resolve concurrent activation winner failed: %v", err)
	}
	if gotMembership == nil || gotMembership.ID != membership.ID || gotListing == nil || gotListing.ID != listing.ID {
		t.Fatalf("unexpected recovered binding: membership=%#v listing=%#v", gotMembership, gotListing)
	}
}

func TestAccountShareModeMembershipHeartbeatAndReleaseTouchCompletion(t *testing.T) {
	repo := &accountShareModeRepoStub{touchSignal: make(chan time.Time, 4)}
	svc := &AccountShareModeService{repo: repo}
	stop := make(chan struct{})
	done := make(chan struct{})
	go svc.runMembershipHeartbeat(context.Background().Done(), 11, time.Millisecond, stop, done)
	select {
	case <-repo.touchSignal:
	case <-time.After(time.Second):
		t.Fatal("membership heartbeat did not touch last_request_at")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("membership heartbeat did not stop")
	}
	if err := svc.forceTouchMembershipLastRequest(11, time.Now().UTC()); err != nil {
		t.Fatalf("completion touch failed: %v", err)
	}
	if repo.touchCalls < 2 {
		t.Fatalf("expected heartbeat and completion touches, got %d", repo.touchCalls)
	}
}

func TestAccountShareModeAcquireMembershipSlotReleaseIsIdempotent(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	cache := &accountShareMembershipConcurrencyCacheStub{}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}

	result, err := svc.AcquireMembershipSlot(context.Background(), 11, 2)
	if err != nil {
		t.Fatalf("acquire membership slot failed: %v", err)
	}
	if result == nil || !result.Acquired || result.ReleaseFunc == nil {
		t.Fatalf("unexpected acquire result: %#v", result)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result.ReleaseFunc()
		}()
	}
	wg.Wait()

	if cache.acquireCalls != 1 {
		t.Fatalf("acquire calls = %d, want 1", cache.acquireCalls)
	}
	if cache.releaseCalls != 1 {
		t.Fatalf("underlying release calls = %d, want 1", cache.releaseCalls)
	}
	if repo.touchCalls != 2 {
		t.Fatalf("initial and completion touch calls = %d, want 2", repo.touchCalls)
	}
}

func TestAccountShareModeAcquireMembershipSlotReleasesWhenMembershipIsNoLongerActive(t *testing.T) {
	repo := &accountShareModeRepoStub{touchErr: ErrAccountShareListingNotFound}
	cache := &accountShareMembershipConcurrencyCacheStub{}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}

	result, err := svc.AcquireMembershipSlot(context.Background(), 11, 2)
	if !errors.Is(err, ErrAccountShareListingNotFound) {
		t.Fatalf("expected inactive membership error, got result=%#v err=%v", result, err)
	}
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	if cache.acquireCalls != 1 || cache.releaseCalls != 1 {
		t.Fatalf("slot acquire/release calls = %d/%d, want 1/1", cache.acquireCalls, cache.releaseCalls)
	}
}

func TestAccountShareModeAcquireMembershipSlotFailsClosedWithoutDependenciesOrValidParameters(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	cache := &accountShareMembershipConcurrencyCacheStub{}
	tests := []struct {
		name           string
		service        *AccountShareModeService
		membershipID   int64
		maxConcurrency int
	}{
		{
			name:           "nil service",
			service:        nil,
			membershipID:   11,
			maxConcurrency: 2,
		},
		{
			name:           "missing repository",
			service:        &AccountShareModeService{concurrencyService: NewConcurrencyService(cache)},
			membershipID:   11,
			maxConcurrency: 2,
		},
		{
			name:           "missing concurrency service",
			service:        &AccountShareModeService{repo: repo},
			membershipID:   11,
			maxConcurrency: 2,
		},
		{
			name: "invalid membership id",
			service: &AccountShareModeService{
				repo:               repo,
				concurrencyService: NewConcurrencyService(cache),
			},
			membershipID:   0,
			maxConcurrency: 2,
		},
		{
			name: "invalid concurrency",
			service: &AccountShareModeService{
				repo:               repo,
				concurrencyService: NewConcurrencyService(cache),
			},
			membershipID:   11,
			maxConcurrency: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.service.AcquireMembershipSlot(context.Background(), test.membershipID, test.maxConcurrency)
			require.ErrorIs(t, err, ErrAccountShareRuntimeLeaseUnavailable)
			require.Nil(t, result)
		})
	}
	require.Zero(t, cache.acquireCalls)
}

func TestAccountShareModeAcquireMembershipSlotFailsClosedWithoutRefreshCapability(t *testing.T) {
	cache := &accountShareMembershipNoLeaseCacheStub{}
	svc := &AccountShareModeService{
		repo:               &accountShareModeRepoStub{},
		concurrencyService: NewConcurrencyService(cache),
	}

	result, err := svc.AcquireMembershipSlot(context.Background(), 11, 2)

	require.ErrorIs(t, err, ErrAccountShareRuntimeLeaseUnavailable)
	require.Nil(t, result)
	require.Zero(t, cache.acquireCalls)
	require.Zero(t, cache.releaseCalls)
}

func TestAccountShareModeAcquireMembershipSlotFailsClosedWithInvalidLeaseTTL(t *testing.T) {
	cache := &accountShareMembershipConcurrencyCacheStub{invalidLeaseTTL: true}
	svc := &AccountShareModeService{
		repo:               &accountShareModeRepoStub{},
		concurrencyService: NewConcurrencyService(cache),
	}

	result, err := svc.AcquireMembershipSlot(context.Background(), 11, 2)

	require.ErrorIs(t, err, ErrAccountShareRuntimeLeaseUnavailable)
	require.Nil(t, result)
	require.Zero(t, cache.acquireCalls)
	require.Zero(t, cache.releaseCalls)
}

func TestAccountShareModeResolveBindingClearsUnavailableAccount(t *testing.T) {
	resetAt := time.Now().UTC().Add(time.Hour)
	repo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{ID: 11, AccountID: 99, ConsumerUserID: 20, APIKeyID: 30},
		listing: &AccountShareListing{
			ID:                  12,
			AccountID:           99,
			OwnerUserID:         40,
			Status:              AccountShareListingStatusActive,
			AccountStatus:       StatusActive,
			AccountSchedulable:  true,
			RateLimitResetAt:    &resetAt,
			CurrentMembershipID: accountShareModeInt64Ptr(11),
			CurrentAPIKeyID:     accountShareModeInt64Ptr(30),
		},
	}
	svc := &AccountShareModeService{repo: repo}
	selectionCtx := WithAccountShareModeRequest(context.Background(), 20, 30)

	membership, listing, err := svc.ResolveActiveBindingForRequest(selectionCtx, 20, 30, 50)
	if !errors.Is(err, ErrAccountShareModeGroupUnbound) {
		t.Fatalf("expected unavailable account to return unbound, got membership=%#v listing=%#v err=%v", membership, listing, err)
	}
	if repo.unavailableCalls != 1 {
		t.Fatalf("expected one unavailable clear call, got %d", repo.unavailableCalls)
	}

	taskCtx := WithAccountShareModeRequestFromContext(context.Background(), selectionCtx)
	_, _, err = svc.ResolveActiveBindingForRequest(taskCtx, 20, 30, 50)
	if !errors.Is(err, ErrAccountShareModeGroupUnbound) {
		t.Fatalf("expected cached unbound error, got %v", err)
	}
	if repo.bindingCalls != 5 {
		t.Fatalf("expected unavailable resolve to query active binding before retry, got %d", repo.bindingCalls)
	}
	if repo.unavailableCalls != 1 {
		t.Fatalf("expected cached unavailable resolve to skip clear call, got %d", repo.unavailableCalls)
	}
}

func TestAccountShareModeResolveBindingCachesNonModeGroup(t *testing.T) {
	repo := &accountShareModeRepoStub{modeGroup: accountShareModeBoolPtr(false)}
	svc := &AccountShareModeService{repo: repo}
	selectionCtx := WithAccountShareModeRequest(context.Background(), 20, 30)

	if membership, listing, err := svc.ResolveActiveBindingForRequest(selectionCtx, 20, 30, 50); err != nil || membership != nil || listing != nil {
		t.Fatalf("expected non-mode group to resolve empty result, membership=%v listing=%v err=%v", membership, listing, err)
	}
	taskCtx := WithAccountShareModeRequestFromContext(context.Background(), selectionCtx)
	if membership, listing, err := svc.ResolveActiveBindingForRequest(taskCtx, 20, 30, 50); err != nil || membership != nil || listing != nil {
		t.Fatalf("expected cached non-mode group to resolve empty result, membership=%v listing=%v err=%v", membership, listing, err)
	}
	if repo.isModeCalls != 1 {
		t.Fatalf("expected mode group check once, got %d", repo.isModeCalls)
	}
	if repo.bindingCalls != 0 {
		t.Fatalf("expected no binding query for non-mode group, got %d", repo.bindingCalls)
	}
}

func TestBuildAccountShareModeBillingSnapshotWithoutGlobalPolicyKeepsPlatformRevenue(t *testing.T) {
	snapshot := BuildAccountShareModeBillingSnapshot(
		&AccountShareMembership{ID: 1, AccountID: 10, ConsumerUserID: 20, APIKeyID: 30},
		&AccountShareListing{ID: 2, AccountID: 10, OwnerUserID: 40, RateMultiplier: 1, HourlyRate: 0.2},
		nil,
		1.25,
		0,
		100,
	)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.OwnerShareRatio != 0 {
		t.Fatalf("owner ratio = %v, want 0", snapshot.OwnerShareRatio)
	}
	if snapshot.PlatformShareRatio != 1 {
		t.Fatalf("platform ratio = %v, want 1", snapshot.PlatformShareRatio)
	}
}

func TestBuildAccountShareModeBillingSnapshotKeepsExplicitZeroRatio(t *testing.T) {
	snapshot := BuildAccountShareModeBillingSnapshot(
		&AccountShareMembership{ID: 1, AccountID: 10, ConsumerUserID: 20, APIKeyID: 30},
		&AccountShareListing{ID: 2, AccountID: 10, OwnerUserID: 40, RateMultiplier: 1, HourlyRate: 0.2},
		&AccountSharePolicy{ID: 9, Version: 2, OwnerShareRatio: 0, InviteShareRatio: 0.75},
		1.25,
		0,
		100,
	)
	if snapshot == nil {
		t.Fatal("expected snapshot")
	}
	if snapshot.OwnerShareRatio != 0 {
		t.Fatalf("owner ratio = %v, want 0", snapshot.OwnerShareRatio)
	}
	if snapshot.PlatformShareRatio != 0.25 {
		t.Fatalf("platform ratio = %v, want 0.25", snapshot.PlatformShareRatio)
	}
	if snapshot.InviteShareRatio != 0.75 {
		t.Fatalf("invite ratio = %v, want 0.75", snapshot.InviteShareRatio)
	}
}

func TestBuildAccountShareModeBillingSnapshotSkipsOwnerSelfUse(t *testing.T) {
	snapshot := BuildAccountShareModeBillingSnapshot(
		&AccountShareMembership{ID: 1, AccountID: 10, ConsumerUserID: 40, APIKeyID: 30},
		&AccountShareListing{ID: 2, AccountID: 10, OwnerUserID: 40, RateMultiplier: 1, HourlyRate: 0.2},
		&AccountSharePolicy{ID: 9, Version: 2, OwnerShareRatio: 0.9, InviteShareRatio: 0.1},
		1.25,
		0,
		100,
	)
	if snapshot != nil {
		t.Fatalf("expected owner self-use snapshot to be skipped, got %#v", snapshot)
	}
}

func TestAccountShareModeResolveOwnerSelfUseMultiplierReadsGlobalSetting(t *testing.T) {
	settingRepo := &accountShareReviewSettingRepoStub{values: map[string]string{
		SettingKeyUserPrivateGroupCommissionRate: "0.0075",
	}}
	svc := &AccountShareModeService{
		settingService: NewSettingService(settingRepo, &config.Config{}),
	}

	multiplier, err := svc.ResolveOwnerSelfUseMultiplier(context.Background())

	if err != nil {
		t.Fatalf("ResolveOwnerSelfUseMultiplier failed: %v", err)
	}
	if multiplier != 0.0075 {
		t.Fatalf("multiplier = %v, want 0.0075", multiplier)
	}
}

func accountShareModeInt64Ptr(v int64) *int64 {
	return &v
}

func newAccountShareRecommendationTestService(repo *accountShareModeRepoStub, apiKeyRepo *accountShareRecommendationAPIKeyRepoStub) *AccountShareModeService {
	billingService := NewBillingService(&config.Config{}, nil)
	return &AccountShareModeService{
		repo:                 repo,
		apiKeyRepo:           apiKeyRepo,
		billingService:       billingService,
		modelPricingResolver: NewModelPricingResolver(nil, billingService),
	}
}

func accountShareModeBoolPtr(v bool) *bool {
	return &v
}

func accountShareTestContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func accountShareRecommendationTestCandidate(id int64, totalCost, overallScore, stabilityScore, availabilityScore, riskControlScore float64) AccountShareRecommendationCandidate {
	return AccountShareRecommendationCandidate{
		Listing: AccountShareListing{
			ID:        id,
			AccountID: id,
		},
		Estimate: AccountShareRecommendationEstimate{
			TotalCost:     totalCost,
			RequestCost:   totalCost,
			HourlyNetCost: 0,
		},
		Score: overallScore,
		ScoreBreakdown: AccountShareRecommendationScoreBreakdown{
			CostSavingScore:   100 - totalCost,
			StabilityScore:    stabilityScore,
			AvailabilityScore: availabilityScore,
			RiskControlScore:  riskControlScore,
			OverallScore:      overallScore,
		},
	}
}

func accountShareRecommendationTestContainsListing(candidates []AccountShareRecommendationCandidate, listingID int64) bool {
	for _, candidate := range candidates {
		if candidate.Listing.ID == listingID {
			return true
		}
	}
	return false
}

func accountShareRecommendationTestListingIDs(candidates []AccountShareRecommendationCandidate) []int64 {
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.Listing.ID)
	}
	return ids
}
