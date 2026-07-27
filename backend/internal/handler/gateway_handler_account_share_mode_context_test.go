package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func (m *concurrencyCacheMock) AcquireAccountShareMembershipSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) ReleaseAccountShareMembershipSlot(context.Context, int64, string) error {
	return nil
}

func (m *concurrencyCacheMock) GetAccountShareMembershipConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}

func (m *concurrencyCacheMock) RefreshAccountSlot(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) RefreshAccountShareMembershipSlot(context.Context, int64, string) (bool, error) {
	return true, nil
}

func (m *concurrencyCacheMock) SlotLeaseTTL() time.Duration {
	return time.Hour
}

type gatewayAccountShareModeContextRepo struct {
	service.AccountShareModeRepository

	mu sync.Mutex

	membership *service.AccountShareMembership
	listing    *service.AccountShareListing

	bindingCalls       int
	selectionRequest   service.AccountShareModeRequestContext
	selectionContextOK bool
	usageRequest       service.AccountShareModeRequestContext
	usageContextOK     bool
}

func (r *gatewayAccountShareModeContextRepo) GetOpenMembershipRuntimeBinding(
	_ context.Context,
	membershipID int64,
	accountID int64,
) (*service.AccountShareMembershipRuntimeBinding, error) {
	return &service.AccountShareMembershipRuntimeBinding{
		BindingID:           8808,
		MembershipID:        membershipID,
		ListingID:           r.listing.ID,
		AccountID:           accountID,
		ListingRevisionID:   9909,
		TermsRevisionNumber: 1,
		RoutingGeneration:   1,
	}, nil
}

func (r *gatewayAccountShareModeContextRepo) IsModeGroup(context.Context, int64) (bool, error) {
	return true, nil
}

func (r *gatewayAccountShareModeContextRepo) GetActiveMembershipForRequest(
	ctx context.Context,
	_, _, _ int64,
) (*service.AccountShareMembership, *service.AccountShareListing, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.bindingCalls++
	if r.bindingCalls > 1 {
		return nil, nil, errors.New("account share binding was resolved more than once")
	}
	r.selectionRequest, r.selectionContextOK = service.AccountShareModeRequestFromContext(ctx)
	return r.membership, r.listing, nil
}

func (r *gatewayAccountShareModeContextRepo) TouchMembershipLastRequest(context.Context, int64, time.Time) error {
	return nil
}

func (r *gatewayAccountShareModeContextRepo) ResolvePolicy(ctx context.Context) (*service.AccountSharePolicy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.usageRequest, r.usageContextOK = service.AccountShareModeRequestFromContext(ctx)
	return &service.AccountSharePolicy{
		ID:              1,
		Version:         1,
		OwnerShareRatio: 0.8,
	}, nil
}

func (r *gatewayAccountShareModeContextRepo) snapshot() (
	int,
	service.AccountShareModeRequestContext,
	bool,
	service.AccountShareModeRequestContext,
	bool,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bindingCalls, r.selectionRequest, r.selectionContextOK, r.usageRequest, r.usageContextOK
}

type gatewayAccountShareModeContextAccountRepo struct {
	service.AccountRepository

	mu      sync.Mutex
	account *service.Account
	calls   int
	request service.AccountShareModeRequestContext
	ok      bool
}

func (r *gatewayAccountShareModeContextAccountRepo) GetByID(ctx context.Context, _ int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++
	r.request, r.ok = service.AccountShareModeRequestFromContext(ctx)
	return r.account, nil
}

func (r *gatewayAccountShareModeContextAccountRepo) snapshot() (int, service.AccountShareModeRequestContext, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.request, r.ok
}

type gatewayAccountShareModeContextGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r *gatewayAccountShareModeContextGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

func (r *gatewayAccountShareModeContextGroupRepo) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}

type gatewayAccountShareModeContextUpstream struct {
	service.HTTPUpstream
}

type gatewayAccountShareModeContextBillingIntentRepo struct {
	service.AccountShareBillingIntentRepository

	mu    sync.Mutex
	state service.AccountShareBillingIntentState
}

