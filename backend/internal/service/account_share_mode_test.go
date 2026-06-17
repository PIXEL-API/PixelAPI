package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type accountShareModeRepoStub struct {
	ensureNameErr       error
	modeGroup           *bool
	isModeCalls         int
	bindingCalls        int
	bindingResults      []accountShareModeBindingResult
	membership          *AccountShareMembership
	listing             *AccountShareListing
	listFilters         AccountShareListingFilters
	updateAdmin         bool
	updateCalls         int
	updateInput         UpdateAccountShareListingInput
	updateListing       *AccountShareListing
	beginInput          BeginAccountShareListingEditInput
	beginActorIsAdmin   bool
	beginListing        *AccountShareListing
	beginErr            error
	endMembership       *AccountShareMembership
	endCalls            int
	requestBillingCalls int
	requestBillingErr   error
	unavailableCalls    int
}

type accountShareModeBindingResult struct {
	membership *AccountShareMembership
	listing    *AccountShareListing
	err        error
}

type accountShareModeProxyRepoStub struct {
	proxy            *Proxy
	getVisibleUserID int64
	getVisibleID     int64
	getVisibleCalls  int
	getVisibleErr    error
	accountCount     int64
	countCalls       int
	countErr         error
}

type accountShareModeTesterStub struct {
	calls     int
	accountID int64
	modelID   string
	result    *ScheduledTestResult
	err       error
}

func (s *accountShareModeTesterStub) RunTestBackground(_ context.Context, accountID int64, modelID string) (*ScheduledTestResult, error) {
	s.calls++
	s.accountID = accountID
	s.modelID = modelID
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &ScheduledTestResult{Status: "success"}, nil
}

type accountShareModeRecoveryStub struct {
	calls     int
	accountID int64
	err       error
}

func (s *accountShareModeRecoveryStub) RecoverAccountAfterSuccessfulTest(_ context.Context, accountID int64) (*SuccessfulTestRecoveryResult, error) {
	s.calls++
	s.accountID = accountID
	if s.err != nil {
		return nil, s.err
	}
	return &SuccessfulTestRecoveryResult{ClearedError: true}, nil
}

