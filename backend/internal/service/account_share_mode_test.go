package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	activationErr        error
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
	idleEndCalls         int
	idleEndMembership    *AccountShareMembership
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
	waiverBacklogQueue   []*AccountShareSeatWaiverBatch
	waiverBacklogCursors [][2]any
	waiverLateCalls      int
	waiverLateQueue      []*AccountShareSeatWaiverBatch
	waiverLateUsageSince []time.Time
	recoverableIDs       []int64
	recoverableSuspend   *AccountShareMembership
	recoverableCalls     int
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
	bindingMemberships   []AccountShareMembership
	bindingErr           error
	bindingConsumerID    int64
	bindingAPIKeyID      int64
	bindingStatusCalls   int
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

func (r *accountShareBillingLifecycleRepoStub) ClearRoomMembersForDrain(
	context.Context,
	int64,
	bool,
	int64,
) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareBillingLifecycleRepoStub) FinalizeDrainingRoom(
	context.Context,
	int64,
	int64,
) (*AccountShareListing, error) {
	return nil, ErrServiceUnavailable
}

func (r *accountShareBillingLifecycleRepoStub) ListOpenRoomLifecycleListingIDs(
	context.Context,
	int64,
	int,
) ([]int64, error) {
	r.lifecycleCalls++
	return nil, nil
}

func (r *accountShareBillingLifecycleRepoStub) FindRoomDeleteOperation(
	context.Context,
	int64,
	bool,
	int64,
	string,
) (*AccountShareRoomOperation, error) {
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
	idempotentListing         *AccountShareListing
	idempotentErr             error
	idempotentCalls           int
	idempotentOwnerUserID     int64
	idempotentAccountID       int64
	idempotentKey             string
	idempotentListingSnapshot *AccountShareListing
	roomAccountsViewerUserID  int64
	roomAccountsViewerIsAdmin bool
	roomAccountsListingID     int64
	roomAccounts              []AccountShareRoomAccount
	roomAccountsErr           error
	attachBatchInput          BatchAccountShareRoomAccountsInput
	attachBatchCalls          int
	attachBatchResult         *BulkUpdateAccountsResult
	attachBatchErr            error
	detachBatchInput          BatchAccountShareRoomAccountsInput
	detachBatchCalls          int
	detachBatchBilling        *AccountShareSeatBillingResult
	detachBatchErr            error
	createRoomCalls           int
	createRoomInput           CreateAccountShareRoomInput
	createRoomListing         *AccountShareListing
	createRoomErr             error
}

type accountShareVisibilityRuntimeRepoStub struct {
	*accountShareModeRepoStub
	visibleListing       *AccountShareListing
	visibleErr           error
	visibleCalls         int
	visibleListingID     int64
	visibleViewerUserID  int64
	visibleViewerIsAdmin bool
	runtimeAccounts      map[int64][]AccountWithConcurrency
	runtimeErr           error
	runtimeCalls         int
	runtimeListingIDs    []int64
}

func (r *accountShareVisibilityRuntimeRepoStub) GetVisibleListingByID(
	_ context.Context,
	listingID int64,
	viewerUserID int64,
	viewerIsAdmin bool,
) (*AccountShareListing, error) {
	r.visibleCalls++
	r.visibleListingID = listingID
	r.visibleViewerUserID = viewerUserID
	r.visibleViewerIsAdmin = viewerIsAdmin
	if r.visibleErr != nil {
		return nil, r.visibleErr
	}
	if r.visibleListing == nil {
		return nil, ErrAccountShareListingNotFound
	}
	listing := *r.visibleListing
	return &listing, nil
}

func (r *accountShareVisibilityRuntimeRepoStub) ListRoomRuntimeAccounts(
	_ context.Context,
	listingIDs []int64,
	_ time.Time,
) (map[int64][]AccountWithConcurrency, error) {
	r.runtimeCalls++
	r.runtimeListingIDs = append([]int64(nil), listingIDs...)
	if r.runtimeErr != nil {
		return nil, r.runtimeErr
	}
	return r.runtimeAccounts, nil
}

type accountShareRuntimeLoadCacheStub struct {
	ConcurrencyCache
	loads    map[int64]*AccountLoadInfo
	err      error
	calls    int
	accounts []AccountWithConcurrency
}

func (c *accountShareRuntimeLoadCacheStub) GetAccountsLoadBatch(
	_ context.Context,
	accounts []AccountWithConcurrency,
) (map[int64]*AccountLoadInfo, error) {
	c.calls++
	c.accounts = append([]AccountWithConcurrency(nil), accounts...)
	if c.err != nil {
		return nil, c.err
	}
	return c.loads, nil
}

func (r *accountShareRoomRepoStub) FindRoomCreationByIdempotency(
	_ context.Context,
	ownerUserID, accountID int64,
	idempotencyKey string,
	listing *AccountShareListing,
) (*AccountShareListing, error) {
	r.idempotentCalls++
	r.idempotentOwnerUserID = ownerUserID
	r.idempotentAccountID = accountID
	r.idempotentKey = idempotencyKey
	if listing != nil {
		snapshot := *listing
		snapshot.AllowedModels = append([]string(nil), listing.AllowedModels...)
		r.idempotentListingSnapshot = &snapshot
	}
	if r.idempotentErr != nil {
		return nil, r.idempotentErr
	}
	if r.idempotentListing == nil {
		return nil, nil
	}
	result := *r.idempotentListing
	result.AllowedModels = append([]string(nil), r.idempotentListing.AllowedModels...)
	return &result, nil
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
) (*BulkUpdateAccountsResult, error) {
	r.attachBatchCalls++
	r.attachBatchInput = input
	if r.attachBatchErr != nil {
		return nil, r.attachBatchErr
	}
	if r.attachBatchResult != nil {
		return r.attachBatchResult, nil
	}
	result := &BulkUpdateAccountsResult{
		Success:    len(input.AccountIDs),
		SuccessIDs: append([]int64(nil), input.AccountIDs...),
		FailedIDs:  []int64{},
		Results:    make([]BulkUpdateAccountResult, 0, len(input.AccountIDs)),
	}
	for _, accountID := range input.AccountIDs {
		result.Results = append(result.Results, BulkUpdateAccountResult{AccountID: accountID, Success: true})
	}
	return result, nil
}

func (r *accountShareRoomRepoStub) DetachRoomAccountsAtomic(
	_ context.Context,
	input BatchAccountShareRoomAccountsInput,
) (*AccountShareSeatBillingResult, error) {
	r.detachBatchCalls++
	r.detachBatchInput = input
	return r.detachBatchBilling, r.detachBatchErr
}

func (r *accountShareRoomRepoStub) CreateRoomFromOwnedAccount(
	_ context.Context,
	_ int64, _ int64, _ int64, _ string,
	listing *AccountShareListing,
) (*AccountShareListing, error) {
	r.createRoomCalls++
	if r.createRoomErr != nil {
		return nil, r.createRoomErr
	}
	if r.createRoomListing != nil {
		result := *r.createRoomListing
		result.AllowedModels = append([]string(nil), r.createRoomListing.AllowedModels...)
		return &result, nil
	}
	if listing != nil {
		result := *listing
		result.AllowedModels = append([]string(nil), listing.AllowedModels...)
		return &result, nil
	}
	return nil, nil
}

func (r *accountShareRoomRepoStub) BeginExternalPlacementDrain(context.Context, int64, int64) (bool, error) {
	return true, nil
}

func (r *accountShareRoomRepoStub) RestoreExternalPlacementAfterDrain(context.Context, int64, int64) error {
	return nil
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
	platform  string
	model     string
	startTime time.Time
	endTime   time.Time
}