func (r *gatewayAccountShareModeContextBillingIntentRepo) CreatePrepared(
	_ context.Context,
	input service.CreateAccountShareBillingIntentInput,
) (*service.AccountShareBillingIntentState, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = service.AccountShareBillingIntentState{
		ID:              101,
		RequestID:       input.RequestID,
		ClientRequestID: input.ClientRequestID,
		DispatchID:      input.DispatchID,
		AttemptNo:       input.AttemptNo,
		APIKeyID:        input.APIKeyID,
		MembershipID:    input.MembershipID,
		Status:          service.AccountShareBillingIntentStatusCreated,
		StateToken:      1,
	}
	state := r.state
	return &state, true, nil
}

func (r *gatewayAccountShareModeContextBillingIntentRepo) MarkInFlight(
	_ context.Context,
	input service.AccountShareBillingIntentTransition,
) (*service.AccountShareBillingIntentState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Status = service.AccountShareBillingIntentStatusInFlight
	r.state.StateToken = input.ExpectedStateToken + 1
	state := r.state
	return &state, nil
}

func (r *gatewayAccountShareModeContextBillingIntentRepo) MarkReady(
	_ context.Context,
	input service.MarkAccountShareBillingIntentReadyInput,
) (*service.AccountShareBillingIntentState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Status = service.AccountShareBillingIntentStatusReady
	r.state.StateToken = input.ExpectedStateToken + 1
	state := r.state
	return &state, nil
}

func (r *gatewayAccountShareModeContextBillingIntentRepo) CancelCreated(
	_ context.Context,
	input service.AccountShareBillingIntentTransition,
	_,
	_ string,
) (*service.AccountShareBillingIntentState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state.Status = service.AccountShareBillingIntentStatusCancelled
	r.state.StateToken = input.ExpectedStateToken + 1
	state := r.state
	return &state, nil
}

func (u *gatewayAccountShareModeContextUpstream) Do(
	_ *http.Request,
	_ string,
	_ int64,
	_ int,
) (*http.Response, error) {
	return u.response(), nil
}

func (u *gatewayAccountShareModeContextUpstream) DoWithTLS(
	_ *http.Request,
	_ string,
	_ int64,
	_ int,
	_ *tlsfingerprint.Profile,
) (*http.Response, error) {
	return u.response(), nil
}

