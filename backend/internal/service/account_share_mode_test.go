package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

type accountShareModeRepoStub struct {
	ensureNameErr        error
	modeGroup            *bool
	isModeCalls          int
	bindingCalls         int
	activationCalls      int
	bindingResults       []accountShareModeBindingResult
	membership           *AccountShareMembership
	listing              *AccountShareListing
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
	endMembership        *AccountShareMembership
	endCalls             int
	submitReview         *AccountShareReview
	submitReviewInput    SubmitAccountShareReviewInput
	submitReviewCalls    int
	submitReviewErr      error
	requestBillingCalls  int
	requestBillingErr    error
	unavailableCalls     int
	dispatchFailureCalls int
	createdAccount       *Account
	createdListing       *AccountShareListing
	createdModeGroupID   int64
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

type accountShareReviewSettingRepoStub struct {
	values map[string]string
}

type accountShareRecommendationAPIKeyRepoStub struct {
	APIKeyRepository
	key   *APIKey
	err   error
	calls int
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
	panic("unexpected GetAll call")
}

func (s *accountShareReviewSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
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

func (r *accountShareModeRepoStub) EnsureModeGroup(_ context.Context, platform string) (*Group, error) {
	return &Group{ID: 1, Platform: platform}, nil
}

func (r *accountShareModeRepoStub) GetModeGroup(_ context.Context, platform string) (*Group, error) {
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

func (r *accountShareModeRepoStub) GetListingByID(context.Context, int64, int64) (*AccountShareListing, error) {
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

func (r *accountShareModeRepoStub) ListListingReviews(context.Context, int64, int64, pagination.PaginationParams) ([]AccountShareReview, *pagination.PaginationResult, error) {
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

func (r *accountShareModeRepoStub) SuspendMembershipForDispatchFailure(context.Context, int64, time.Time, time.Time) (*AccountShareMembership, error) {
	r.dispatchFailureCalls++
	r.unavailableCalls++
	membership := r.membership
	if membership == nil {
		membership = &AccountShareMembership{ID: 11, ConsumerUserID: 20, APIKeyID: 30}
	}
	r.membership = nil
	r.listing = nil
	return membership, nil
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

func TestAccountShareModeJoinListingRejectsZeroIdleTimeout(t *testing.T) {
	svc := &AccountShareModeService{}

	_, err := svc.JoinListing(context.Background(), 1, 2, 3, 0)
	if !errors.Is(err, ErrAccountShareModeInvalidIdleTimeout) {
		t.Fatalf("expected invalid idle timeout, got %v", err)
	}
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
	if repo.bindingCalls != 4 {
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

func TestBuildAccountShareModeBillingSnapshotSkipsOwnerSelfUse(t *testing.T) {
	snapshot := BuildAccountShareModeBillingSnapshot(
		&AccountShareMembership{ID: 1, AccountID: 10, ConsumerUserID: 40, APIKeyID: 30},
		&AccountShareListing{ID: 2, AccountID: 10, OwnerUserID: 40, RateMultiplier: 1, HourlyRate: 0.2},
		&AccountShareModePolicy{Enabled: true, OwnerShareRatio: 0.9, PlatformShareRatio: 0.1},
		1.25,
		0,
		100,
	)
	if snapshot != nil {
		t.Fatalf("expected owner self-use snapshot to be skipped, got %#v", snapshot)
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
