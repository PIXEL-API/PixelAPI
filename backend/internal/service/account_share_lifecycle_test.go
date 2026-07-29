package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

const accountShareLifecycleTestTokenSecret = "0123456789abcdef0123456789abcdef"

type accountShareLifecycleManagementCall struct {
	viewerUserID  int64
	viewerIsAdmin bool
	listingID     int64
}

type accountShareLifecycleTransitionCall struct {
	actorUserID  int64
	actorIsAdmin bool
	listingID    int64
	command      string
	input        AccountShareRoomLifecycleCommandInput
}

type accountShareLifecycleRepoStub struct {
	*accountShareModeRepoStub

	managementStates []AccountShareRoomManagementState
	managementErr    error
	managementCalls  []accountShareLifecycleManagementCall

	transitionResults map[string]*AccountShareListing
	transitionErrors  map[string]error
	transitionCalls   []accountShareLifecycleTransitionCall
	roomAccounts      []AccountShareRoomAccount
	roomAccountsErr   error
	roomAccountCalls  int
	transitionSignal  chan accountShareLifecycleTransitionCall
	transitionHook    func(string)

	validatingRoomIDs     []int64
	validatingRoomIDsErr  error
	validatingRoomIDCalls int
	validatingStaleBefore time.Time
	validatingLimit       int

	softDeleteOperation *AccountShareRoomOperation
	softDeleteErr       error
	softDeleteCalls     int
	softDeleteActorID   int64
	softDeleteAdmin     bool
	softDeleteListingID int64
	softDeleteInput     AccountShareRoomDeleteInput

	existingDeleteOperation *AccountShareRoomOperation
	findDeleteCalls         int
	findDeleteRequestID     string

	finalizeOperation   *AccountShareRoomOperation
	finalizeErr         error
	finalizeCalls       int
	finalizeListingID   int64
	finalizeOperationID string

	roomOperation *AccountShareRoomOperation
}

var _ AccountShareModeRepository = (*accountShareLifecycleRepoStub)(nil)
var _ accountShareLifecycleRepository = (*accountShareLifecycleRepoStub)(nil)

func (r *accountShareLifecycleRepoStub) GetRoomManagementState(
	_ context.Context,
	viewerUserID int64,
	viewerIsAdmin bool,
	listingID int64,
) (*AccountShareRoomManagementState, error) {
	r.managementCalls = append(r.managementCalls, accountShareLifecycleManagementCall{
		viewerUserID:  viewerUserID,
		viewerIsAdmin: viewerIsAdmin,
		listingID:     listingID,
	})
	if r.managementErr != nil {
		return nil, r.managementErr
	}
	if len(r.managementStates) == 0 {
		return nil, ErrAccountShareListingNotFound
	}
	index := len(r.managementCalls) - 1
	if index >= len(r.managementStates) {
		index = len(r.managementStates) - 1
	}
	return cloneAccountShareRoomManagementState(&r.managementStates[index]), nil
}

func (r *accountShareLifecycleRepoStub) TransitionRoomLifecycle(
	_ context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	command string,
	input AccountShareRoomLifecycleCommandInput,
) (*AccountShareListing, error) {
	call := accountShareLifecycleTransitionCall{
		actorUserID:  actorUserID,
		actorIsAdmin: actorIsAdmin,
		listingID:    listingID,
		command:      command,
		input:        input,
	}
	r.transitionCalls = append(r.transitionCalls, call)
	if r.transitionHook != nil {
		r.transitionHook(command)
	}
	if err := r.transitionErrors[command]; err != nil {
		if r.transitionSignal != nil {
			r.transitionSignal <- call
		}
		return nil, err
	}
	listing := r.transitionResults[command]
	if listing == nil {
		if r.transitionSignal != nil {
			r.transitionSignal <- call
		}
		return nil, ErrAccountShareRoomInvalidTransition
	}
	cloned := *listing
	cloned.AllowedModels = append([]string(nil), listing.AllowedModels...)
	if r.transitionSignal != nil {
		r.transitionSignal <- call
	}
	return &cloned, nil
}

type accountShareLifecycleContextTester struct {
	contextErr error
}

func (t *accountShareLifecycleContextTester) RunTestBackground(
	ctx context.Context,
	_ int64,
	_ string,
) (*ScheduledTestResult, error) {
	t.contextErr = ctx.Err()
	return &ScheduledTestResult{Status: "success"}, nil
}

func (r *accountShareLifecycleRepoStub) ListRoomAccounts(
	_ context.Context,
	_ int64,
	_ int64,
	_ bool,
) ([]AccountShareRoomAccount, error) {
	r.roomAccountCalls++
	return append([]AccountShareRoomAccount(nil), r.roomAccounts...), r.roomAccountsErr
}

func (r *accountShareLifecycleRepoStub) FinalizeDrainingRoom(
	context.Context,
	int64,
	int64,
) (*AccountShareListing, error) {
	return nil, ErrAccountShareRoomInvalidTransition
}

func (r *accountShareLifecycleRepoStub) ListDrainingRoomIDs(context.Context, int64, int) ([]int64, error) {
	return nil, nil
}

func (r *accountShareLifecycleRepoStub) ListValidatingRoomIDs(
	_ context.Context,
	staleBefore time.Time,
	limit int,
) ([]int64, error) {
	r.validatingRoomIDCalls++
	r.validatingStaleBefore = staleBefore
	r.validatingLimit = limit
	return append([]int64(nil), r.validatingRoomIDs...), r.validatingRoomIDsErr
}

type accountShareCreateValidationRepoStub struct {
	*accountShareLifecycleRepoStub
	AccountShareRoomRepository

	createRoomCalls int
	createdListing  *AccountShareListing
}

var _ AccountShareModeRepository = (*accountShareCreateValidationRepoStub)(nil)
var _ AccountShareRoomRepository = (*accountShareCreateValidationRepoStub)(nil)
var _ accountShareLifecycleRepository = (*accountShareCreateValidationRepoStub)(nil)

func (r *accountShareCreateValidationRepoStub) CreateRoomFromOwnedAccount(
	_ context.Context,
	ownerUserID int64,
	accountID int64,
	_ int64,
	_ string,
	listing *AccountShareListing,
) (*AccountShareListing, error) {
	r.createRoomCalls++
	if listing == nil {
		return nil, ErrServiceUnavailable
	}
	created := *listing
	created.AllowedModels = append([]string(nil), listing.AllowedModels...)
	created.ID = 701
	created.RowVersion = 1
	created.OwnerUserID = ownerUserID
	created.AccountID = accountID
	r.createdListing = &created
	r.accountShareModeRepoStub.listing = &created
	result := created
	result.AllowedModels = append([]string(nil), created.AllowedModels...)
	return &result, nil
}