func (r *accountShareModeProxyRepoStub) Create(_ context.Context, proxy *Proxy) error {
	if proxy.ID <= 0 {
		proxy.ID = 7
	}
	r.proxy = proxy
	return nil
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

func (r *accountShareModeRepoStub) EnsureModeGroup(context.Context, string) (*Group, error) {
	return &Group{ID: 1, Platform: PlatformOpenAI}, nil
}

func (r *accountShareModeRepoStub) GetModeGroup(context.Context, string) (*Group, error) {
	return &Group{ID: 1, Platform: PlatformOpenAI}, nil
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

func (r *accountShareModeRepoStub) CreateOpenAIListing(context.Context, *Account, *AccountShareListing, int64) (*AccountShareListing, error) {
	return nil, nil
}

func (r *accountShareModeRepoStub) GetListingByID(context.Context, int64, int64) (*AccountShareListing, error) {
	if r.listing != nil {
		return r.listing, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) GetListingByAccountID(context.Context, int64) (*AccountShareListing, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) ListListings(_ context.Context, _ int64, filters AccountShareListingFilters, _ pagination.PaginationParams) ([]AccountShareListing, *pagination.PaginationResult, error) {
	r.listFilters = filters
	return nil, &pagination.PaginationResult{}, nil
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

func (r *accountShareModeRepoStub) JoinListing(context.Context, int64, int64, int64, int) (*AccountShareMembership, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) EndMembership(context.Context, int64, int64) (*AccountShareMembership, error) {
	r.endCalls++
	if r.endMembership != nil {
		return r.endMembership, nil
	}
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) UpdateMembershipIdleTimeout(context.Context, int64, int64, int) (*AccountShareMembership, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) TouchMembershipLastRequest(context.Context, int64, time.Time) error {
	return nil
}

func (r *accountShareModeRepoStub) ListIdleMembershipCandidates(context.Context, time.Time, AccountShareIdleMembershipFilter, int) ([]AccountShareIdleMembershipCandidate, error) {
	return nil, nil
}

func (r *accountShareModeRepoStub) EndIdleMembership(context.Context, int64, time.Time) (*AccountShareMembership, error) {
	return nil, ErrAccountShareListingNotFound
}

func (r *accountShareModeRepoStub) ProcessUnavailableMemberships(context.Context, time.Time, int) (*AccountShareSeatBillingResult, error) {
	return &AccountShareSeatBillingResult{}, nil
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
	return r.membership, r.listing, nil
}

func (r *accountShareModeRepoStub) ResolvePolicy(context.Context, string) (*AccountShareModePolicy, error) {
	return &AccountShareModePolicy{Platform: PlatformOpenAI, PlatformShareRatio: AccountShareModeDefaultPlatformShareRatio, OwnerShareRatio: AccountShareModeDefaultOwnerShareRatio, Enabled: true}, nil
}

func (r *accountShareModeRepoStub) UpsertPolicy(context.Context, UpdateAccountShareModePolicyInput) (*AccountShareModePolicy, error) {
	return nil, nil
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

func TestAccountShareModeUpdateListingPassesAdminFlag(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}
	status := AccountShareListingStatusPaused

	_, err := svc.UpdateListing(context.Background(), 42, true, 7, UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, ErrAccountShareListingNotFound) {
		t.Fatalf("expected repository error, got %v", err)
	}
	if !repo.updateAdmin {
		t.Fatal("expected admin update flag to be passed through")
	}
}

func TestAccountShareModeUpdateListingRejectsAccountConcurrencyAboveLimit(t *testing.T) {
	repo := &accountShareModeRepoStub{}
	svc := &AccountShareModeService{repo: repo}
	concurrency := AccountShareModeMaxAccountConcurrency + 1

	_, err := svc.UpdateListing(context.Background(), 42, true, 7, UpdateAccountShareListingInput{Concurrency: &concurrency, EditSessionID: "edit-session"})
	if !errors.Is(err, ErrAccountShareModeInvalidConcurrency) {
		t.Fatalf("expected invalid concurrency error, got %v", err)
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

	_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{AllowedModels: &models})
	if err != nil {
		t.Fatalf("expected owner model update to pass, got %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("expected repository update once, got %d", repo.updateCalls)
	}
	if repo.updateAdmin {
		t.Fatal("expected owner update to stay non-admin")
	}
	if repo.updateInput.AllowedModels == nil {
		t.Fatal("expected normalized allowed models")
	}
	got := strings.Join(*repo.updateInput.AllowedModels, ",")
	if got != "gpt-5.5,gpt-5.4" {
		t.Fatalf("normalized models = %q", got)
	}

	name := "共享账号一"
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Name: &name})
	if !errors.Is(err, ErrAccountShareEditSessionRequired) {
		t.Fatalf("expected owner config update without edit session to be rejected, got %v", err)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("expected rejected config update to skip repository, got %d calls", repo.updateCalls)
	}

	sessionID := "edit-session-1"
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Name: &name, EditSessionID: sessionID})
	if err != nil {
		t.Fatalf("expected owner config update with edit session to pass, got %v", err)
	}
	if repo.updateCalls != 2 {
		t.Fatalf("expected repository update twice, got %d", repo.updateCalls)
	}
	if repo.updateInput.Name == nil || *repo.updateInput.Name != name {
		t.Fatalf("expected trimmed name in update input, got %#v", repo.updateInput.Name)
	}
	if repo.updateInput.EditSessionID != sessionID {
		t.Fatalf("expected edit session %q, got %q", sessionID, repo.updateInput.EditSessionID)
	}

	status := AccountShareListingStatusPaused
	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, ErrInsufficientPerms) {
		t.Fatalf("expected owner non-model update to be rejected, got %v", err)
	}
	if repo.updateCalls != 2 {
		t.Fatalf("expected rejected update to skip repository, got %d calls", repo.updateCalls)
	}

	_, err = svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Name: &name, EditSessionID: sessionID, ForceActiveEdit: true})
	if !errors.Is(err, ErrInsufficientPerms) {
		t.Fatalf("expected owner forced edit to be rejected, got %v", err)
	}
}