func (u *gatewayAccountShareModeContextUpstream) response() *http.Response {
	body := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_account_share","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-5","stop_reason":"","usage":{"input_tokens":12}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":"hello"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		``,
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"x-request-id": []string{"req_account_share"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestGatewayHandlerCompatibleEndpointsPreserveAccountShareModeContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		path     string
		body     string
		dispatch func(*GatewayHandler, *gin.Context)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: `{"model":"claude-sonnet-4-5","stream":false,"messages":[{"role":"user","content":"hello"}]}`,
			dispatch: func(h *GatewayHandler, c *gin.Context) {
				h.ChatCompletions(c)
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: `{"model":"claude-sonnet-4-5","stream":false,"input":"hello"}`,
			dispatch: func(h *GatewayHandler, c *gin.Context) {
				h.Responses(c)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				userID       int64 = 1101
				apiKeyID     int64 = 2202
				groupID      int64 = 3303
				accountID    int64 = 4404
				membershipID int64 = 5505
				listingID    int64 = 6606
				ownerUserID  int64 = 7707
			)

			group := &service.Group{
				ID:             groupID,
				Name:           "account-share-anthropic",
				Platform:       service.PlatformAnthropic,
				RateMultiplier: 1,
				Status:         service.StatusActive,
				Hydrated:       true,
			}
			account := &service.Account{
				ID:          accountID,
				Name:        "account-share-oauth",
				Platform:    service.PlatformAnthropic,
				Type:        service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "test-token"},
				Concurrency: 1,
				Status:      service.StatusActive,
				Schedulable: true,
			}
			membership := &service.AccountShareMembership{
				ID:             membershipID,
				ListingID:      listingID,
				AccountID:      accountID,
				OwnerUserID:    ownerUserID,
				ConsumerUserID: userID,
				APIKeyID:       apiKeyID,
				Status:         service.AccountShareMembershipStatusActive,
				TermsSnapshot: &service.AccountShareListingTermsSnapshot{
					ListingRevisionID:  9909,
					RowVersion:         1,
					SchemaVersion:      1,
					RoomName:           "account-share-room",
					Status:             service.AccountShareListingStatusActive,
					SeatLimit:          1,
					RateMultiplier:     1,
					AllowedModels:      []string{"claude-sonnet-4-5"},
					PerUserConcurrency: 1,
				},
			}
			listing := &service.AccountShareListing{
				ID:                 listingID,
				AccountID:          accountID,
				OwnerUserID:        ownerUserID,
				Status:             service.AccountShareListingStatusActive,
				PerUserConcurrency: 1,
				RateMultiplier:     1,
				AccountStatus:      service.StatusActive,
				AccountSchedulable: true,
			}

			accountShareRepo := &gatewayAccountShareModeContextRepo{
				membership: membership,
				listing:    listing,
			}
			accountRepo := &gatewayAccountShareModeContextAccountRepo{account: account}
			accountShareService := service.NewAccountShareModeService(
				accountShareRepo,
				accountRepo,
				nil,
				nil,
				nil,
				nil,
			)

			cfg := &config.Config{RunMode: config.RunModeSimple}
			cfg.Default.RateMultiplier = 1
			billingService := service.NewBillingService(cfg, nil)
			deferredService := service.NewDeferredService(accountRepo, nil, time.Hour)
			concurrencyCache := &concurrencyCacheMock{
				acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return true, nil
				},
				acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
					return true, nil
				},
			}
			concurrencyService := service.NewConcurrencyService(concurrencyCache)
			accountShareService.SetRuntimeDependencies(concurrencyService, nil, nil, nil)
			accountShareService.SetBillingIntentRepository(&gatewayAccountShareModeContextBillingIntentRepo{})
			gatewayService := service.NewGatewayService(
				accountRepo,
				nil,
				&gatewayAccountShareModeContextGroupRepo{group: group},
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				cfg,
				nil,
				concurrencyService,
				billingService,
				nil,
				nil,
				nil,
				&gatewayAccountShareModeContextUpstream{},
				deferredService,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				nil,
				accountShareService,
			)

			billingCacheService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg)
			t.Cleanup(billingCacheService.Stop)
			handler := &GatewayHandler{
				gatewayService:      gatewayService,
				billingCacheService: billingCacheService,
				concurrencyHelper: NewConcurrencyHelper(
					concurrencyService,
					SSEPingFormatClaude,
					time.Second,
				),
				maxAccountSwitches: 1,
			}

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			request := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")
			requestContext := context.WithValue(request.Context(), ctxkey.Group, group)
			requestContext = context.WithValue(requestContext, ctxkey.ClientRequestID, "client-account-share-context")
			request = request.WithContext(requestContext)
			c.Request = request

			apiKey := &service.APIKey{
				ID:      apiKeyID,
				UserID:  userID,
				GroupID: func() *int64 { id := groupID; return &id }(),
				Status:  service.StatusAPIKeyActive,
				User: &service.User{
					ID:          userID,
					Concurrency: 10,
					Balance:     100,
				},
				Group: group,
			}
			c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{
				UserID:      userID,
				Concurrency: 10,
			})

			tt.dispatch(handler, c)

			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

			accountCalls, accountRequest, accountContextOK := accountRepo.snapshot()
			require.Equal(t, 1, accountCalls)
			require.True(t, accountContextOK)
			require.Equal(t, userID, accountRequest.UserID)
			require.Equal(t, apiKeyID, accountRequest.APIKeyID)

			bindingCalls, selectionRequest, selectionContextOK, usageRequest, usageContextOK := accountShareRepo.snapshot()
			require.Equal(t, 1, bindingCalls, "计费任务必须复用选号时解析的 membership/listing 快照")
			require.True(t, selectionContextOK)
			require.Equal(t, userID, selectionRequest.UserID)
			require.Equal(t, apiKeyID, selectionRequest.APIKeyID)
			require.True(t, usageContextOK)
			require.Equal(t, userID, usageRequest.UserID)
			require.Equal(t, apiKeyID, usageRequest.APIKeyID)
		})
	}
}