func (r *accountShareCreateValidationRepoStub) ListRoomAccounts(
	ctx context.Context,
	listingID int64,
	viewerUserID int64,
	viewerIsAdmin bool,
) ([]AccountShareRoomAccount, error) {
	return r.accountShareLifecycleRepoStub.ListRoomAccounts(
		ctx,
		listingID,
		viewerUserID,
		viewerIsAdmin,
	)
}

type accountShareBlockingTesterStub struct {
	started   chan struct{}
	release   <-chan struct{}
	calls     int
	accountID int64
	modelID   string
}

func (s *accountShareBlockingTesterStub) RunTestBackground(
	ctx context.Context,
	accountID int64,
	modelID string,
) (*ScheduledTestResult, error) {
	s.calls++
	s.accountID = accountID
	s.modelID = modelID
	s.started <- struct{}{}
	select {
	case <-s.release:
		return &ScheduledTestResult{Status: "success"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type accountShareValidationLeaseRepoStub struct {
	ClusterRepository
	renewAllowed bool
	renewCalls   int
}

func (r *accountShareValidationLeaseRepoStub) RenewTaskLease(
	context.Context,
	string,
	string,
	string,
	string,
	int64,
	time.Duration,
) (bool, error) {
	r.renewCalls++
	return r.renewAllowed, nil
}

type accountShareLeaseLosingTesterStub struct {
	leaseRepo *accountShareValidationLeaseRepoStub
	calls     int
}

func (s *accountShareLeaseLosingTesterStub) RunTestBackground(
	context.Context,
	int64,
	string,
) (*ScheduledTestResult, error) {
	s.calls++
	s.leaseRepo.renewAllowed = false
	return &ScheduledTestResult{Status: "success"}, nil
}

func (r *accountShareLifecycleRepoStub) SoftDeleteRoom(
	_ context.Context,
	actorUserID int64,
	actorIsAdmin bool,
	listingID int64,
	input AccountShareRoomDeleteInput,
) (*AccountShareRoomOperation, error) {
	r.softDeleteCalls++
	r.softDeleteActorID = actorUserID
	r.softDeleteAdmin = actorIsAdmin
	r.softDeleteListingID = listingID
	r.softDeleteInput = input
	if r.softDeleteErr != nil {
		return nil, r.softDeleteErr
	}
	if r.softDeleteOperation == nil {
		return nil, ErrAccountShareRoomOperationConflict
	}
	cloned := *r.softDeleteOperation
	return &cloned, nil
}

func (r *accountShareLifecycleRepoStub) FindRoomDeleteOperation(
	_ context.Context,
	_ int64,
	_ bool,
	_ int64,
	requestID string,
) (*AccountShareRoomOperation, error) {
	r.findDeleteCalls++
	r.findDeleteRequestID = requestID
	if r.existingDeleteOperation == nil {
		return nil, nil
	}
	cloned := *r.existingDeleteOperation
	return &cloned, nil
}

func (r *accountShareLifecycleRepoStub) FinalizeRoomDeletion(
	_ context.Context,
	listingID int64,
	operationID string,
) (*AccountShareRoomOperation, error) {
	r.finalizeCalls++
	r.finalizeListingID = listingID
	r.finalizeOperationID = operationID
	if r.finalizeErr != nil {
		return nil, r.finalizeErr
	}
	if r.finalizeOperation == nil {
		return nil, ErrAccountShareRoomOperationConflict
	}
	cloned := *r.finalizeOperation
	return &cloned, nil
}

func (r *accountShareLifecycleRepoStub) ListPendingRoomDeletionOperations(
	context.Context,
	int,
) ([]AccountShareRoomOperation, error) {
	return nil, nil
}

func (r *accountShareLifecycleRepoStub) GetRoomOperation(
	context.Context,
	int64,
	bool,
	string,
) (*AccountShareRoomOperation, error) {
	if r.roomOperation == nil {
		return nil, ErrAccountShareRoomOperationConflict
	}
	cloned := *r.roomOperation
	return &cloned, nil
}

func cloneAccountShareRoomManagementState(
	state *AccountShareRoomManagementState,
) *AccountShareRoomManagementState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.AllowedActions = append([]string(nil), state.AllowedActions...)
	cloned.RuntimeMembershipIDs = append([]int64(nil), state.RuntimeMembershipIDs...)
	cloned.RuntimeAccountIDs = append([]int64(nil), state.RuntimeAccountIDs...)
	return &cloned
}

type accountShareLifecycleConcurrencyCacheStub struct {
	ConcurrencyCache
	counts     map[int64]int
	err        error
	batchCalls int
	accountIDs []int64
}

func (c *accountShareLifecycleConcurrencyCacheStub) GetAccountConcurrencyBatch(
	_ context.Context,
	accountIDs []int64,
) (map[int64]int, error) {
	c.batchCalls++
	c.accountIDs = append([]int64(nil), accountIDs...)
	if c.err != nil {
		return nil, c.err
	}
	result := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = c.counts[accountID]
	}
	return result, nil
}

func newAccountShareLifecycleTestService(
	repo *accountShareLifecycleRepoStub,
	cache ConcurrencyCache,
	tester accountShareConnectivityTester,
	recovery accountShareAccountStateRecovery,
	accountRepositories ...AccountRepository,
) *AccountShareModeService {
	if cache == nil {
		cache = &accountShareLifecycleConcurrencyCacheStub{}
	}
	var accountRepo AccountRepository
	if len(accountRepositories) > 0 {
		accountRepo = accountRepositories[0]
	}
	service := &AccountShareModeService{
		repo:                     repo,
		accountRepo:              accountRepo,
		concurrencyService:       NewConcurrencyService(cache),
		accountTestService:       tester,
		rateLimitService:         recovery,
		lifecycleContractEnabled: true,
	}
	service.SetActionTokenSecret(accountShareLifecycleTestTokenSecret)
	return service
}

func accountShareLifecycleTestRoomAccount(accountID int64) AccountShareRoomAccount {
	return AccountShareRoomAccount{
		AccountID:          accountID,
		Status:             StatusActive,
		Schedulable:        true,
		CurrentConcurrency: 1,
		PlacementState:     "active",
	}
}

func accountShareLifecycleTestAccountRepository(accounts ...*Account) AccountRepository {
	return &accountShareOwnedAccountRepoStub{accounts: accounts}
}

func TestAccountShareLifecycleRolloutDisabledBlocksMutations(t *testing.T) {
	service := &AccountShareModeService{}

	_, err := service.DrainRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.ErrorIs(t, err, ErrAccountShareLifecycleRolloutDisabled)
}

func TestCreateRoomFromOwnedAccountStartsValidatingAndCompletesAsyncValidation(t *testing.T) {
	ownerUserID := int64(42)
	account := &Account{
		ID:           70,
		Name:         "owned-account",
		Platform:     PlatformAnthropic,
		AccountLevel: AccountLevelPro,
		OwnerUserID:  &ownerUserID,
		Status:       StatusActive,
		Schedulable:  true,
		Concurrency:  5,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"claude-sonnet-4-20250514": "claude-sonnet-4-20250514",
			},
		},
	}
	lifecycleRepo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		transitionResults: map[string]*AccountShareListing{
			"validation-pass": {
				ID:          701,
				RowVersion:  2,
				AccountID:   account.ID,
				OwnerUserID: ownerUserID,
				Status:      AccountShareListingStatusActive,
			},
		},
		transitionSignal: make(chan accountShareLifecycleTransitionCall, 1),
		roomAccounts: []AccountShareRoomAccount{
			accountShareLifecycleTestRoomAccount(account.ID),
		},
	}
	repo := &accountShareCreateValidationRepoStub{
		accountShareLifecycleRepoStub: lifecycleRepo,
	}
	accountRepo := &accountShareOwnedAccountRepoStub{
		account:  account,
		accounts: []*Account{account},
	}
	validationRelease := make(chan struct{})
	validationReleased := false
	releaseValidation := func() {
		if validationReleased {
			return
		}
		close(validationRelease)
		validationReleased = true
	}
	defer releaseValidation()
	tester := &accountShareBlockingTesterStub{
		started: make(chan struct{}, 1),
		release: validationRelease,
	}
	recovery := &accountShareModeRecoveryStub{}
	service := &AccountShareModeService{
		repo:                     repo,
		accountRepo:              accountRepo,
		concurrencyService:       NewConcurrencyService(nil),
		accountTestService:       tester,
		rateLimitService:         recovery,
		lifecycleContractEnabled: true,
	}

	type createRoomResult struct {
		listing *AccountShareListing
		err     error
	}
	createResult := make(chan createRoomResult, 1)
	go func() {
		created, err := service.CreateRoomFromOwnedAccount(
			context.Background(),
			ownerUserID,
			CreateAccountShareRoomInput{
				AccountID:          account.ID,
				IdempotencyKey:     "create-room-validation",
				RoomName:           "验证房间",
				SeatLimit:          3,
				RateMultiplier:     1,
				AllowedModels:      []string{"claude-sonnet-4-20250514"},
				PerUserConcurrency: 1,
			},
		)
		createResult <- createRoomResult{listing: created, err: err}
	}()

	select {
	case <-tester.started:
	case <-time.After(2 * time.Second):
		t.Fatal("post-create connectivity validation did not start")
	}

	var result createRoomResult
	select {
	case result = <-createResult:
	case <-time.After(2 * time.Second):
		t.Fatal("room creation waited for connectivity validation instead of returning asynchronously")
	}
	require.NoError(t, result.err)
	require.NotNil(t, result.listing)
	require.Equal(t, AccountShareListingStatusValidating, result.listing.Status)
	require.Equal(t, 1, repo.createRoomCalls)
	require.NotNil(t, repo.createdListing)
	require.Equal(t, AccountShareListingStatusValidating, repo.createdListing.Status)

	releaseValidation()
	var transition accountShareLifecycleTransitionCall
	select {
	case transition = <-lifecycleRepo.transitionSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("post-create room validation did not complete")
	}
	require.Equal(t, "validation-pass", transition.command)
	require.Equal(t, int64(1), transition.input.ExpectedVersion)
	require.True(t, transition.input.Confirmed)
	require.True(t, transition.actorIsAdmin)
	require.Zero(t, transition.actorUserID)
	require.Equal(t, 1, tester.calls)
	require.Equal(t, account.ID, tester.accountID)
	require.Equal(t, "claude-sonnet-4-20250514", tester.modelID)
	require.Equal(t, 1, recovery.calls)
	require.Equal(t, account.ID, recovery.accountID)
}