func TestAccountShareModeUpdateListingOwnerRelistRequiresSuccessfulTest(t *testing.T) {
	status := AccountShareListingStatusActive
	repo := &accountShareModeRepoStub{
		listing: &AccountShareListing{
			ID:            7,
			AccountID:     99,
			OwnerUserID:   42,
			Status:        AccountShareListingStatusDisabled,
			AllowedModels: []string{"gpt-5.5"},
		},
		updateListing: &AccountShareListing{ID: 7, AccountID: 99, OwnerUserID: 42, Status: AccountShareListingStatusActive},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	svc := &AccountShareModeService{
		repo:               repo,
		accountTestService: tester,
		rateLimitService:   recovery,
	}

	_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status})
	if err != nil {
		t.Fatalf("expected owner relist to pass after successful test, got %v", err)
	}
	if tester.calls != 1 || tester.accountID != 99 || tester.modelID != "gpt-5.5" {
		t.Fatalf("unexpected tester call: calls=%d account=%d model=%q", tester.calls, tester.accountID, tester.modelID)
	}
	if recovery.calls != 1 || recovery.accountID != 99 {
		t.Fatalf("unexpected recovery call: calls=%d account=%d", recovery.calls, recovery.accountID)
	}
	if repo.updateCalls != 1 || repo.updateInput.Status == nil || *repo.updateInput.Status != AccountShareListingStatusActive {
		t.Fatalf("expected one active status update, calls=%d input=%#v", repo.updateCalls, repo.updateInput.Status)
	}
	if repo.updateAdmin {
		t.Fatal("expected owner relist to stay non-admin")
	}
}

func TestAccountShareModeUpdateListingOwnerRelistRejectsFailedTest(t *testing.T) {
	status := AccountShareListingStatusActive
	repo := &accountShareModeRepoStub{
		listing: &AccountShareListing{
			ID:          7,
			AccountID:   99,
			OwnerUserID: 42,
			Status:      AccountShareListingStatusPaused,
		},
		updateListing: &AccountShareListing{ID: 7, AccountID: 99, OwnerUserID: 42, Status: AccountShareListingStatusActive},
	}
	tester := &accountShareModeTesterStub{result: &ScheduledTestResult{Status: "failed", ErrorMessage: "oauth expired"}}
	recovery := &accountShareModeRecoveryStub{}
	svc := &AccountShareModeService{
		repo:               repo,
		accountTestService: tester,
		rateLimitService:   recovery,
	}

	_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, infraerrors.New(400, "ACCOUNT_SHARE_RELIST_TEST_FAILED", "")) {
		t.Fatalf("expected relist test failure, got %v", err)
	}
	if tester.calls != 1 {
		t.Fatalf("expected one tester call, got %d", tester.calls)
	}
	if recovery.calls != 0 {
		t.Fatalf("expected recovery not to run, got %d calls", recovery.calls)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected failed relist to skip repository update, got %d calls", repo.updateCalls)
	}
}

func TestAccountShareModeUpdateListingOwnerRelistRejectsUnavailableAccountAfterRecovery(t *testing.T) {
	status := AccountShareListingStatusActive
	repo := &accountShareModeRepoStub{
		listing: &AccountShareListing{
			ID:                 7,
			AccountID:          99,
			OwnerUserID:        42,
			Status:             AccountShareListingStatusDisabled,
			AccountStatus:      StatusDisabled,
			AccountSchedulable: true,
		},
		updateListing: &AccountShareListing{ID: 7, AccountID: 99, OwnerUserID: 42, Status: AccountShareListingStatusActive},
	}
	tester := &accountShareModeTesterStub{}
	recovery := &accountShareModeRecoveryStub{}
	svc := &AccountShareModeService{
		repo:               repo,
		accountTestService: tester,
		rateLimitService:   recovery,
	}

	_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, ErrAccountShareRelistAccountUnavailable) {
		t.Fatalf("expected unavailable account relist rejection, got %v", err)
	}
	if tester.calls != 1 {
		t.Fatalf("expected one tester call, got %d", tester.calls)
	}
	if recovery.calls != 1 {
		t.Fatalf("expected one recovery call, got %d", recovery.calls)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected unavailable relist to skip repository update, got %d calls", repo.updateCalls)
	}
}