func (s *accountShareRecommendationUsageProfileRepoStub) GetAccountShareRecommendationUsageProfile(_ context.Context, userID int64, platform, model string, startTime, endTime time.Time) (*AccountShareRecommendationUsageProfileStats, error) {
	s.calls++
	s.userID = userID
	s.platform = platform
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

func (r *accountShareModeProxyRepoStub) GetVisibleByID(_ context.Context, scope ProxyScope, id int64) (*Proxy, error) {
	r.getVisibleUserID = scope.OwnerUserID
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

func (r *accountShareModeProxyRepoStub) ListActiveVisibleWithAccountCount(context.Context, ProxyScope) ([]ProxyWithAccountCount, error) {
	if r.proxy != nil {
		return []ProxyWithAccountCount{{Proxy: *r.proxy}}, nil
	}
	return []ProxyWithAccountCount{}, nil
}

func (r *accountShareModeProxyRepoStub) FindVisibleActiveByEndpoint(context.Context, ProxyScope, string, string, int, string, string) (*Proxy, error) {
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

func TestAttachRoomAccountsOrdersPartialRepositoryResultByRequest(t *testing.T) {
	repo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		attachBatchResult: &BulkUpdateAccountsResult{
			Success:    1,
			Failed:     1,
			SuccessIDs: []int64{11},
			FailedIDs:  []int64{10},
			Results: []BulkUpdateAccountResult{
				{
					AccountID: 10,
					Success:   false,
					Error:     ErrAccountShareAccountUnavailable.Message,
					Reason:    ErrAccountShareAccountUnavailable.Reason,
					Message:   ErrAccountShareAccountUnavailable.Message,
					Metadata:  map[string]string{"blocker": "overloaded"},
				},
				{AccountID: 11, Success: true},
			},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	result, err := svc.AttachRoomAccounts(context.Background(), BatchAccountShareRoomAccountsInput{
		ListingID:      700,
		AccountIDs:     []int64{11, 10, 11},
		OwnerUserID:    42,
		IdempotencyKey: "attach-partial",
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Equal(t, 1, result.Failed)
	require.Equal(t, []int64{11}, result.SuccessIDs)
	require.Equal(t, []int64{10}, result.FailedIDs)
	require.Equal(t, []int64{11, 10}, []int64{result.Results[0].AccountID, result.Results[1].AccountID})
	require.True(t, result.Results[0].Success)
	require.False(t, result.Results[1].Success)
	require.Equal(t, "ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE", result.Results[1].Reason)
	require.Equal(t, "overloaded", result.Results[1].Metadata["blocker"])
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
	svc.concurrencyService = NewConcurrencyService(&accountShareLifecycleConcurrencyCacheStub{
		counts: map[int64]int{},
	})

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

func TestCreateRoomFromOwnedAccountRejectsWhitespaceOnlyRoomName(t *testing.T) {
	svc := NewAccountShareModeService(nil, nil, nil, nil, nil, nil)

	listing, err := svc.CreateRoomFromOwnedAccount(
		context.Background(),
		42,
		CreateAccountShareRoomInput{
			AccountID:      70,
			IdempotencyKey: "create-room-empty-name",
			RoomName:       " \t\r\n ",
			SeatLimit:      1,
		},
	)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareModeInvalidName)
}

// 公共号池账号在途请求 > 0 时也能建房间。修复前 ensureAccountExternalPlacementIdle
// 会以 ACCOUNT_EXTERNAL_PLACEMENT_BUSY 拒绝——公共号池账号被公共调度占用时并发几乎
// 恒 > 0，用户永远无法从广场把热门号建为房间。建房间与入房一致都是收敛性操作
// （repo 层 CreateRoomFromOwnedAccount 同一事务原子改写 placement 与房间绑定），
// 等待「归零」既不必要也等不到。
func TestCreateRoomFromOwnedAccountPublicPoolSkipsIdleGuard(t *testing.T) {
	ownerUserID := int64(42)
	roomRepo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
	}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account: &Account{
			ID:           70,
			Name:         "public-pool-account",
			Platform:     PlatformAnthropic,
			AccountLevel: AccountLevelPro,
			OwnerUserID:  &ownerUserID,
			Status:       StatusActive,
			Schedulable:  true,
			Concurrency:  5,
			ShareMode:    AccountShareModePublic,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"claude-sonnet-4-20250514": "claude-sonnet-4-20250514"},
			},
			ExternalPlacement: &AccountExternalPlacement{
				Target: AccountExternalPlacementPublicPool,
				State:  "active",
			},
		},
	}
	svc := NewAccountShareModeService(roomRepo, accountRepo, nil, nil, nil, nil)
	// 模拟账号正被公共调度：Redis 槽位里有在途请求。修复前这里会 ErrAccountExternalPlacementBusy。
	svc.SetRuntimeDependencies(
		&ConcurrencyService{cache: &accountShareRuntimeLoadCacheStub{loads: map[int64]*AccountLoadInfo{
			70: {AccountID: 70, CurrentConcurrency: 3, WaitingCount: 0},
		}}},
		nil,
		nil,
		nil,
	)

	listing, err := svc.CreateRoomFromOwnedAccount(
		context.Background(),
		ownerUserID,
		CreateAccountShareRoomInput{
			AccountID:          70,
			IdempotencyKey:     "create-room-public-pool-inflight",
			RoomName:           "room-a",
			SeatLimit:          1,
			RateMultiplier:     1,
			AllowedModels:      []string{"claude-sonnet-4-20250514"},
			PerUserConcurrency: 1,
		},
	)

	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Equal(t, 1, roomRepo.createRoomCalls)
}

func TestCreateRoomFromOwnedAccountReplaysBeforeCurrentAccountAvailabilityChecks(t *testing.T) {
	ownerUserID := int64(42)
	replayed := &AccountShareListing{
		ID:                 700,
		AccountID:          70,
		OwnerUserID:        ownerUserID,
		Platform:           PlatformAnthropic,
		RoomName:           "room-a",
		SeatLimit:          1,
		RateMultiplier:     1,
		AllowedModels:      []string{"claude-sonnet-4-20250514"},
		PerUserConcurrency: 1,
		AccountSampleScope: AccountShareAccountSampleScopeRepresentative,
	}
	roomRepo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		idempotentListing:        replayed,
	}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account: &Account{
			ID:           70,
			Name:         "owned-account",
			Platform:     PlatformAnthropic,
			AccountLevel: AccountLevelUnknown,
			OwnerUserID:  &ownerUserID,
			Status:       StatusDisabled,
			Schedulable:  false,
			Concurrency:  5,
		},
	}
	svc := NewAccountShareModeService(roomRepo, accountRepo, nil, nil, nil, nil)

	listing, err := svc.CreateRoomFromOwnedAccount(
		context.Background(),
		ownerUserID,
		CreateAccountShareRoomInput{
			AccountID:          70,
			IdempotencyKey:     "create-room-replay",
			RoomName:           "room-a",
			SeatLimit:          1,
			RateMultiplier:     1,
			AllowedModels:      []string{"claude-sonnet-4-20250514"},
			PerUserConcurrency: 1,
		},
	)

	require.NoError(t, err)
	require.Equal(t, replayed, listing)
	require.Equal(t, 1, accountRepo.calls)
	require.Equal(t, 1, roomRepo.idempotentCalls)
	require.Equal(t, ownerUserID, roomRepo.idempotentOwnerUserID)
	require.Equal(t, int64(70), roomRepo.idempotentAccountID)
	require.Equal(t, "create-room-replay", roomRepo.idempotentKey)
	require.Empty(t, roomRepo.modeGroupEnsureCalls)
}

func TestCreateRoomFromOwnedAccountRejectsDynamicallyUnavailableAccount(t *testing.T) {
	ownerUserID := int64(42)
	expiredAt := time.Now().UTC().Add(-time.Minute)
	modeRepo := &accountShareModeRepoStub{}
	roomRepo := &accountShareRoomRepoStub{
		accountShareModeRepoStub: modeRepo,
	}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account: &Account{
			ID:                 70,
			Name:               "expired-owned-account",
			Platform:           PlatformAnthropic,
			AccountLevel:       AccountLevelPro,
			OwnerUserID:        &ownerUserID,
			Status:             StatusActive,
			Schedulable:        true,
			Concurrency:        5,
			AutoPauseOnExpired: true,
			ExpiresAt:          &expiredAt,
		},
	}
	svc := NewAccountShareModeService(roomRepo, accountRepo, nil, nil, nil, nil)

	listing, err := svc.CreateRoomFromOwnedAccount(
		context.Background(),
		ownerUserID,
		CreateAccountShareRoomInput{
			AccountID:          70,
			IdempotencyKey:     "create-room-expired-account",
			RoomName:           "room-a",
			SeatLimit:          1,
			RateMultiplier:     1,
			AllowedModels:      []string{"claude-sonnet-4-20250514"},
			PerUserConcurrency: 1,
		},
	)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareAccountUnavailable)
	require.Equal(t, 1, accountRepo.calls)
	require.Equal(t, 1, roomRepo.idempotentCalls)
	require.Empty(t, modeRepo.modeGroupEnsureCalls)
}