func TestAccountShareRoomActivationValidationPass(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{
			listing: &AccountShareListing{
				ID:                 7,
				AccountID:          99,
				OwnerUserID:        42,
				AccountStatus:      StatusActive,
				AccountSchedulable: true,
			},
		},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       7,
			RoomName:        "稳定房间",
			OwnerUserID:     42,
			RowVersion:      3,
			LifecycleStatus: AccountShareListingStatusActive,
		}},
		transitionResults: map[string]*AccountShareListing{
			AccountShareRoomActionActivate: {
				ID:            7,
				RowVersion:    2,
				AccountID:     99,
				OwnerUserID:   42,
				Status:        AccountShareListingStatusValidating,
				AllowedModels: []string{"gpt-5.5"},
			},
			"validation-pass": {
				ID:         7,
				RowVersion: 3,
				AccountID:  99,
				Status:     AccountShareListingStatusActive,
			},
		},
		roomAccounts: []AccountShareRoomAccount{accountShareLifecycleTestRoomAccount(99)},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(&Account{ID: 99, Platform: PlatformOpenAI})
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, AccountShareListingStatusActive, state.LifecycleStatus)
	require.Equal(t, []string{AccountShareRoomActionDelete, AccountShareRoomActionDrain}, state.AllowedActions)
	require.Equal(t, 1, tester.calls)
	require.Equal(t, int64(99), tester.accountID)
	require.Equal(t, "gpt-5.5", tester.modelID)
	require.Equal(t, 1, recovery.calls)
	require.Equal(t, int64(99), recovery.accountID)
	require.Len(t, repo.transitionCalls, 2)
	require.Equal(t, AccountShareRoomActionActivate, repo.transitionCalls[0].command)
	require.Equal(t, int64(1), repo.transitionCalls[0].input.ExpectedVersion)
	require.False(t, repo.transitionCalls[0].actorIsAdmin)
	require.Equal(t, "validation-pass", repo.transitionCalls[1].command)
	require.Equal(t, int64(2), repo.transitionCalls[1].input.ExpectedVersion)
	require.True(t, repo.transitionCalls[1].input.Confirmed)
	require.Empty(t, repo.transitionCalls[1].input.Reason)
}

func TestAccountShareRoomValidationWorkerUsesDedicatedClusterLease(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository()
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)
	clusterRepo := &clusterAdminRepositoryStub{}
	cfg := testClusterRuntimeConfig()
	service.taskExecutor = NewClusterTaskExecutor(cfg, clusterRepo, NewClusterNodeState(cfg))

	service.processRoomValidationOnce()

	require.Equal(t, accountShareRoomValidationTaskName, clusterRepo.acquiredTaskName)
	require.NotEqual(t, accountShareSeatBillingTaskName, clusterRepo.acquiredTaskName)
	require.Zero(t, repo.validatingRoomIDCalls)
	require.Zero(t, repo.requestBillingCalls)
	require.Zero(t, repo.waiverCompCalls)
}