func TestAccountShareModeUpdateListingOwnerRelistRequiresOwner(t *testing.T) {
	status := AccountShareListingStatusActive
	repo := &accountShareModeRepoStub{
		listing: &AccountShareListing{
			ID:          7,
			AccountID:   99,
			OwnerUserID: 100,
			Status:      AccountShareListingStatusDisabled,
		},
	}
	tester := &accountShareModeTesterStub{}
	svc := &AccountShareModeService{
		repo:               repo,
		accountTestService: tester,
		rateLimitService:   &accountShareModeRecoveryStub{},
	}

	_, err := svc.UpdateListing(context.Background(), 42, false, 7, UpdateAccountShareListingInput{Status: &status})
	if !errors.Is(err, ErrAccountShareListingNotFound) {
		t.Fatalf("expected non-owner relist to be hidden as not found, got %v", err)
	}
	if tester.calls != 0 {
		t.Fatalf("expected non-owner relist to skip test, got %d calls", tester.calls)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("expected non-owner relist to skip repository update, got %d calls", repo.updateCalls)
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

	got, err := svc.BeginListingEdit(context.Background(), 100, true, 7, "edit-session", false)
	if err != nil {
		t.Fatalf("BeginListingEdit failed: %v", err)
	}
	if !repo.beginActorIsAdmin {
		t.Fatal("expected admin flag to pass through")
	}
	if repo.beginInput.SessionID != "edit-session" {
		t.Fatalf("unexpected edit session: %q", repo.beginInput.SessionID)
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

func TestAccountShareModeListingConfigAcceptsMaxSeatsWithFloorConcurrency(t *testing.T) {
	err := validateAccountShareListingConfig(
		AccountShareModeMaxSeats,
		1,
		[]string{"gpt-5"},
		4,
		AccountShareModeMaxAccountConcurrency,
		0.2,
		0,
		0,
		AccountShareModeDefaultCodexLimitPercent,
		AccountShareModeDefaultCodexLimitPercent,
	)
	if err != nil {
		t.Fatalf("expected max seats and max account concurrency to be valid, got %v", err)
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

func TestAccountShareModeEndMembershipAcceptsIssuedConfirmationToken(t *testing.T) {
	repo := &accountShareModeRepoStub{
		endMembership: &AccountShareMembership{
			ID:             7,
			ConsumerUserID: 42,
			OwnerUserID:    100,
			APIKeyID:       0,
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

func TestAccountShareModeResolveBindingRefreshesExpiredSeatBeforeReturningUnbound(t *testing.T) {
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
		t.Fatalf("expected binding query retried after catch-up, got %d", repo.bindingCalls)
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
	if repo.bindingCalls != 1 {
		t.Fatalf("expected cached unavailable resolve to skip binding query, got %d", repo.bindingCalls)
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

func TestBuildAccountShareModeBillingSnapshotDisabledPolicyKeepsPlatformRevenue(t *testing.T) {
	snapshot := BuildAccountShareModeBillingSnapshot(
		&AccountShareMembership{ID: 1, AccountID: 10, ConsumerUserID: 20, APIKeyID: 30},
		&AccountShareListing{ID: 2, AccountID: 10, OwnerUserID: 40, RateMultiplier: 1, HourlyRate: 0.2},
		&AccountShareModePolicy{Enabled: false, OwnerShareRatio: 0.9, PlatformShareRatio: 0.1},
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
		&AccountShareModePolicy{Enabled: true, OwnerShareRatio: 0, PlatformShareRatio: 0.25},
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
}

func accountShareModeInt64Ptr(v int64) *int64 {
	return &v
}

func accountShareModeBoolPtr(v bool) *bool {
	return &v
}