func TestGetVisibleListingForwardsViewerRoleToVisibilityRepository(t *testing.T) {
	repo := &accountShareVisibilityRuntimeRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		visibleListing: &AccountShareListing{
			ID:       700,
			Platform: PlatformAnthropic,
			Status:   AccountShareListingStatusPaused,
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	listing, err := svc.GetVisibleListing(context.Background(), 42, true, 700)

	require.NoError(t, err)
	require.NotNil(t, listing)
	require.Equal(t, 1, repo.visibleCalls)
	require.Equal(t, int64(700), repo.visibleListingID)
	require.Equal(t, int64(42), repo.visibleViewerUserID)
	require.True(t, repo.visibleViewerIsAdmin)
}

func TestGetVisibleListingHidesRepresentativeAccountFromPublicViewer(t *testing.T) {
	identityID := int64(99)
	proxyID := int64(88)
	repo := &accountShareVisibilityRuntimeRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		visibleListing: &AccountShareListing{
			ID:                700,
			OwnerUserID:       7,
			AccountID:         70,
			AccountName:       "底层账号",
			AccountIdentityID: &identityID,
			Accounts:          []AccountShareRoomAccount{{AccountID: 70, AccountName: "底层账号"}},
			ProxyID:           &proxyID,
			Proxy:             &AccountShareListingProxy{ID: proxyID},
			AccountStatus:     StatusActive,
			Platform:          PlatformAnthropic,
			Status:            AccountShareListingStatusActive,
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	listing, err := svc.GetVisibleListing(context.Background(), 42, false, 700)

	require.NoError(t, err)
	require.Zero(t, listing.AccountID)
	require.Empty(t, listing.AccountName)
	require.Nil(t, listing.AccountIdentityID)
	require.Empty(t, listing.Accounts)
	require.Nil(t, listing.ProxyID)
	require.Nil(t, listing.Proxy)
	require.Equal(t, StatusActive, listing.AccountStatus)
	require.Equal(t, AccountShareAccountSampleScopeRepresentative, listing.AccountSampleScope)

	payload, err := json.Marshal(listing)
	require.NoError(t, err)
	var publicView map[string]any
	require.NoError(t, json.Unmarshal(payload, &publicView))
	require.NotContains(t, publicView, "account_id")
}

func TestEnrichListingsRuntimeAggregatesEveryRoomAccount(t *testing.T) {
	repo := &accountShareVisibilityRuntimeRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		runtimeAccounts: map[int64][]AccountWithConcurrency{
			700: {
				{ID: 10, MaxConcurrency: 5},
				{ID: 11, MaxConcurrency: 7},
			},
			701: {
				{ID: 12, MaxConcurrency: 3},
			},
		},
	}
	cache := &accountShareRuntimeLoadCacheStub{
		loads: map[int64]*AccountLoadInfo{
			10: {AccountID: 10, CurrentConcurrency: 2},
			11: {AccountID: 11, CurrentConcurrency: 4},
			12: {AccountID: 12, CurrentConcurrency: 1},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetRuntimeDependencies(&ConcurrencyService{cache: cache}, nil, nil, nil)
	listings := []AccountShareListing{
		{ID: 700, AccountID: 10, AccountConcurrency: 5},
		{ID: 701, AccountID: 12, AccountConcurrency: 3},
	}

	svc.enrichListingsRuntime(context.Background(), listings)

	require.Equal(t, 1, repo.runtimeCalls)
	require.ElementsMatch(t, []int64{700, 701}, repo.runtimeListingIDs)
	require.Equal(t, 1, cache.calls)
	require.ElementsMatch(t, []AccountWithConcurrency{
		{ID: 10, MaxConcurrency: 5},
		{ID: 11, MaxConcurrency: 7},
		{ID: 12, MaxConcurrency: 3},
	}, cache.accounts)
	require.Equal(t, 12, listings[0].AccountConcurrency)
	require.Equal(t, 6, listings[0].CurrentConcurrency)
	require.True(t, listings[0].RuntimeLoadKnown)
	require.Equal(t, 3, listings[1].AccountConcurrency)
	require.Equal(t, 1, listings[1].CurrentConcurrency)
	require.True(t, listings[1].RuntimeLoadKnown)
}

func TestEnrichListingsRuntimeLeavesLoadUnknownWhenAnyRoomAccountIsMissing(t *testing.T) {
	repo := &accountShareVisibilityRuntimeRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		runtimeAccounts: map[int64][]AccountWithConcurrency{
			700: {
				{ID: 10, MaxConcurrency: 5},
				{ID: 11, MaxConcurrency: 7},
			},
		},
	}
	cache := &accountShareRuntimeLoadCacheStub{
		loads: map[int64]*AccountLoadInfo{
			10: {AccountID: 10, CurrentConcurrency: 2},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.SetRuntimeDependencies(&ConcurrencyService{cache: cache}, nil, nil, nil)
	listings := []AccountShareListing{{
		ID:                 700,
		AccountID:          10,
		AccountConcurrency: 5,
	}}

	svc.enrichListingsRuntime(context.Background(), listings)

	require.False(t, listings[0].RuntimeLoadKnown)
	require.Zero(t, listings[0].CurrentConcurrency)
	require.Equal(t, 5, listings[0].AccountConcurrency)
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

func TestCreateRoomFromOwnedAccountRejectsUnsupportedPlatformBeforeRuntimeMutation(t *testing.T) {
	ownerUserID := int64(42)
	modeRepo := &accountShareModeRepoStub{}
	roomRepo := &accountShareRoomRepoStub{accountShareModeRepoStub: modeRepo}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account: &Account{
			ID:           70,
			Name:         "unsupported-owned-account",
			Platform:     PlatformGrok,
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
			IdempotencyKey:     "unsupported-room-platform",
			RoomName:           "room-a",
			SeatLimit:          1,
			RateMultiplier:     1,
			AllowedModels:      []string{"grok-4"},
			PerUserConcurrency: 1,
		},
	)

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountPlatformUnsupported)
	require.Equal(t, 1, accountRepo.calls)
	require.Equal(t, 1, roomRepo.idempotentCalls)
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

func (r *accountShareModeRepoStub) BeginReviewModerationAttempt(context.Context, int64, int) (bool, error) {
	return true, nil
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

func (r *accountShareModeRepoStub) ListAPIKeyBindingMemberships(_ context.Context, consumerUserID int64, apiKeyID int64) ([]AccountShareMembership, error) {
	r.bindingStatusCalls++
	r.bindingConsumerID = consumerUserID
	r.bindingAPIKeyID = apiKeyID
	if r.bindingErr != nil {
		return nil, r.bindingErr
	}
	return append([]AccountShareMembership(nil), r.bindingMemberships...), nil
}

func TestAccountShareModeGetAPIKeyBindingStatusCountsEveryBlockingState(t *testing.T) {
	repo := &accountShareModeRepoStub{
		bindingMemberships: []AccountShareMembership{
			{ID: 1, APIKeyID: 42, Status: AccountShareMembershipStatusActive},
			{ID: 2, APIKeyID: 42, Status: AccountShareMembershipStatusQueued},
			{
				ID:                    3,
				APIKeyID:              42,
				Status:                AccountShareMembershipStatusEnding,
				SettlementStatus:      "pending",
				EndingOperationID:     "00000000-0000-4000-8000-000000000003",
				EndingOperationStatus: "needs_attention",
			},
		},
	}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 42, UserID: 7},
	}
	svc := &AccountShareModeService{repo: repo, apiKeyRepo: apiKeyRepo}

	status, err := svc.GetAPIKeyBindingStatus(context.Background(), 7, 42)

	require.NoError(t, err)
	require.Equal(t, int64(42), status.APIKeyID)
	require.Equal(t, 1, status.ActiveCount)
	require.Equal(t, 1, status.QueuedCount)
	require.Equal(t, 1, status.EndingCount)
	require.Equal(t, 3, status.BlockingCount)
	require.Len(t, status.Memberships, 3)
	require.Equal(t, "pending", status.Memberships[2].SettlementStatus)
	require.Equal(t, "needs_attention", status.Memberships[2].EndingOperationStatus)
	require.Equal(t, 1, repo.bindingStatusCalls)
	require.Equal(t, int64(7), repo.bindingConsumerID)
	require.Equal(t, int64(42), repo.bindingAPIKeyID)
}

func TestAccountShareModeGetAPIKeyBindingStatusRejectsForeignAPIKey(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	apiKeyRepo := &accountShareRecommendationAPIKeyRepoStub{
		key: &APIKey{ID: 42, UserID: 8},
	}
	svc := &AccountShareModeService{repo: repo, apiKeyRepo: apiKeyRepo}

	status, err := svc.GetAPIKeyBindingStatus(context.Background(), 7, 42)

	require.Nil(t, status)
	require.ErrorIs(t, err, ErrInsufficientPerms)
	require.Zero(t, repo.bindingStatusCalls)
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
	r.idleEndCalls++
	if r.idleEndMembership != nil {
		membership := r.idleEndMembership
		r.membership = nil
		r.listing = nil
		return membership, accountShareModeStubBillingResult(membership), nil
	}
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
	if r.recoverableSuspend != nil && r.membership != nil && r.recoverableSuspend.ID == r.membership.ID {
		r.membership = nil
		r.listing = nil
	}
	return r.recoverableSuspend, accountShareModeStubBillingResult(r.recoverableSuspend), nil
}

func (r *accountShareModeRepoStub) DisablePermanentlyUnavailableListings(context.Context, time.Time, int) (*AccountShareListingMaintenanceResult, error) {
	return &AccountShareListingMaintenanceResult{}, nil
}

func (r *accountShareModeRepoStub) EndUnavailableAccountMemberships(context.Context, int64, time.Time, int) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{EndedConsumerUserIDs: []int64{20}}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatBilling(context.Context, time.Time, int) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatWaiverBacklogCompensations(_ context.Context, _ time.Time, limit int, cursorPeriodEndedAt time.Time, cursorID int64) (*AccountShareSeatWaiverBatch, error) {
	r.waiverCompCalls++
	r.waiverCompLimit = limit
	r.waiverBacklogCursors = append(r.waiverBacklogCursors, [2]any{cursorPeriodEndedAt, cursorID})
	if len(r.waiverBacklogQueue) > 0 {
		batch := r.waiverBacklogQueue[0]
		r.waiverBacklogQueue = r.waiverBacklogQueue[1:]
		return batch, nil
	}
	return &AccountShareSeatWaiverBatch{Billing: &AccountShareSeatBillingResult{}}, nil
}

func (r *accountShareModeRepoStub) ProcessSeatWaiverLateUsageCompensations(_ context.Context, _ time.Time, limit int, usageSince, _ time.Time, _ time.Time, _ int64) (*AccountShareSeatWaiverBatch, error) {
	r.waiverLateCalls++
	r.waiverCompLimit = limit
	r.waiverLateUsageSince = append(r.waiverLateUsageSince, usageSince)
	if len(r.waiverLateQueue) > 0 {
		batch := r.waiverLateQueue[0]
		r.waiverLateQueue = r.waiverLateQueue[1:]
		return batch, nil
	}
	return &AccountShareSeatWaiverBatch{Billing: &AccountShareSeatBillingResult{}}, nil
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
	if r.activationErr != nil {
		return nil, nil, r.activationErr
	}
	if len(r.bindingResults) > 0 {
		result := r.bindingResults[0]
		r.bindingResults = r.bindingResults[1:]
		return result.membership, result.listing, result.err
	}
	return nil, nil, ErrAccountShareListingNotFound
}

type accountShareModeRebindRepoStub struct {
	*accountShareModeRepoStub
	AccountShareRoomRepository
	rebindCalls       int
	rebindToAccountID int64
}

func (r *accountShareModeRebindRepoStub) RebindMembershipToHealthyRoomAccount(
	_ context.Context,
	membershipID int64,
	currentAccountID int64,
	_ time.Time,
) (bool, error) {
	r.rebindCalls++
	if r.accountShareModeRepoStub == nil ||
		r.membership == nil ||
		r.listing == nil ||
		r.membership.ID != membershipID ||
		r.membership.AccountID != currentAccountID {
		return false, ErrAccountShareListingNotFound
	}
	r.membership.AccountID = r.rebindToAccountID
	r.listing.AccountID = r.rebindToAccountID
	r.listing.RepresentativeAccountConcurrency = 5
	return true, nil
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

func TestAccountShareModeSeatBillingDoesNotRunRoomLifecycle(t *testing.T) {
	baseRepo := &accountShareModeRepoStub{}
	repo := &accountShareBillingLifecycleRepoStub{AccountShareModeRepository: baseRepo}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)

	err := svc.processSeatBillingOnceLeased(context.Background(), &ClusterLeaseGuard{})

	require.NoError(t, err)
	require.Equal(t, 1, repo.endingCalls)
	require.Zero(t, repo.lifecycleCalls)
}

func TestAccountShareModeRoomLifecycleFinalizerRunsIndependentlyFromSeatBilling(t *testing.T) {
	baseRepo := &accountShareModeRepoStub{}
	repo := &accountShareBillingLifecycleRepoStub{AccountShareModeRepository: baseRepo}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.taskExecutor = &ClusterTaskExecutor{}

	svc.processRoomLifecycleFinalizationOnce()

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
		t.Fatalf("expected one waiver backlog pass, got %d", repo.waiverCompCalls)
	}
	if repo.waiverLateCalls != 1 {
		t.Fatalf("expected one late usage pass, got %d", repo.waiverLateCalls)
	}
	if repo.waiverCompLimit != AccountShareModeSeatWaiverCompensationBatchSize {
		t.Fatalf("waiver compensation limit = %d, want %d", repo.waiverCompLimit, AccountShareModeSeatWaiverCompensationBatchSize)
	}
	if svc.seatWaiverLateUsageHWM.IsZero() {
		t.Fatal("expected late usage HWM to advance after drained round")
	}
}

func TestAccountShareModeSeatWaiverBacklogLoopsWithCursorUntilDrained(t *testing.T) {
	batch := AccountShareModeSeatWaiverCompensationBatchSize
	repo := &accountShareModeRepoStub{
		waiverBacklogQueue: []*AccountShareSeatWaiverBatch{
			{Billing: &AccountShareSeatBillingResult{Processed: batch}, Matched: batch, CursorPeriodEndedAt: time.Unix(1000, 0).UTC(), CursorID: 42},
			{Billing: &AccountShareSeatBillingResult{Processed: 3}, Matched: 3},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.taskExecutor = &ClusterTaskExecutor{}

	svc.processSeatWaiverCompensationsOnce()

	if repo.waiverCompCalls != 2 {
		t.Fatalf("expected backlog loop to run twice, got %d", repo.waiverCompCalls)
	}
	if len(repo.waiverBacklogCursors) != 2 {
		t.Fatalf("expected cursor recorded per call, got %d", len(repo.waiverBacklogCursors))
	}
	first, second := repo.waiverBacklogCursors[0], repo.waiverBacklogCursors[1]
	firstTime, firstTimeOK := first[0].(time.Time)
	firstID, firstIDOK := first[1].(int64)
	if !firstTimeOK || !firstIDOK || !firstTime.IsZero() || firstID != 0 {
		t.Fatalf("first backlog call should start without cursor, got %#v", first)
	}
	secondTime, secondTimeOK := second[0].(time.Time)
	secondID, secondIDOK := second[1].(int64)
	if !secondTimeOK || !secondIDOK || !secondTime.Equal(time.Unix(1000, 0).UTC()) || secondID != 42 {
		t.Fatalf("second backlog call should resume from batch cursor, got %#v", second)
	}
	if repo.waiverLateCalls != 1 {
		t.Fatalf("late usage pass should run once after backlog drained, got %d", repo.waiverLateCalls)
	}
}

func TestAccountShareModeSeatWaiverHWMNarrowsLateUsageWindow(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.taskExecutor = &ClusterTaskExecutor{}

	svc.processSeatWaiverCompensationsOnce()
	firstHWM := svc.seatWaiverLateUsageHWM
	svc.processSeatWaiverCompensationsOnce()

	if len(repo.waiverLateUsageSince) != 2 {
		t.Fatalf("expected two late usage passes, got %d", len(repo.waiverLateUsageSince))
	}
	lookbackFloor := time.Now().UTC().Add(-AccountShareModeSeatWaiverLateUsageLookback)
	if !repo.waiverLateUsageSince[0].Before(lookbackFloor.Add(time.Minute)) {
		t.Fatalf("first pass should use lookback floor, got %v", repo.waiverLateUsageSince[0])
	}
	if !repo.waiverLateUsageSince[1].Equal(firstHWM) {
		t.Fatalf("second pass should use advanced HWM %v, got %v", firstHWM, repo.waiverLateUsageSince[1])
	}
}

func TestAccountShareModeSeatWaiverLateUsageLoopsUntilDrained(t *testing.T) {
	batch := AccountShareModeSeatWaiverCompensationBatchSize
	repo := &accountShareModeRepoStub{
		waiverLateQueue: []*AccountShareSeatWaiverBatch{
			{Billing: &AccountShareSeatBillingResult{Processed: batch}, Matched: batch, CursorPeriodEndedAt: time.Unix(2000, 0).UTC(), CursorID: 7},
			{Billing: &AccountShareSeatBillingResult{Processed: 1}, Matched: 1},
		},
	}
	svc := NewAccountShareModeService(repo, nil, nil, nil, nil, nil)
	svc.taskExecutor = &ClusterTaskExecutor{}

	svc.processSeatWaiverCompensationsOnce()

	// 第一批满批未排干 → 续扫第二批(未满批,排干)→ HWM 才推进。
	if repo.waiverLateCalls != 2 {
		t.Fatalf("expected late usage loop to run twice, got %d", repo.waiverLateCalls)
	}
	if svc.seatWaiverLateUsageHWM.IsZero() {
		t.Fatal("expected HWM to advance once late usage drained")
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
		PlatformOpencode:  {ID: 303, Platform: PlatformOpencode},
	}}
	svc := &AccountShareModeService{repo: repo}

	groups, err := svc.ListModeGroups(context.Background())
	if err != nil {
		t.Fatalf("list mode groups failed: %v", err)
	}
	if len(groups) != 3 || groups[0].GroupID != 101 || groups[0].Platform != PlatformOpenAI || groups[1].GroupID != 202 || groups[1].Platform != PlatformAnthropic || groups[2].GroupID != 303 || groups[2].Platform != PlatformOpencode {
		t.Fatalf("unexpected mode groups: %#v", groups)
	}
	if len(repo.modeGroupGetCalls) != 3 || repo.modeGroupGetCalls[0] != PlatformOpenAI || repo.modeGroupGetCalls[1] != PlatformAnthropic || repo.modeGroupGetCalls[2] != PlatformOpencode {
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
		repo:               repo,
		proxyRepo:          proxyRepo,
		openaiOAuthService: &OpenAIOAuthService{},
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
		repo:         repo,
		proxyRepo:    proxyRepo,
		oauthService: &OAuthService{},
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

// 普通用户浏览广场（tab=all、未选状态过滤器、未显式 available_only）时，
// 列表默认只返回可用房间（service 层强制 available_only=true），
// 避免不可用的房间（已暂停/账号不可调度/无空位等）刷屏。
func TestAccountShareModeListListingsDefaultsToAvailableOnlyForPublicBrowse(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}

	_, _, err := svc.ListListings(context.Background(), 42, false, AccountShareListingFilters{
		Tab: AccountShareModeListingTabAll,
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if !repo.listFilters.AvailableOnly {
		t.Fatal("expected available_only to default true for public tab=all browse")
	}
	if repo.listFilters.Status != "" {
		t.Fatalf("expected status to stay empty, got %q", repo.listFilters.Status)
	}
}

// 普通用户显式选了「已上架」(status=active 不带 available_only) 时，
// 表示想看全部上架房间（含暂时不可用），不应被强制可用性过滤。
func TestAccountShareModeListListingsKeepsExplicitActiveStatusWithoutAvailableFilter(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}

	_, _, err := svc.ListListings(context.Background(), 42, false, AccountShareListingFilters{
		Tab:    AccountShareModeListingTabAll,
		Status: AccountShareListingStatusActive,
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if repo.listFilters.AvailableOnly {
		t.Fatal("expected available_only to stay false when status=active explicitly requested")
	}
	if repo.listFilters.Status != AccountShareListingStatusActive {
		t.Fatalf("expected status active, got %q", repo.listFilters.Status)
	}
}

// 号主管理视图（tab=mine）即使普通用户身份也保持全量，不被可用性过滤。
func TestAccountShareModeListListingsKeepsMineViewFullForOwner(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}

	_, _, err := svc.ListListings(context.Background(), 42, false, AccountShareListingFilters{
		Tab: AccountShareModeListingTabMine,
	}, pagination.PaginationParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListListings failed: %v", err)
	}
	if repo.listFilters.AvailableOnly {
		t.Fatal("expected available_only to stay false for tab=mine owner management view")
	}
}

func TestAccountShareModeListListingsProjectsSensitiveAccountFieldsByViewer(t *testing.T) {
	identityID := int64(99)
	proxyID := int64(88)
	source := AccountShareListing{
		ID:                700,
		OwnerUserID:       7,
		AccountID:         70,
		AccountName:       "底层账号",
		AccountIdentityID: &identityID,
		Accounts:          []AccountShareRoomAccount{{AccountID: 70, AccountName: "底层账号"}},
		ProxyID:           &proxyID,
		Proxy:             &AccountShareListingProxy{ID: proxyID},
		AccountStatus:     StatusActive,
	}
	for _, test := range []struct {
		name          string
		viewerUserID  int64
		viewerIsAdmin bool
		wantHidden    bool
	}{
		{name: "public", viewerUserID: 42, wantHidden: true},
		{name: "owner", viewerUserID: 7, wantHidden: false},
		{name: "admin", viewerUserID: 42, viewerIsAdmin: true, wantHidden: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &accountShareModeRepoStub{
				listingsByPage: map[int][]AccountShareListing{1: {source}},
			}
			svc := &AccountShareModeService{repo: repo}

			listings, _, err := svc.ListListings(
				context.Background(),
				test.viewerUserID,
				test.viewerIsAdmin,
				AccountShareListingFilters{},
				pagination.PaginationParams{Page: 1, PageSize: 20},
			)

			require.NoError(t, err)
			require.Len(t, listings, 1)
			require.Equal(t, StatusActive, listings[0].AccountStatus)
			if test.wantHidden {
				require.Zero(t, listings[0].AccountID)
				require.Empty(t, listings[0].AccountName)
				require.Nil(t, listings[0].AccountIdentityID)
				require.Empty(t, listings[0].Accounts)
				require.Nil(t, listings[0].ProxyID)
				require.Nil(t, listings[0].Proxy)
			} else {
				require.Equal(t, source.AccountID, listings[0].AccountID)
				require.Equal(t, source.AccountName, listings[0].AccountName)
				require.Equal(t, source.AccountIdentityID, listings[0].AccountIdentityID)
				require.Equal(t, source.ProxyID, listings[0].ProxyID)
				require.Equal(t, source.Proxy, listings[0].Proxy)
			}
		})
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

func TestAccountShareModeRecommendListingsUsesRoomQuotaMaximumForRiskRanking(t *testing.T) {
	high5h := 96.0
	high7d := 91.0
	low5h := 30.0
	low7d := 40.0
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
					RateMultiplier:     1,
					PerUserConcurrency: 5,
					AccountConcurrency: 20,
					Codex5hUsage:       &UsageProgress{Utilization: 5},
					Codex7dUsage:       &UsageProgress{Utilization: 10},
					QuotaSummary: &AccountShareQuotaSummary{
						Scope:         AccountShareQuotaSummaryScopeRoom,
						AttachedCount: 2,
						EligibleCount: 2,
						Window5h: AccountShareQuotaWindowSummary{
							KnownCount:     2,
							MaxUtilization: &high5h,
						},
						Window7d: AccountShareQuotaWindowSummary{
							KnownCount:     2,
							MaxUtilization: &high7d,
						},
					},
				},
				{
					ID:                 2,
					AccountID:          102,
					OwnerUserID:        101,
					Status:             AccountShareListingStatusActive,
					Platform:           PlatformOpenAI,
					AllowedModels:      []string{"gpt-5.4"},
					SeatLimit:          2,
					RateMultiplier:     1,
					PerUserConcurrency: 5,
					AccountConcurrency: 20,
					Codex5hUsage:       &UsageProgress{Utilization: 80},
					Codex7dUsage:       &UsageProgress{Utilization: 85},
					QuotaSummary: &AccountShareQuotaSummary{
						Scope:         AccountShareQuotaSummaryScopeRoom,
						AttachedCount: 2,
						EligibleCount: 2,
						Window5h: AccountShareQuotaWindowSummary{
							KnownCount:     2,
							MaxUtilization: &low5h,
						},
						Window7d: AccountShareQuotaWindowSummary{
							KnownCount:     2,
							MaxUtilization: &low7d,
						},
					},
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
		RequestCount:           1,
		ActiveHours:            1,
		InputTokensPerRequest:  100,
		OutputTokensPerRequest: 50,
		Limit:                  2,
	})

	require.NoError(t, err)
	require.Len(t, got.Items, 2)
	require.Equal(t, int64(2), got.Items[0].Listing.ID)
	require.Greater(
		t,
		got.Items[0].ScoreBreakdown.RiskControlScore,
		got.Items[1].ScoreBreakdown.RiskControlScore,
	)
}

func TestAccountShareModeRecommendListingsExcludesRepresentativeAccountWithZeroConcurrency(t *testing.T) {
	repo := &accountShareModeRepoStub{
		listingsByPage: map[int][]AccountShareListing{
			1: {
				{
					ID:                               1,
					AccountID:                        101,
					OwnerUserID:                      100,
					Status:                           AccountShareListingStatusActive,
					Platform:                         PlatformOpenAI,
					AllowedModels:                    []string{"gpt-5.4"},
					SeatLimit:                        2,
					RateMultiplier:                   1,
					PerUserConcurrency:               1,
					AccountConcurrency:               20,
					RepresentativeAccountConcurrency: 0,
					AccountStatus:                    StatusActive,
					AccountSchedulable:               true,
				},
				{
					ID:                               2,
					AccountID:                        102,
					OwnerUserID:                      101,
					Status:                           AccountShareListingStatusActive,
					Platform:                         PlatformOpenAI,
					AllowedModels:                    []string{"gpt-5.4"},
					SeatLimit:                        2,
					RateMultiplier:                   1,
					PerUserConcurrency:               1,
					AccountConcurrency:               5,
					RepresentativeAccountConcurrency: 5,
					AccountStatus:                    StatusActive,
					AccountSchedulable:               true,
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
		RequestCount:           1,
		ActiveHours:            1,
		InputTokensPerRequest:  100,
		OutputTokensPerRequest: 50,
		Limit:                  5,
	})

	require.NoError(t, err)
	require.Equal(t, 1, got.CandidateCount)
	require.Len(t, got.Items, 1)
	require.Equal(t, int64(2), got.Items[0].Listing.ID)
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
			TotalImageInputTokens:    201,
			TotalImageOutputTokens:   102,
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
	if repo.calls != 1 || repo.userID != 42 || repo.platform != PlatformOpenAI || repo.model != "gpt-5.5" {
		t.Fatalf("unexpected repo call: calls=%d user=%d platform=%q model=%q", repo.calls, repo.userID, repo.platform, repo.model)
	}
	if profile.RequestCount != 34 {
		t.Fatalf("RequestCount = %d, want 34", profile.RequestCount)
	}
	if profile.ActiveHours != 3 {
		t.Fatalf("ActiveHours = %v, want 3", profile.ActiveHours)
	}
	if profile.InputTokensPerRequest != 8 ||
		profile.OutputTokensPerRequest != 3 ||
		profile.CacheCreationTokensPerRequest != 1 ||
		profile.CacheReadTokensPerRequest != 3 ||
		profile.ImageInputTokensPerRequest != 3 ||
		profile.ImageOutputTokensPerRequest != 2 {
		t.Fatalf("unexpected per-request tokens: %#v", profile)
	}
	if !profile.HasHistory || !profile.ModelMatched || profile.UsedModelFallback {
		t.Fatalf("unexpected profile flags: %#v", profile)
	}
	if !profile.EndTime.After(profile.StartTime) {
		t.Fatalf("expected valid time range: start=%s end=%s", profile.StartTime, profile.EndTime)
	}
}

func TestBuildAccountShareQuotaWindowSummaryKeepsPartialAndMaxReset(t *testing.T) {
	firstReset := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	maxReset := firstReset.Add(time.Hour)
	summary := buildAccountShareQuotaWindowSummary(
		[]AccountShareRoomQuotaSnapshot{
			{Window5h: &UsageProgress{Utilization: 20, ResetsAt: &firstReset}},
			{Window5h: &UsageProgress{Utilization: 82, ResetsAt: &maxReset}},
			{},
		},
		3,
		true,
	)

	require.Equal(t, 2, summary.KnownCount)
	require.NotNil(t, summary.MinUtilization)
	require.Equal(t, 20.0, *summary.MinUtilization)
	require.NotNil(t, summary.MaxUtilization)
	require.Equal(t, 82.0, *summary.MaxUtilization)
	require.NotNil(t, summary.AverageUtilization)
	require.Equal(t, 51.0, *summary.AverageUtilization)
	require.NotNil(t, summary.MaxUtilizationResetsAt)
	require.True(t, summary.MaxUtilizationResetsAt.Equal(maxReset))
	require.True(t, summary.Partial)
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

	// 合约字段没带编辑锁时，service 层刻意不再直接拒绝：仓储会先算一遍
	// accountShareListingUpdateProtectsConsumers（只降费 / 提并发 / 加模型 / 不伤现有席位地
	// 减席位即免锁放行），算不过才要求编辑锁。旧的前置判定条件与那条免锁分支的进入条件
	// 逐字相同，等于把整条「消费者安全更新」堵死。裁决权归仓储，见
	// account_share_mode_repo.go 的 contractUpdate / consumerSafeUpdate 分支。
	callsBeforeSessionless := repo.updateCalls
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		AllowedModels:   &models,
		ExpectedVersion: &expectedVersion,
		Reason:          "调整可用模型",
	})
	if err != nil {
		t.Fatalf("expected sessionless contract update to reach the repository, got %v", err)
	}
	if repo.updateCalls != callsBeforeSessionless+1 {
		t.Fatalf("expected sessionless contract update to be forwarded to the repository, calls=%d", repo.updateCalls)
	}
	if strings.TrimSpace(repo.updateInput.EditSessionID) != "" {
		t.Fatalf("expected empty edit session to be forwarded verbatim, got %q", repo.updateInput.EditSessionID)
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
	if repo.updateCalls != callsBeforeSessionless+2 || repo.updateAdmin {
		t.Fatalf("expected one more non-admin repository update, calls=%d admin=%t", repo.updateCalls, repo.updateAdmin)
	}
	got := strings.Join(*repo.updateInput.AllowedModels, ",")
	if got != "gpt-5.5,gpt-5.4" {
		t.Fatalf("normalized models = %q", got)
	}

	callsBeforeName := repo.updateCalls
	name := "共享账号一"
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Name: &name, ExpectedVersion: &expectedVersion})
	if !errors.Is(err, ErrAccountShareUpdateReasonRequired) {
		t.Fatalf("expected room-name update reason to be required, got %v", err)
	}
	if repo.updateCalls != callsBeforeName {
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
	if repo.updateCalls != callsBeforeName+1 {
		t.Fatalf("expected one more repository update, got %d", repo.updateCalls)
	}
	if repo.updateInput.Name == nil || *repo.updateInput.Name != name {
		t.Fatalf("expected trimmed name in update input, got %#v", repo.updateInput.Name)
	}

	callsBeforeStatus := repo.updateCalls
	status := AccountShareListingStatusPaused
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status, ExpectedVersion: &expectedVersion})
	if !errors.Is(err, ErrAccountShareRoomLifecycleCommandRequired) {
		t.Fatalf("expected status PATCH to require a lifecycle command, got %v", err)
	}
	if repo.updateCalls != callsBeforeStatus {
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

func TestAccountShareModeListingConfigRejectsPerUserConcurrencyAboveRoomConcurrency(t *testing.T) {
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
	if !errors.Is(err, ErrAccountShareModeInvalidConcurrency) {
		t.Fatalf("expected per-user concurrency above room concurrency to be rejected, got %v", err)
	}
}

func TestAccountShareModeUpdateListingRejectsPerUserConcurrencyAboveRoomConcurrency(t *testing.T) {
	expectedVersion := int64(1)
	perUserConcurrency := 6
	repo := &accountShareModeRepoStub{
		listing: &AccountShareListing{
			ID:                 7,
			OwnerUserID:        42,
			AccountConcurrency: 5,
			PerUserConcurrency: 1,
		},
		updateListing: &AccountShareListing{ID: 7, OwnerUserID: 42},
	}
	svc := &AccountShareModeService{repo: repo}

	listing, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{
		PerUserConcurrency: &perUserConcurrency,
		EditSessionID:      "edit-session",
		ExpectedVersion:    &expectedVersion,
		Reason:             "调整单用户并发",
	})

	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareModeInvalidConcurrency)
	require.Zero(t, repo.updateCalls)
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

func TestAccountShareModeJoinIntentRejectsAutomaticallyPausedExpiredAccount(t *testing.T) {
	groupID := int64(1)
	expiredAt := time.Now().UTC().Add(-time.Minute)
	repo := &accountShareModeRepoStub{
		listing: &AccountShareListing{
			ID:                                      2,
			AccountID:                               10,
			Platform:                                PlatformOpenAI,
			OwnerUserID:                             42,
			Status:                                  AccountShareListingStatusActive,
			SeatLimit:                               3,
			AccountStatus:                           StatusActive,
			AccountSchedulable:                      true,
			RepresentativeAccountConcurrency:        5,
			RepresentativeAccountAutoPauseOnExpired: true,
			AccountExpiresAt:                        &expiredAt,
		},
	}
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

	_, err := svc.CreateJoinIntent(context.Background(), 1, 2, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})

	require.ErrorIs(t, err, ErrAccountShareAccountUnavailable)
	require.Zero(t, repo.joinInput.ListingID)
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

// 旧房间退出结算中（ending）时加入新房间：该 key 被唯一索引锁定，新加入必然进排队。
// CreateJoinIntent 必须把跨房 ending 纳入 queue_may_be_required，让确认弹窗如实提示
// 「需要预约队列」，避免用户以为可直接加入、提交时才被后端拒绝。
func TestAccountShareModeJoinIntentFlagsQueueWhenOtherRoomEnding(t *testing.T) {
	groupID := int64(1)
	revisionID := int64(91)
	listing := &AccountShareListing{
		ID:                               2,
		RowVersion:                       7,
		CurrentRevisionID:                &revisionID,
		AccountID:                        10,
		RoomName:                         "target-room",
		Platform:                         PlatformOpenAI,
		OwnerUserID:                      42,
		Status:                           AccountShareListingStatusActive,
		SeatLimit:                        3,
		ActiveSeats:                      1,
		AllowedModels:                    []string{"gpt-5.5"},
		PerUserConcurrency:               2,
		HourlyRate:                       0.3,
		MinBalanceRequired:               1,
		AccountStatus:                    StatusActive,
		AccountSchedulable:               true,
		RepresentativeAccountConcurrency: 5,
	}
	revisionTerms := accountShareJoinTermsFromListing(listing, revisionID)
	repo := &accountShareModeRepoStub{
		listing:       listing,
		revisionTerms: &revisionTerms,
		// 同一 key 在「其它房间」（ListingID=999）有退出结算中的 membership。
		bindingMemberships: []AccountShareMembership{
			{ID: 800, ListingID: 999, ConsumerUserID: 1, APIKeyID: 3, Status: AccountShareMembershipStatusEnding},
		},
	}
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

	intent, err := svc.CreateJoinIntent(context.Background(), 1, listing.ID, CreateAccountShareJoinIntentInput{
		APIKeyID:           3,
		IdleTimeoutMinutes: 30,
		AcceptQueue:        true,
	})
	require.NoError(t, err)
	require.True(t, intent.QueueMayBeRequired, "cross-room ending membership must force queue consent")
}

func TestAccountShareModeJoinIntentBindsAcceptedTermsToFinalJoin(t *testing.T) {
	groupID := int64(1)
	revisionID := int64(91)
	listing := &AccountShareListing{
		ID:                               2,
		RowVersion:                       7,
		CurrentRevisionID:                &revisionID,
		AccountID:                        10,
		RoomName:                         "stable-room",
		Platform:                         PlatformOpenAI,
		OwnerUserID:                      42,
		Status:                           AccountShareListingStatusActive,
		SeatLimit:                        3,
		ActiveSeats:                      1,
		RateMultiplier:                   0.75,
		AllowedModels:                    []string{"gpt-5.5"},
		PerUserConcurrency:               2,
		HourlyRate:                       0.3,
		HourlyFeeWaiverMinimum:           0.1,
		MinBalanceRequired:               1,
		CodexCLIOnly:                     true,
		Codex5hLimitPercent:              90,
		Codex7dLimitPercent:              80,
		AccountStatus:                    StatusActive,
		AccountSchedulable:               true,
		RepresentativeAccountConcurrency: 5,
		Anthropic5hLimitPercent:          0,
		Anthropic7dLimitPercent:          0,
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
		ID:                               2,
		RowVersion:                       1,
		AccountID:                        10,
		RoomName:                         "legacy-room",
		Platform:                         PlatformOpenAI,
		OwnerUserID:                      42,
		Status:                           AccountShareListingStatusActive,
		SeatLimit:                        3,
		RateMultiplier:                   0.75,
		AllowedModels:                    []string{"gpt-5.5"},
		PerUserConcurrency:               2,
		HourlyRate:                       0.3,
		HourlyFeeWaiverMinimum:           0.1,
		MinBalanceRequired:               1,
		CodexCLIOnly:                     true,
		Codex5hLimitPercent:              90,
		Codex7dLimitPercent:              80,
		Anthropic5hLimitPercent:          90,
		Anthropic7dLimitPercent:          80,
		AccountStatus:                    StatusActive,
		AccountSchedulable:               true,
		RepresentativeAccountConcurrency: 5,
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
		ID:                               2,
		RowVersion:                       7,
		CurrentRevisionID:                &revisionID,
		AccountID:                        10,
		RoomName:                         "stable-room",
		Platform:                         PlatformOpenAI,
		OwnerUserID:                      42,
		Status:                           AccountShareListingStatusActive,
		SeatLimit:                        3,
		RateMultiplier:                   0.75,
		AllowedModels:                    []string{"gpt-5.5"},
		PerUserConcurrency:               2,
		HourlyRate:                       0.3,
		MinBalanceRequired:               1,
		AccountStatus:                    StatusActive,
		AccountSchedulable:               true,
		RepresentativeAccountConcurrency: 5,
		Codex5hLimitPercent:              90,
		Codex7dLimitPercent:              80,
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
		ID:                               2,
		RowVersion:                       7,
		CurrentRevisionID:                &revisionID,
		AccountID:                        10,
		RoomName:                         "stable-room",
		Platform:                         PlatformOpenAI,
		OwnerUserID:                      42,
		Status:                           AccountShareListingStatusActive,
		SeatLimit:                        3,
		AllowedModels:                    []string{"gpt-5.5"},
		PerUserConcurrency:               2,
		MinBalanceRequired:               1,
		AccountStatus:                    StatusActive,
		AccountSchedulable:               true,
		RepresentativeAccountConcurrency: 5,
		Codex5hLimitPercent:              90,
		Codex7dLimitPercent:              80,
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

func TestAccountShareModeEndMembershipRejectsLifecycleConflict(t *testing.T) {
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

func TestAccountShareModeEndMembershipUsesExistingConcurrentOperation(t *testing.T) {
	existingOperationID := "a8b25548-e953-42f8-83a5-c947fc2d629a"
	repo := &accountShareModeRepoStub{
		endSnapshot: &AccountShareMembership{
			ID:             761,
			ConsumerUserID: 42,
			Status:         AccountShareMembershipStatusActive,
			UpdatedAt:      time.Now().UTC(),
		},
		endMembership: &AccountShareMembership{
			ID:                761,
			ConsumerUserID:    42,
			Status:            AccountShareMembershipStatusEnding,
			EndingOperationID: existingOperationID,
		},
		finalizeMembership: &AccountShareMembership{
			ID:                761,
			ConsumerUserID:    42,
			Status:            AccountShareMembershipStatusEnded,
			EndingOperationID: existingOperationID,
		},
		finalizeDone: true,
	}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(&accountShareMembershipConcurrencyCacheStub{current: 0}),
	}
	svc.SetActionTokenSecret(strings.Repeat("s", 32))

	intent, err := svc.CreateEndMembershipToken(context.Background(), 42, 761)
	require.NoError(t, err)
	require.NotEqual(t, existingOperationID, intent.OperationID)

	membership, err := svc.EndMembership(context.Background(), 42, 761, intent.Token)
	require.NoError(t, err)
	require.Equal(t, AccountShareMembershipStatusEnded, membership.Status)
	require.Equal(t, existingOperationID, repo.finalizeOperationID)
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

func TestAccountShareModeEndingWorkerForceFinalizeSkipsInFlightRequest(t *testing.T) {
	operationID := "a19e2b8f-4c2d-4e9a-9b1c-7f6e5d4c3b2a"
	endingRequestedAt := time.Now().UTC().Add(-AccountShareModeEndSettlementForceTimeout - time.Minute)
	repo := &accountShareModeRepoStub{
		endingCandidates: []AccountShareEndingMembershipCandidate{{
			MembershipID:      79,
			OperationID:       operationID,
			EndingRequestedAt: endingRequestedAt,
			// 在途请求的心跳把 last_request_at 刷到了结束请求之后：说明还有请求在跑，
			// 即使 Redis 断连也不应强制结算。
			LastRequestAt: endingRequestedAt.Add(5 * time.Minute),
		}},
		finalizeMembership: &AccountShareMembership{ID: 79, ConsumerUserID: 42, Status: AccountShareMembershipStatusEnded},
		finalizeDone:       true,
	}
	cache := &accountShareMembershipConcurrencyCacheStub{currentErr: errors.New("redis unavailable")}
	svc := &AccountShareModeService{
		repo:               repo,
		concurrencyService: NewConcurrencyService(cache),
	}

	svc.processEndingMembershipsOnce(context.Background())

	require.Equal(t, 0, repo.finalizeCalls)
}

func TestAccountShareModeEndingWorkerForceFinalizesWhenNoInFlightRequest(t *testing.T) {
	operationID := "b20e3c9a-5d3e-4fa0-8a2c-8a7f6e5d4c3b"
	endingRequestedAt := time.Now().UTC().Add(-AccountShareModeEndSettlementForceTimeout - time.Minute)
	repo := &accountShareModeRepoStub{
		endingCandidates: []AccountShareEndingMembershipCandidate{{
			MembershipID:      80,
			OperationID:       operationID,
			EndingRequestedAt: endingRequestedAt,
			// 无在途请求（LastRequestAt 早于结束请求）：Redis 断连超过阈值后应强制结算。
			LastRequestAt: endingRequestedAt.Add(-time.Minute),
		}},
		finalizeMembership: &AccountShareMembership{ID: 80, ConsumerUserID: 42, Status: AccountShareMembershipStatusEnded},
		finalizeDone:       true,
	}
	cache := &accountShareMembershipConcurrencyCacheStub{currentErr: errors.New("redis unavailable")}
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

func TestAccountShareModeResolveBindingPreservesQueuedRecoveringState(t *testing.T) {
	repo := &accountShareModeRepoStub{
		bindingResults: []accountShareModeBindingResult{
			{err: ErrAccountShareListingNotFound},
			{err: ErrAccountShareListingNotFound},
			{err: ErrAccountShareListingNotFound},
			{err: ErrAccountShareModeRecovering},
		},
	}
	svc := &AccountShareModeService{repo: repo}

	membership, listing, err := svc.ResolveActiveBindingForRequest(
		WithAccountShareModeRequest(context.Background(), 20, 30),
		20,
		30,
		50,
	)

	require.Nil(t, membership)
	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareModeRecovering)
	require.NotErrorIs(t, err, ErrAccountShareModeGroupUnbound)
	require.Equal(t, 4, repo.bindingCalls)
	require.Equal(t, 1, repo.activationCalls)
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

func TestAccountShareModeResolveBindingSuspendsDurableRateLimitAsRecovering(t *testing.T) {
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
		activationErr: ErrAccountShareModeRecovering,
	}
	repo.recoverableSuspend = repo.membership
	svc := &AccountShareModeService{repo: repo}
	selectionCtx := WithAccountShareModeRequest(context.Background(), 20, 30)

	membership, listing, err := svc.ResolveActiveBindingForRequest(selectionCtx, 20, 30, 50)
	if !errors.Is(err, ErrAccountShareModeRecovering) {
		t.Fatalf("expected durable unavailable account to suspend and report recovering, got membership=%#v listing=%#v err=%v", membership, listing, err)
	}
	if repo.recoverableCalls != 1 {
		t.Fatalf("expected one canonical recoverable suspension call, got %d", repo.recoverableCalls)
	}

	taskCtx := WithAccountShareModeRequestFromContext(context.Background(), selectionCtx)
	_, _, err = svc.ResolveActiveBindingForRequest(taskCtx, 20, 30, 50)
	if !errors.Is(err, ErrAccountShareModeRecovering) {
		t.Fatalf("expected cached recovering error, got %v", err)
	}
	if repo.bindingCalls != 5 {
		t.Fatalf("expected unavailable resolve to query active binding before retry, got %d", repo.bindingCalls)
	}
	if repo.recoverableCalls != 1 {
		t.Fatalf("expected cached unavailable resolve to skip suspension, got %d", repo.recoverableCalls)
	}
}

func TestAccountShareModeResolveBindingKeepsActiveMembershipDuringShortRateLimitWindow(t *testing.T) {
	now := time.Now().UTC()
	rateLimitedAt := now.Add(-5 * time.Second)
	resetAt := now.Add(20 * time.Second)
	baseRepo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{
			ID:             11,
			AccountID:      99,
			ConsumerUserID: 20,
			APIKeyID:       30,
			Status:         AccountShareMembershipStatusActive,
		},
		listing: &AccountShareListing{
			ID:                               12,
			AccountID:                        99,
			OwnerUserID:                      40,
			Status:                           AccountShareListingStatusActive,
			AccountStatus:                    StatusActive,
			AccountSchedulable:               true,
			RepresentativeAccountConcurrency: 1,
			RateLimitedAt:                    &rateLimitedAt,
			RateLimitResetAt:                 &resetAt,
			CurrentMembershipID:              accountShareModeInt64Ptr(11),
			CurrentAPIKeyID:                  accountShareModeInt64Ptr(30),
		},
	}
	repo := &accountShareModeRebindRepoStub{
		accountShareModeRepoStub: baseRepo,
		rebindToAccountID:        100,
	}
	svc := &AccountShareModeService{repo: repo}

	membership, listing, err := svc.ResolveActiveBindingForRequest(
		WithAccountShareModeRequest(context.Background(), 20, 30),
		20,
		30,
		50,
	)

	require.Nil(t, membership)
	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareModeRecovering)
	require.NotErrorIs(t, err, ErrAccountShareModeGroupUnbound)
	require.Zero(t, repo.rebindCalls)
	require.Zero(t, repo.recoverableCalls)
	require.NotNil(t, repo.membership)
	require.Equal(t, AccountShareMembershipStatusActive, repo.membership.Status)
	require.Equal(t, int64(99), repo.membership.AccountID)
}

func TestAccountShareModeResolveBindingReportsIdleTimeoutInsteadOfUnbound(t *testing.T) {
	lastRequestAt := time.Now().UTC().Add(-11 * time.Minute)
	repo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{
			ID:                 11,
			AccountID:          99,
			ConsumerUserID:     20,
			APIKeyID:           30,
			Status:             AccountShareMembershipStatusActive,
			IdleTimeoutMinutes: 10,
			JoinedAt:           lastRequestAt.Add(-time.Minute),
			LastRequestAt:      &lastRequestAt,
		},
		listing: &AccountShareListing{
			ID:                               12,
			AccountID:                        99,
			OwnerUserID:                      40,
			Status:                           AccountShareListingStatusActive,
			AccountStatus:                    StatusActive,
			AccountSchedulable:               true,
			RepresentativeAccountConcurrency: 1,
		},
		idleEndMembership: &AccountShareMembership{
			ID:             11,
			ConsumerUserID: 20,
			APIKeyID:       30,
			Status:         AccountShareMembershipStatusEnded,
			EndedReason:    AccountShareMembershipEndReasonIdleTimeout,
		},
	}
	svc := &AccountShareModeService{repo: repo}

	membership, listing, err := svc.ResolveActiveBindingForRequest(
		WithAccountShareModeRequest(context.Background(), 20, 30),
		20,
		30,
		50,
	)

	require.Nil(t, membership)
	require.Nil(t, listing)
	require.ErrorIs(t, err, ErrAccountShareMembershipIdleTimeout)
	require.NotErrorIs(t, err, ErrAccountShareModeGroupUnbound)
	require.Equal(t, 1, repo.idleEndCalls)
	require.Nil(t, repo.membership)
}

func TestAccountShareModeResolveBindingRebindsZeroConcurrencyAccountEvenWhenRoomHasCapacity(t *testing.T) {
	baseRepo := &accountShareModeRepoStub{
		membership: &AccountShareMembership{
			ID:             11,
			AccountID:      99,
			ConsumerUserID: 20,
			APIKeyID:       30,
		},
		listing: &AccountShareListing{
			ID:                               12,
			AccountID:                        99,
			OwnerUserID:                      40,
			Status:                           AccountShareListingStatusActive,
			AccountStatus:                    StatusActive,
			AccountSchedulable:               true,
			AccountConcurrency:               20,
			RepresentativeAccountConcurrency: 0,
		},
	}
	repo := &accountShareModeRebindRepoStub{
		accountShareModeRepoStub: baseRepo,
		rebindToAccountID:        100,
	}
	svc := &AccountShareModeService{repo: repo}

	membership, listing, err := svc.ResolveActiveBindingForRequest(
		WithAccountShareModeRequest(context.Background(), 20, 30),
		20,
		30,
		50,
	)

	require.NoError(t, err)
	require.Equal(t, 1, repo.rebindCalls)
	require.NotNil(t, membership)
	require.Equal(t, int64(100), membership.AccountID)
	require.NotNil(t, listing)
	require.Equal(t, int64(100), listing.AccountID)
	require.Equal(t, 20, listing.AccountConcurrency)
	require.Equal(t, 5, listing.RepresentativeAccountConcurrency)
	require.Zero(t, repo.recoverableCalls)
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

func TestValidateAccountShareAccountNameRejectsNamesLongerThanDatabaseLimit(t *testing.T) {
	require.NoError(t, validateAccountShareAccountName(strings.Repeat("房", AccountShareRoomNameMaxRunes)))
	require.NoError(t, validateAccountShareAccountName(strings.Repeat("😀", AccountShareRoomNameMaxRunes)))
	require.ErrorIs(
		t,
		validateAccountShareAccountName(strings.Repeat("房", AccountShareRoomNameMaxRunes+1)),
		ErrAccountShareModeInvalidName,
	)
}