func TestAccountShareRoomValidationWorkerRecoversStaleValidatingRoom(t *testing.T) {
	listing := &AccountShareListing{
		ID:            17,
		RowVersion:    4,
		AccountID:     99,
		OwnerUserID:   42,
		Status:        AccountShareListingStatusValidating,
		AllowedModels: []string{"gpt-5.5"},
	}
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{listing: listing},
		validatingRoomIDs:        []int64{listing.ID},
		transitionResults: map[string]*AccountShareListing{
			"validation-pass": {
				ID:          listing.ID,
				RowVersion:  listing.RowVersion + 1,
				AccountID:   listing.AccountID,
				OwnerUserID: listing.OwnerUserID,
				Status:      AccountShareListingStatusActive,
			},
		},
		roomAccounts: []AccountShareRoomAccount{
			accountShareLifecycleTestRoomAccount(listing.AccountID),
		},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(
		&Account{ID: listing.AccountID, Platform: PlatformOpenAI},
	)
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)
	startedAt := time.Now().UTC()

	err := service.processRoomValidationOnceLeased(context.Background(), &ClusterLeaseGuard{})
	finishedAt := time.Now().UTC()

	require.NoError(t, err)
	require.Equal(t, 1, repo.validatingRoomIDCalls)
	require.Equal(t, accountShareRoomValidationBatchSize, repo.validatingLimit)
	require.Equal(t, []int64{listing.ID}, repo.getListingIDs)
	require.Equal(t, []int64{0}, repo.getListingViewerIDs)
	require.False(
		t,
		repo.validatingStaleBefore.Before(startedAt.Add(-accountShareRoomValidationRecoveryDelay)),
	)
	require.False(
		t,
		repo.validatingStaleBefore.After(finishedAt.Add(-accountShareRoomValidationRecoveryDelay)),
	)
	require.Len(t, repo.transitionCalls, 1)
	require.Equal(t, "validation-pass", repo.transitionCalls[0].command)
	require.Equal(t, listing.RowVersion, repo.transitionCalls[0].input.ExpectedVersion)
	require.True(t, repo.transitionCalls[0].actorIsAdmin)
	require.Zero(t, repo.transitionCalls[0].actorUserID)
	require.Equal(t, 1, tester.calls)
	require.Equal(t, 1, recovery.calls)
	require.Zero(t, repo.requestBillingCalls)
	require.Zero(t, repo.waiverCompCalls)
}

func TestAccountShareRoomValidationWorkerDoesNotCommitAfterLeaseLoss(t *testing.T) {
	listing := &AccountShareListing{
		ID:            17,
		RowVersion:    4,
		AccountID:     99,
		OwnerUserID:   42,
		Status:        AccountShareListingStatusValidating,
		AllowedModels: []string{"gpt-5.5"},
	}
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{listing: listing},
		validatingRoomIDs:        []int64{listing.ID},
		transitionResults: map[string]*AccountShareListing{
			"validation-pass": {
				ID:          listing.ID,
				RowVersion:  listing.RowVersion + 1,
				AccountID:   listing.AccountID,
				OwnerUserID: listing.OwnerUserID,
				Status:      AccountShareListingStatusActive,
			},
		},
		roomAccounts: []AccountShareRoomAccount{
			accountShareLifecycleTestRoomAccount(listing.AccountID),
		},
	}
	leaseRepo := &accountShareValidationLeaseRepoStub{renewAllowed: true}
	tester := &accountShareLeaseLosingTesterStub{leaseRepo: leaseRepo}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(
		&Account{ID: listing.AccountID, Platform: PlatformOpenAI},
	)
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)
	executor := &ClusterTaskExecutor{
		repo:          leaseRepo,
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
		taskName:     accountShareRoomValidationTaskName,
		fencingToken: 1,
	}

	err := service.processRoomValidationOnceLeased(context.Background(), guard)

	require.ErrorIs(t, err, ErrClusterTaskLeaseLost)
	require.Equal(t, 3, leaseRepo.renewCalls)
	require.Equal(t, 1, tester.calls)
	require.Empty(t, repo.transitionCalls)
}

func TestAccountShareRoomValidationWorkerSkipsRoomThatIsNoLongerValidating(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{
			listing: &AccountShareListing{
				ID:         17,
				RowVersion: 5,
				Status:     AccountShareListingStatusPaused,
			},
		},
		validatingRoomIDs: []int64{17},
	}
	tester := &accountShareModeTesterStub{}
	service := newAccountShareLifecycleTestService(repo, nil, tester, nil)

	err := service.processRoomValidationOnceLeased(context.Background(), &ClusterLeaseGuard{})

	require.NoError(t, err)
	require.Equal(t, 1, repo.validatingRoomIDCalls)
	require.Empty(t, repo.transitionCalls)
	require.Zero(t, tester.calls)
	require.Zero(t, repo.roomAccountCalls)
}

func TestAccountShareRoomValidationWorkerIgnoresConcurrentLifecycleWinner(t *testing.T) {
	conflicts := []struct {
		name string
		err  error
	}{
		{name: "version conflict", err: ErrAccountShareVersionConflict},
		{name: "invalid transition", err: ErrAccountShareRoomInvalidTransition},
		{name: "operation conflict", err: ErrAccountShareRoomOperationConflict},
	}
	for _, conflict := range conflicts {
		t.Run(conflict.name, func(t *testing.T) {
			listing := &AccountShareListing{
				ID:            17,
				RowVersion:    4,
				AccountID:     99,
				OwnerUserID:   42,
				Status:        AccountShareListingStatusValidating,
				AllowedModels: []string{"gpt-5.5"},
			}
			repo := &accountShareLifecycleRepoStub{
				accountShareModeRepoStub: &accountShareModeRepoStub{listing: listing},
				validatingRoomIDs:        []int64{listing.ID},
				transitionErrors: map[string]error{
					"validation-pass": conflict.err,
				},
				roomAccounts: []AccountShareRoomAccount{
					accountShareLifecycleTestRoomAccount(listing.AccountID),
				},
			}
			tester := &accountShareModeTesterStub{}
			recovery := &accountShareModeRecoveryStub{}
			accountRepo := accountShareLifecycleTestAccountRepository(
				&Account{ID: listing.AccountID, Platform: PlatformOpenAI},
			)
			service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

			err := service.processRoomValidationOnceLeased(context.Background(), &ClusterLeaseGuard{})

			require.NoError(t, err)
			require.Len(t, repo.transitionCalls, 1)
			require.Equal(t, "validation-pass", repo.transitionCalls[0].command)
			require.Equal(t, listing.RowVersion, repo.transitionCalls[0].input.ExpectedVersion)
			require.Equal(t, 1, tester.calls)
			require.Equal(t, 1, recovery.calls)
		})
	}
}

func TestAccountShareRoomActivationValidationFailClosesRoom(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       7,
			RoomName:        "待修复房间",
			OwnerUserID:     42,
			RowVersion:      3,
			LifecycleStatus: AccountShareListingStatusPaused,
			StatusReason:    "oauth expired",
		}},
		transitionResults: map[string]*AccountShareListing{
			AccountShareRoomActionActivate: {
				ID:            7,
				RowVersion:    2,
				AccountID:     99,
				OwnerUserID:   42,
				Status:        AccountShareListingStatusValidating,
				AllowedModels: []string{"gpt-5.5"},
			},
			"validation-fail": {
				ID:         7,
				RowVersion: 3,
				AccountID:  99,
				Status:     AccountShareListingStatusPaused,
			},
		},
		roomAccounts: []AccountShareRoomAccount{accountShareLifecycleTestRoomAccount(99)},
	}
	tester := &accountShareModeTesterStub{
		result: &ScheduledTestResult{Status: "failed", ErrorMessage: "oauth expired"},
	}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(&Account{ID: 99, Platform: PlatformOpenAI})
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		context.Background(),
		900,
		true,
		7,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1, Reason: "管理员复测"},
	)

	require.Equal(t, "ACCOUNT_SHARE_ROOM_VALIDATION_FAILED", infraerrors.Reason(err))
	require.NotNil(t, state)
	require.Equal(t, AccountShareListingStatusPaused, state.LifecycleStatus)
	require.Equal(t, "oauth expired", state.StatusReason)
	require.Equal(t, 1, tester.calls)
	require.Zero(t, recovery.calls)
	require.Len(t, repo.transitionCalls, 2)
	require.True(t, repo.transitionCalls[0].actorIsAdmin)
	require.True(t, repo.transitionCalls[1].actorIsAdmin)
	require.Equal(t, "validation-fail", repo.transitionCalls[1].command)
	require.Contains(t, repo.transitionCalls[1].input.Reason, "oauth expired")
	require.True(t, repo.transitionCalls[1].input.Confirmed)
}

func TestAccountShareRoomActivationContinuesAfterRequestContextCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       7,
			OwnerUserID:     42,
			RowVersion:      3,
			LifecycleStatus: AccountShareListingStatusActive,
		}},
		transitionResults: map[string]*AccountShareListing{
			AccountShareRoomActionActivate: {
				ID:            7,
				RowVersion:    2,
				AccountID:     99,
				OwnerUserID:   42,
				Status:        AccountShareListingStatusValidating,
				AllowedModels: []string{"gpt-5.5"},
			},
			"validation-pass": {
				ID:         7,
				RowVersion: 3,
				AccountID:  99,
				Status:     AccountShareListingStatusActive,
			},
		},
		roomAccounts: []AccountShareRoomAccount{accountShareLifecycleTestRoomAccount(99)},
		transitionHook: func(command string) {
			if command == AccountShareRoomActionActivate {
				cancelRequest()
			}
		},
	}
	tester := &accountShareLifecycleContextTester{}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(&Account{ID: 99, Platform: PlatformOpenAI})
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		requestCtx,
		42,
		false,
		7,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.NoError(t, tester.contextErr)
	require.Len(t, repo.transitionCalls, 2)
	require.Equal(t, "validation-pass", repo.transitionCalls[1].command)
}

func TestAccountShareRoomActivationValidatesEveryModelLocallyAndEveryRoutableAccountUpstream(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       17,
			OwnerUserID:     42,
			RowVersion:      3,
			LifecycleStatus: AccountShareListingStatusActive,
		}},
		transitionResults: map[string]*AccountShareListing{
			AccountShareRoomActionActivate: {
				ID:            17,
				RowVersion:    2,
				OwnerUserID:   42,
				Status:        AccountShareListingStatusValidating,
				AllowedModels: []string{"gpt-5.5", "gpt-5.4"},
			},
			"validation-pass": {
				ID:         17,
				RowVersion: 3,
				Status:     AccountShareListingStatusActive,
			},
		},
		roomAccounts: []AccountShareRoomAccount{
			accountShareLifecycleTestRoomAccount(99),
			accountShareLifecycleTestRoomAccount(100),
		},
	}
	accountRepo := accountShareLifecycleTestAccountRepository(
		&Account{ID: 99, Platform: PlatformOpenAI},
		&Account{ID: 100, Platform: PlatformOpenAI},
	)
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		context.Background(),
		42,
		false,
		17,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.NoError(t, err)
	require.NotNil(t, state)
	require.ElementsMatch(t, []int64{99, 100}, tester.accountIDs)
	require.ElementsMatch(t, []string{"gpt-5.5", "gpt-5.5"}, tester.modelIDs)
	require.Equal(t, []int64{99, 100}, recovery.accountIDs)
	require.Equal(t, 2, repo.roomAccountCalls)
	require.Len(t, repo.transitionCalls, 2)
	require.Equal(t, "validation-pass", repo.transitionCalls[1].command)
}

func TestAccountShareRoomActivationRejectsModelUnsupportedByAnyRoomAccount(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       18,
			OwnerUserID:     42,
			RowVersion:      3,
			LifecycleStatus: AccountShareListingStatusPaused,
		}},
		transitionResults: map[string]*AccountShareListing{
			AccountShareRoomActionActivate: {
				ID:            18,
				RowVersion:    2,
				OwnerUserID:   42,
				Status:        AccountShareListingStatusValidating,
				AllowedModels: []string{"gpt-5.5", "gpt-5.4"},
			},
			"validation-fail": {
				ID:         18,
				RowVersion: 3,
				Status:     AccountShareListingStatusPaused,
			},
		},
		roomAccounts: []AccountShareRoomAccount{
			accountShareLifecycleTestRoomAccount(99),
			accountShareLifecycleTestRoomAccount(100),
		},
	}
	accountRepo := accountShareLifecycleTestAccountRepository(
		&Account{ID: 99, Platform: PlatformOpenAI},
		&Account{
			ID:       100,
			Platform: PlatformOpenAI,
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-5.5": "gpt-5.5"},
			},
		},
	)
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		context.Background(),
		42,
		false,
		18,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.Equal(t, "ACCOUNT_SHARE_ROOM_VALIDATION_FAILED", infraerrors.Reason(err))
	require.NotNil(t, state)
	require.Zero(t, tester.calls)
	require.Zero(t, recovery.calls)
	require.Len(t, repo.transitionCalls, 2)
	require.Equal(t, "validation-fail", repo.transitionCalls[1].command)
	require.Contains(t, repo.transitionCalls[1].input.Reason, "ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL")
}

func TestAccountShareRoomActivationValidationFailsWhenAccountRemainsUnavailable(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{
			listing: &AccountShareListing{
				ID:                 7,
				AccountID:          99,
				OwnerUserID:        42,
				AccountStatus:      StatusDisabled,
				AccountSchedulable: true,
			},
		},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       7,
			RoomName:        "不可用房间",
			OwnerUserID:     42,
			RowVersion:      3,
			LifecycleStatus: AccountShareListingStatusPaused,
		}},
		transitionResults: map[string]*AccountShareListing{
			AccountShareRoomActionActivate: {
				ID:            7,
				RowVersion:    2,
				AccountID:     99,
				OwnerUserID:   42,
				AllowedModels: []string{"gpt-5.5"},
			},
			"validation-fail": {
				ID:         7,
				RowVersion: 3,
				AccountID:  99,
				Status:     AccountShareListingStatusPaused,
			},
		},
		roomAccounts: []AccountShareRoomAccount{{
			AccountID:          99,
			Status:             StatusDisabled,
			Schedulable:        false,
			CurrentConcurrency: 1,
			PlacementState:     "active",
		}},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(&Account{ID: 99, Platform: PlatformOpenAI})
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.Equal(t, "ACCOUNT_SHARE_ROOM_VALIDATION_FAILED", infraerrors.Reason(err))
	require.NotNil(t, state)
	require.Equal(t, AccountShareListingStatusPaused, state.LifecycleStatus)
	require.Zero(t, tester.calls)
	require.Zero(t, recovery.calls)
	require.Len(t, repo.transitionCalls, 2)
	require.Equal(t, "validation-fail", repo.transitionCalls[1].command)
	require.Contains(t, repo.transitionCalls[1].input.Reason, "ACCOUNT_SHARE_RELIST_ACCOUNT_UNAVAILABLE")
}

func TestAccountShareRoomActivationPropagatesOwnershipCheckBeforeConnectivity(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		transitionErrors: map[string]error{
			AccountShareRoomActionActivate: ErrAccountShareListingNotFound,
		},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	accountRepo := accountShareLifecycleTestAccountRepository(&Account{ID: 99, Platform: PlatformOpenAI})
	service := newAccountShareLifecycleTestService(repo, nil, tester, recovery, accountRepo)

	state, err := service.ActivateRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomLifecycleCommandInput{ExpectedVersion: 1},
	)

	require.ErrorIs(t, err, ErrAccountShareListingNotFound)
	require.Nil(t, state)
	require.Len(t, repo.transitionCalls, 1)
	require.False(t, repo.transitionCalls[0].actorIsAdmin)
	require.Zero(t, tester.calls)
	require.Zero(t, recovery.calls)
}

func TestAccountShareRoomSuspendRequiresAdministratorReasonAndConfirmation(t *testing.T) {
	newService := func() (*AccountShareModeService, *accountShareLifecycleRepoStub) {
		repo := &accountShareLifecycleRepoStub{
			accountShareModeRepoStub: &accountShareModeRepoStub{},
			managementStates: []AccountShareRoomManagementState{{
				ListingID:       7,
				RoomName:        "管理房间",
				OwnerUserID:     42,
				RowVersion:      2,
				LifecycleStatus: AccountShareListingStatusSuspended,
			}},
			transitionResults: map[string]*AccountShareListing{
				AccountShareRoomActionSuspend: {
					ID:         7,
					RowVersion: 2,
					Status:     AccountShareListingStatusSuspended,
				},
			},
		}
		return newAccountShareLifecycleTestService(repo, nil, nil, nil), repo
	}

	t.Run("owner cannot suspend", func(t *testing.T) {
		service, repo := newService()
		state, err := service.SuspendRoom(
			context.Background(),
			42,
			false,
			7,
			AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: 1,
				Reason:          "owner request",
				Confirmed:       true,
			},
		)
		require.ErrorIs(t, err, ErrInsufficientPerms)
		require.Nil(t, state)
		require.Empty(t, repo.transitionCalls)
	})

	t.Run("admin reason is required", func(t *testing.T) {
		service, repo := newService()
		state, err := service.SuspendRoom(
			context.Background(),
			900,
			true,
			7,
			AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: 1,
				Confirmed:       true,
			},
		)
		require.ErrorIs(t, err, ErrAccountShareRoomReasonRequired)
		require.Nil(t, state)
		require.Empty(t, repo.transitionCalls)
	})

	t.Run("admin confirmation is required", func(t *testing.T) {
		service, repo := newService()
		state, err := service.SuspendRoom(
			context.Background(),
			900,
			true,
			7,
			AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: 1,
				Reason:          "风控封停",
			},
		)
		require.ErrorIs(t, err, ErrAccountShareForceConfirmationRequired)
		require.Nil(t, state)
		require.Empty(t, repo.transitionCalls)
	})

	t.Run("confirmed admin command is audited", func(t *testing.T) {
		service, repo := newService()
		state, err := service.SuspendRoom(
			context.Background(),
			900,
			true,
			7,
			AccountShareRoomLifecycleCommandInput{
				ExpectedVersion: 1,
				Reason:          "  风控封停  ",
				Confirmed:       true,
			},
		)
		require.NoError(t, err)
		require.NotNil(t, state)
		require.Equal(t, AccountShareListingStatusSuspended, state.LifecycleStatus)
		require.Equal(t, []string{AccountShareRoomActionActivate, AccountShareRoomActionDelete}, state.AllowedActions)
		require.Len(t, repo.transitionCalls, 1)
		require.True(t, repo.transitionCalls[0].actorIsAdmin)
		require.Equal(t, int64(900), repo.transitionCalls[0].actorUserID)
		require.Equal(t, AccountShareRoomActionSuspend, repo.transitionCalls[0].command)
		require.Equal(t, "风控封停", repo.transitionCalls[0].input.Reason)
		require.True(t, repo.transitionCalls[0].input.Confirmed)
	})
}

func TestAccountShareRoomManagementStateUsesViewerRoleForAllowedActions(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       7,
			RoomName:        "角色房间",
			OwnerUserID:     42,
			RowVersion:      1,
			LifecycleStatus: AccountShareListingStatusActive,
		}},
	}
	service := newAccountShareLifecycleTestService(repo, nil, nil, nil)

	ownerState, err := service.GetRoomManagementState(context.Background(), 42, false, 7)
	require.NoError(t, err)
	require.Equal(t, []string{AccountShareRoomActionDelete, AccountShareRoomActionDrain}, ownerState.AllowedActions)

	adminState, err := service.GetRoomManagementState(context.Background(), 900, true, 7)
	require.NoError(t, err)
	require.Equal(
		t,
		[]string{
			AccountShareRoomActionDelete,
			AccountShareRoomActionDrain,
			AccountShareRoomActionSuspend,
		},
		adminState.AllowedActions,
	)

	require.Equal(t, []accountShareLifecycleManagementCall{
		{viewerUserID: 42, viewerIsAdmin: false, listingID: 7},
		{viewerUserID: 900, viewerIsAdmin: true, listingID: 7},
	}, repo.managementCalls)
}

func TestAccountShareRoomManagementStateReturnsDeletedRoomAsReadOnlyArchive(t *testing.T) {
	deletedAt := time.Date(2026, 7, 27, 8, 30, 0, 0, time.UTC)
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:               7,
			RoomName:                "已删除房间",
			OwnerUserID:             42,
			RowVersion:              9,
			LifecycleStatus:         AccountShareListingStatusPaused,
			HealthState:             AccountShareRoomHealthHealthy,
			SeatLimit:               15,
			AdmissionRemainingSeats: 15,
			DeletedAt:               &deletedAt,
			RuntimeAccountIDs:       []int64{11},
		}},
	}
	cache := &accountShareLifecycleConcurrencyCacheStub{
		err: errors.New("deleted archive must not hydrate runtime state"),
	}
	service := newAccountShareLifecycleTestService(repo, cache, nil, nil)

	state, err := service.GetRoomManagementState(context.Background(), 42, false, 7)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, deletedAt, *state.DeletedAt)
	require.Equal(t, AccountShareRoomHealthUnavailable, state.HealthState)
	require.Zero(t, state.AdmissionRemainingSeats)
	require.Empty(t, state.AllowedActions)
	require.Zero(t, state.InFlightConcurrency)
	require.False(t, state.Blockers.RuntimeDependencyUnavailable)
	require.Zero(t, cache.batchCalls, "deleted archive must not query runtime concurrency")
}

func TestAccountShareRoomDeleteTokenRejectsTamperingExpiryAndClaimMismatch(t *testing.T) {
	service := &AccountShareModeService{}
	service.SetActionTokenSecret(accountShareLifecycleTestTokenSecret)
	now := time.Now().UTC()
	claims := accountShareRoomDeleteClaims{
		Action:      accountShareRoomDeleteTokenAction,
		ListingID:   7,
		ActorUserID: 42,
		RowVersion:  11,
		RoomName:    "删除确认房间",
		ExpiresAt:   now.Add(time.Minute).Unix(),
	}
	validToken, err := service.signRoomDeleteToken(claims)
	require.NoError(t, err)
	require.NoError(t, service.validateRoomDeleteToken(
		validToken,
		claims.ActorUserID,
		claims.ListingID,
		claims.RowVersion,
		claims.RoomName,
		now,
	))

	tokenParts := strings.Split(validToken, ".")
	require.Len(t, tokenParts, 2)
	require.NotEmpty(t, tokenParts[1])
	replacement := byte('A')
	if tokenParts[1][0] == replacement {
		replacement = 'B'
	}
	tamperedToken := tokenParts[0] + "." + string(replacement) + tokenParts[1][1:]
	expiredClaims := claims
	expiredClaims.ExpiresAt = now.Add(-time.Second).Unix()
	expiredToken, err := service.signRoomDeleteToken(expiredClaims)
	require.NoError(t, err)

	tests := []struct {
		name       string
		token      string
		rowVersion int64
		roomName   string
	}{
		{
			name:       "signature tampered",
			token:      tamperedToken,
			rowVersion: claims.RowVersion,
			roomName:   claims.RoomName,
		},
		{
			name:       "expired",
			token:      expiredToken,
			rowVersion: claims.RowVersion,
			roomName:   claims.RoomName,
		},
		{
			name:       "row version changed",
			token:      validToken,
			rowVersion: claims.RowVersion + 1,
			roomName:   claims.RoomName,
		},
		{
			name:       "room name changed",
			token:      validToken,
			rowVersion: claims.RowVersion,
			roomName:   claims.RoomName + "-renamed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := service.validateRoomDeleteToken(
				test.token,
				claims.ActorUserID,
				claims.ListingID,
				test.rowVersion,
				test.roomName,
				now,
			)
			require.ErrorIs(t, err, ErrAccountShareRoomDeleteTokenInvalid)
		})
	}
}

func TestAccountShareRoomDeleteRejectsVersionChangedAfterIntent(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{
			{
				ListingID:       7,
				RoomName:        "待删除房间",
				OwnerUserID:     42,
				RowVersion:      11,
				LifecycleStatus: AccountShareListingStatusPaused,
			},
			{
				ListingID:       7,
				RoomName:        "待删除房间",
				OwnerUserID:     42,
				RowVersion:      12,
				LifecycleStatus: AccountShareListingStatusPaused,
			},
		},
	}
	service := newAccountShareLifecycleTestService(repo, nil, nil, nil)
	intent, err := service.CreateRoomDeleteIntent(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteIntentInput{ExpectedVersion: 11},
	)
	require.NoError(t, err)
	require.True(t, intent.CanDelete)
	require.NotEmpty(t, intent.Token)

	operation, err := service.DeleteRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteInput{
			ExpectedVersion: 11,
			RoomName:        "待删除房间",
			Token:           intent.Token,
			Confirmed:       true,
		},
	)

	require.ErrorIs(t, err, ErrAccountShareVersionConflict)
	require.Nil(t, operation)
	require.Zero(t, repo.softDeleteCalls)
}

func TestAccountShareRoomDeleteReplaysDurableOperationBeforeMutablePreconditions(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		existingDeleteOperation: &AccountShareRoomOperation{
			ID:        "delete-operation-1",
			ListingID: 7,
			Action:    AccountShareRoomOperationActionDelete,
			Status:    "succeeded",
		},
	}
	service := newAccountShareLifecycleTestService(repo, nil, nil, nil)

	operation, err := service.DeleteRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteInput{
			RequestID: " durable-request-1 ",
		},
	)

	require.NoError(t, err)
	require.NotNil(t, operation)
	require.Equal(t, "succeeded", operation.Status)
	require.Equal(t, 1, repo.findDeleteCalls)
	require.Equal(t, "durable-request-1", repo.findDeleteRequestID)
	require.Empty(t, repo.managementCalls)
	require.Zero(t, repo.softDeleteCalls)
}

func TestAccountShareRoomDeleteRejectsRoomNameMismatchBeforeMutation(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:       7,
			RoomName:        "必须准确输入",
			OwnerUserID:     42,
			RowVersion:      11,
			LifecycleStatus: AccountShareListingStatusPaused,
		}},
	}
	service := newAccountShareLifecycleTestService(repo, nil, nil, nil)
	intent, err := service.CreateRoomDeleteIntent(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteIntentInput{ExpectedVersion: 11},
	)
	require.NoError(t, err)

	operation, err := service.DeleteRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteInput{
			ExpectedVersion: 11,
			RoomName:        "输入了另一个名称",
			Token:           intent.Token,
			Confirmed:       true,
		},
	)

	require.ErrorIs(t, err, ErrAccountShareRoomDeleteTokenInvalid)
	require.Nil(t, operation)
	require.Zero(t, repo.softDeleteCalls)
	require.Len(t, repo.managementCalls, 1)
}

func TestAccountShareRoomDeleteFailsClosedWhenRuntimeStateUnavailable(t *testing.T) {
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{{
			ListingID:         7,
			RoomName:          "运行中房间",
			OwnerUserID:       42,
			RowVersion:        11,
			LifecycleStatus:   AccountShareListingStatusPaused,
			RuntimeAccountIDs: []int64{99},
		}},
	}
	cache := &accountShareLifecycleConcurrencyCacheStub{
		counts: map[int64]int{99: 0},
	}
	service := newAccountShareLifecycleTestService(repo, cache, nil, nil)
	intent, err := service.CreateRoomDeleteIntent(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteIntentInput{ExpectedVersion: 11},
	)
	require.NoError(t, err)
	require.True(t, intent.CanDelete)
	cache.err = errors.New("redis unavailable")

	operation, err := service.DeleteRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteInput{
			ExpectedVersion: 11,
			RoomName:        "运行中房间",
			Token:           intent.Token,
			Confirmed:       true,
		},
	)

	require.ErrorIs(t, err, ErrAccountShareRuntimeDependencyUnavailable)
	require.Nil(t, operation)
	require.Zero(t, repo.softDeleteCalls)
	require.Equal(t, 2, cache.batchCalls)
	require.Equal(t, []int64{99}, cache.accountIDs)
}

func TestAccountShareRoomDeleteFinalizesWhenOnlyOwnOperationRemains(t *testing.T) {
	const operationID = "delete-op-1"
	repo := &accountShareLifecycleRepoStub{
		accountShareModeRepoStub: &accountShareModeRepoStub{},
		managementStates: []AccountShareRoomManagementState{
			{
				ListingID:         7,
				RoomName:          "可删除房间",
				OwnerUserID:       42,
				RowVersion:        11,
				LifecycleStatus:   AccountShareListingStatusPaused,
				RuntimeAccountIDs: []int64{99},
			},
			{
				ListingID:         7,
				RoomName:          "可删除房间",
				OwnerUserID:       42,
				RowVersion:        11,
				LifecycleStatus:   AccountShareListingStatusPaused,
				RuntimeAccountIDs: []int64{99},
			},
			{
				ListingID:         7,
				RoomName:          "可删除房间",
				OwnerUserID:       42,
				RowVersion:        12,
				LifecycleStatus:   AccountShareListingStatusDraining,
				RuntimeAccountIDs: []int64{99},
				Blockers: AccountShareRoomBlockers{
					ConflictingOperation:   true,
					ConflictingOperationID: operationID,
				},
			},
		},
		softDeleteOperation: &AccountShareRoomOperation{
			ID:          operationID,
			ListingID:   7,
			ActorUserID: 42,
			ActorRole:   "owner",
			Action:      AccountShareRoomOperationActionDelete,
			Status:      "running",
		},
		finalizeOperation: &AccountShareRoomOperation{
			ID:        operationID,
			ListingID: 7,
			Action:    AccountShareRoomOperationActionDelete,
			Status:    "succeeded",
		},
	}
	cache := &accountShareLifecycleConcurrencyCacheStub{
		counts: map[int64]int{99: 0},
	}
	service := newAccountShareLifecycleTestService(repo, cache, nil, nil)
	intent, err := service.CreateRoomDeleteIntent(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteIntentInput{ExpectedVersion: 11},
	)
	require.NoError(t, err)

	operation, err := service.DeleteRoom(
		context.Background(),
		42,
		false,
		7,
		AccountShareRoomDeleteInput{
			ExpectedVersion: 11,
			RoomName:        "可删除房间",
			Token:           intent.Token,
			Reason:          "房主确认删除",
			Confirmed:       true,
			RequestID:       "request-1",
		},
	)

	require.NoError(t, err)
	require.NotNil(t, operation)
	require.Equal(t, "succeeded", operation.Status)
	require.Equal(t, 1, repo.softDeleteCalls)
	require.Equal(t, int64(42), repo.softDeleteActorID)
	require.False(t, repo.softDeleteAdmin)
	require.Equal(t, int64(7), repo.softDeleteListingID)
	require.Equal(t, "request-1", repo.softDeleteInput.RequestID)
	require.Equal(t, 1, repo.finalizeCalls)
	require.Equal(t, int64(7), repo.finalizeListingID)
	require.Equal(t, operationID, repo.finalizeOperationID)
	require.Equal(t, 3, cache.batchCalls)
}

func TestAccountShareRoomAllowedActions(t *testing.T) {
	deletedAt := time.Now().UTC()
	tests := []struct {
		name          string
		state         *AccountShareRoomManagementState
		viewerIsAdmin bool
		want          []string
	}{
		{
			name: "active owner",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusActive,
			},
			want: []string{AccountShareRoomActionDelete, AccountShareRoomActionDrain},
		},
		{
			name: "active admin",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusActive,
			},
			viewerIsAdmin: true,
			want: []string{
				AccountShareRoomActionDelete,
				AccountShareRoomActionDrain,
				AccountShareRoomActionSuspend,
			},
		},
		{
			name: "paused owner",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusPaused,
			},
			want: []string{AccountShareRoomActionActivate, AccountShareRoomActionDelete},
		},
		{
			name: "suspended owner cannot activate",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusSuspended,
			},
			want: []string{AccountShareRoomActionDelete},
		},
		{
			name: "suspended admin can activate",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusSuspended,
			},
			viewerIsAdmin: true,
			want:          []string{AccountShareRoomActionActivate, AccountShareRoomActionDelete},
		},
		{
			name: "membership blocks delete but not drain",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusActive,
				Blockers: AccountShareRoomBlockers{
					ActiveMembershipCount: 1,
				},
			},
			want: []string{AccountShareRoomActionDrain},
		},
		{
			name: "membership blocks admin delete",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusActive,
				Blockers: AccountShareRoomBlockers{
					ActiveMembershipCount: 1,
				},
			},
			viewerIsAdmin: true,
			want:          []string{AccountShareRoomActionDrain, AccountShareRoomActionSuspend},
		},
		{
			name: "conflicting operation blocks every action",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusActive,
				Blockers: AccountShareRoomBlockers{
					ConflictingOperation: true,
				},
			},
			viewerIsAdmin: true,
			want:          []string{},
		},
		{
			name: "deleted room blocks every action",
			state: &AccountShareRoomManagementState{
				LifecycleStatus: AccountShareListingStatusPaused,
				DeletedAt:       &deletedAt,
			},
			viewerIsAdmin: true,
			want:          []string{},
		},
		{
			name:          "nil state",
			state:         nil,
			viewerIsAdmin: true,
			want:          []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				test.want,
				accountShareRoomAllowedActions(test.state, test.viewerIsAdmin),
			)
		})
	}
}
