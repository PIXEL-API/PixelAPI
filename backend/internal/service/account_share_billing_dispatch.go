package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	accountShareBillingReleaseBarrierTimeout  = 15 * time.Second
	accountShareBillingRecoveryAttemptTimeout = 5 * time.Second
	accountShareBillingRecoveryMaxDelay       = 5 * time.Second
)

var accountShareBillingDispatchNamespace = uuid.MustParse("919ac1d1-7dd8-4bbc-bb8e-2786627849d4")

type accountShareBillingDispatchContextKey struct{}
type forwardResultBillingGateContextKey struct{}
type openAIForwardResultBillingGateContextKey struct{}

var ErrAccountShareBillingUsageValidation = errors.New("account share billing usage validation failed")

// ForwardResultBillingGate serializes the durable billing transition for one
// Anthropic-compatible forward result. The service layer invokes it before a
// successful terminal response is exposed, while the handler may invoke it
// again after Forward returns; sync.Once guarantees both callers observe the
// same result without rebuilding a time-sensitive billing payload.
type ForwardResultBillingGate struct {
	once   sync.Once
	submit func(*ForwardResult) error
	err    error
}

// OpenAIForwardResultBillingGate is the OpenAI-compatible counterpart of
// ForwardResultBillingGate.
type OpenAIForwardResultBillingGate struct {
	once   sync.Once
	submit func(*OpenAIForwardResult) error
	err    error
}

func NewForwardResultBillingGate(submit func(*ForwardResult) error) *ForwardResultBillingGate {
	if submit == nil {
		return nil
	}
	return &ForwardResultBillingGate{submit: submit}
}

func NewOpenAIForwardResultBillingGate(submit func(*OpenAIForwardResult) error) *OpenAIForwardResultBillingGate {
	if submit == nil {
		return nil
	}
	return &OpenAIForwardResultBillingGate{submit: submit}
}

func WithForwardResultBillingGate(ctx context.Context, gate *ForwardResultBillingGate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if gate == nil {
		return ctx
	}
	return context.WithValue(ctx, forwardResultBillingGateContextKey{}, gate)
}

func WithOpenAIForwardResultBillingGate(ctx context.Context, gate *OpenAIForwardResultBillingGate) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if gate == nil {
		return ctx
	}
	return context.WithValue(ctx, openAIForwardResultBillingGateContextKey{}, gate)
}

func CommitForwardResultBillingGate(ctx context.Context, result *ForwardResult) (bool, error) {
	return commitForwardResultBillingGate(ctx, result, false)
}

func CommitForwardResultBillingGateBeforeTerminal(ctx context.Context, result *ForwardResult) (bool, error) {
	return commitForwardResultBillingGate(ctx, result, true)
}

func commitForwardResultBillingGate(ctx context.Context, result *ForwardResult, requireComplete bool) (bool, error) {
	if ctx == nil {
		return false, nil
	}
	gate, _ := ctx.Value(forwardResultBillingGateContextKey{}).(*ForwardResultBillingGate)
	if gate == nil {
		return false, nil
	}
	gate.once.Do(func() {
		if result == nil {
			gate.err = fmt.Errorf(
				"%w: %w: %w: forward result is required",
				ErrAccountShareBillingPreTerminalCommit,
				ErrAccountShareBillingUsageValidation,
				ErrAccountShareBillingIntentInvalid,
			)
			return
		}
		hasBillableUsage := ForwardResultHasBillableUsage(result)
		hasCompleteUsage := ForwardResultHasCompleteBillableUsage(result)
		if !hasBillableUsage && !(requireComplete && hasCompleteUsage) {
			gate.err = fmt.Errorf(
				"%w: %w: %w: forward result has no billable usage",
				ErrAccountShareBillingPreTerminalCommit,
				ErrAccountShareBillingUsageValidation,
				ErrAccountShareBillingIntentInvalid,
			)
			return
		}
		if requireComplete && !hasCompleteUsage {
			gate.err = fmt.Errorf(
				"%w: %w: %w: forward result has no complete billable usage",
				ErrAccountShareBillingPreTerminalCommit,
				ErrAccountShareBillingUsageValidation,
				ErrAccountShareBillingIntentInvalid,
			)
			return
		}
		if err := gate.submit(result); err != nil {
			gate.err = fmt.Errorf("%w: %w", ErrAccountShareBillingPreTerminalCommit, err)
		}
	})
	return true, gate.err
}

func CommitOpenAIForwardResultBillingGate(ctx context.Context, result *OpenAIForwardResult) (bool, error) {
	return commitOpenAIForwardResultBillingGate(ctx, result, false)
}

func CommitOpenAIForwardResultBillingGateBeforeTerminal(ctx context.Context, result *OpenAIForwardResult) (bool, error) {
	return commitOpenAIForwardResultBillingGate(ctx, result, true)
}

func commitOpenAIForwardResultBillingGate(ctx context.Context, result *OpenAIForwardResult, requireComplete bool) (bool, error) {
	if ctx == nil {
		return false, nil
	}
	gate, _ := ctx.Value(openAIForwardResultBillingGateContextKey{}).(*OpenAIForwardResultBillingGate)
	if gate == nil {
		return false, nil
	}
	gate.once.Do(func() {
		if result == nil {
			gate.err = fmt.Errorf(
				"%w: %w: %w: OpenAI forward result is required",
				ErrAccountShareBillingPreTerminalCommit,
				ErrAccountShareBillingUsageValidation,
				ErrAccountShareBillingIntentInvalid,
			)
			return
		}
		hasBillableUsage := OpenAIForwardResultHasBillableUsage(result)
		hasCompleteUsage := OpenAIForwardResultHasCompleteBillableUsage(result)
		if !hasBillableUsage && !(requireComplete && hasCompleteUsage) {
			gate.err = fmt.Errorf(
				"%w: %w: %w: OpenAI forward result has no billable usage",
				ErrAccountShareBillingPreTerminalCommit,
				ErrAccountShareBillingUsageValidation,
				ErrAccountShareBillingIntentInvalid,
			)
			return
		}
		if requireComplete && !hasCompleteUsage {
			gate.err = fmt.Errorf(
				"%w: %w: %w: OpenAI forward result has no complete billable usage",
				ErrAccountShareBillingPreTerminalCommit,
				ErrAccountShareBillingUsageValidation,
				ErrAccountShareBillingIntentInvalid,
			)
			return
		}
		if err := gate.submit(result); err != nil {
			gate.err = fmt.Errorf("%w: %w", ErrAccountShareBillingPreTerminalCommit, err)
		}
	})
	return true, gate.err
}

type AccountShareBillingDispatchInput struct {
	ClientRequestID    string
	AttemptNo          int
	APIKey             *APIKey
	User               *User
	Account            *Account
	Subscription       *UserSubscription
	RequestedModel     string
	RoutedModel        string
	InboundEndpoint    string
	UpstreamEndpoint   string
	RequestType        RequestType
	RequestPayloadHash string
	ServiceTier        string
	ReasoningEffort    string
	ChannelUsageFields ChannelUsageFields
}

type AccountShareBillingDispatch struct {
	repository  AccountShareBillingIntentRepository
	barrier     *accountShareBillingReleaseBarrier
	command     AccountShareBillingCommand
	recoveryCtx context.Context

	mu                sync.Mutex
	state             AccountShareBillingIntentState
	recoveryScheduled bool
}

type accountShareBillingReleaseBarrier struct {
	done         chan struct{}
	recovery     chan struct{}
	timeout      time.Duration
	completeOnce sync.Once
	recoveryOnce sync.Once
}

func newAccountShareBillingReleaseBarrier(timeout time.Duration) *accountShareBillingReleaseBarrier {
	if timeout <= 0 {
		timeout = accountShareBillingReleaseBarrierTimeout
	}
	return &accountShareBillingReleaseBarrier{
		done:     make(chan struct{}),
		recovery: make(chan struct{}),
		timeout:  timeout,
	}
}

func (b *accountShareBillingReleaseBarrier) complete() {
	if b == nil {
		return
	}
	b.completeOnce.Do(func() {
		close(b.done)
	})
}

// beginRecovery permanently disables the ordinary release timeout. Once the
// post-forward usage payload exists in memory, releasing the runtime slots
// before that payload is durable would let room deletion race ahead of billing.
func (b *accountShareBillingReleaseBarrier) beginRecovery() {
	if b == nil {
		return
	}
	b.recoveryOnce.Do(func() {
		close(b.recovery)
	})
}

func (b *accountShareBillingReleaseBarrier) completed() bool {
	if b == nil {
		return true
	}
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

func (b *accountShareBillingReleaseBarrier) recovering() bool {
	if b == nil {
		return false
	}
	select {
	case <-b.recovery:
		return true
	default:
		return false
	}
}

func (b *accountShareBillingReleaseBarrier) wait(ctx context.Context) {
	if b == nil || b.completed() {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	waitForDurableResult := func() {
		select {
		case <-b.done:
		case <-ctx.Done():
		}
	}
	if b.recovering() {
		waitForDurableResult()
		return
	}
	timer := time.NewTimer(b.timeout)
	defer timer.Stop()
	select {
	case <-b.done:
	case <-b.recovery:
		waitForDurableResult()
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (d *AccountShareBillingDispatch) State() AccountShareBillingIntentState {
	if d == nil {
		return AccountShareBillingIntentState{}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func (d *AccountShareBillingDispatch) markReady(ctx context.Context, command *UsageBillingCommand) error {
	if d == nil || d.repository == nil || d.barrier == nil {
		return ErrServiceUnavailable
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	readyInput, err := buildAccountShareBillingIntentReadyInput(d.state, d.command, command)
	if err != nil {
		d.barrier.complete()
		return err
	}
	return d.markReadyLocked(ctx, readyInput)
}

func (d *AccountShareBillingDispatch) failWithoutUsage(
	ctx context.Context,
	errorCode string,
	errorMessage string,
) error {
	if d == nil || d.repository == nil || d.barrier == nil {
		return ErrServiceUnavailable
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Status == AccountShareBillingIntentStatusNeedsAttention {
		d.barrier.complete()
		return nil
	}
	if d.state.Status != AccountShareBillingIntentStatusInFlight {
		d.barrier.complete()
		return fmt.Errorf("%w: billing dispatch is in status %q", ErrAccountShareBillingIntentStateConflict, d.state.Status)
	}
	state, err := d.repository.FailInFlightWithoutUsage(
		ctx,
		AccountShareBillingIntentTransition{
			ID:                 d.state.ID,
			ExpectedStateToken: d.state.StateToken,
		},
		strings.TrimSpace(errorCode),
		strings.TrimSpace(errorMessage),
	)
	if err != nil {
		d.barrier.complete()
		return err
	}
	if state == nil {
		d.barrier.complete()
		return ErrAccountShareBillingIntentNotFound
	}
	d.state = *state
	d.barrier.complete()
	return nil
}

func (d *AccountShareBillingDispatch) markReadyLocked(
	ctx context.Context,
	readyInput MarkAccountShareBillingIntentReadyInput,
) error {
	if d.state.Status == AccountShareBillingIntentStatusReady ||
		d.state.Status == AccountShareBillingIntentStatusProcessing ||
		d.state.Status == AccountShareBillingIntentStatusSettled {
		d.barrier.complete()
		return nil
	}
	if d.state.Status != AccountShareBillingIntentStatusInFlight {
		d.barrier.complete()
		return fmt.Errorf("%w: billing dispatch is in status %q", ErrAccountShareBillingIntentStateConflict, d.state.Status)
	}
	d.barrier.beginRecovery()
	retryDelay := 25 * time.Millisecond
	for {
		state, err := d.repository.MarkReady(ctx, readyInput)
		if err == nil {
			if state == nil {
				d.barrier.complete()
				return ErrAccountShareBillingIntentNotFound
			}
			d.state = *state
			d.barrier.complete()
			return nil
		}
		if isAccountShareBillingDispatchError(err) {
			d.barrier.complete()
			return err
		}
		timer := time.NewTimer(retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			d.scheduleReadyRecoveryLocked(readyInput)
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
		if retryDelay < time.Second {
			retryDelay *= 2
			if retryDelay > time.Second {
				retryDelay = time.Second
			}
		}
	}
}

func (d *AccountShareBillingDispatch) scheduleReadyRecoveryLocked(
	readyInput MarkAccountShareBillingIntentReadyInput,
) {
	if d == nil || d.recoveryScheduled {
		return
	}
	d.recoveryScheduled = true
	go d.recoverReadyUntilDurable(readyInput)
}

func (d *AccountShareBillingDispatch) recoverReadyUntilDurable(
	readyInput MarkAccountShareBillingIntentReadyInput,
) {
	if d == nil {
		return
	}
	recoveryCtx := d.recoveryCtx
	if recoveryCtx == nil {
		recoveryCtx = context.Background()
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Status == AccountShareBillingIntentStatusReady ||
		d.state.Status == AccountShareBillingIntentStatusProcessing ||
		d.state.Status == AccountShareBillingIntentStatusSettled {
		d.barrier.complete()
		return
	}
	if d.state.Status != AccountShareBillingIntentStatusInFlight {
		d.barrier.complete()
		return
	}

	attempt := 0
	for {
		if err := recoveryCtx.Err(); err != nil {
			return
		}
		attemptCtx, cancel := context.WithTimeout(recoveryCtx, accountShareBillingRecoveryAttemptTimeout)
		state, err := d.repository.MarkReady(attemptCtx, readyInput)
		cancel()
		if err == nil {
			if state == nil {
				log.Printf("account_share_billing: durable ready recovery returned no state intent_id=%d", d.state.ID)
				d.barrier.complete()
				return
			}
			d.state = *state
			d.barrier.complete()
			return
		}
		if isAccountShareBillingDispatchError(err) {
			log.Printf("account_share_billing: durable ready recovery stopped intent_id=%d err=%v", d.state.ID, err)
			d.barrier.complete()
			return
		}

		delay := accountShareBillingRecoveryDelay(d.state.ID, attempt)
		attempt++
		timer := time.NewTimer(delay)
		select {
		case <-recoveryCtx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func accountShareBillingRecoveryDelay(intentID int64, attempt int) time.Duration {
	delay := time.Second
	for i := 0; i < attempt && delay < accountShareBillingRecoveryMaxDelay; i++ {
		delay *= 2
		if delay > accountShareBillingRecoveryMaxDelay {
			delay = accountShareBillingRecoveryMaxDelay
		}
	}
	if intentID < 0 {
		intentID = -intentID
	}
	return delay + time.Duration(intentID%251)*time.Millisecond
}

func WithAccountShareBillingDispatch(ctx context.Context, dispatch *AccountShareBillingDispatch) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if dispatch == nil {
		return ctx
	}
	return context.WithValue(ctx, accountShareBillingDispatchContextKey{}, dispatch)
}

func WithAccountShareBillingDispatchFromContext(ctx context.Context, source context.Context) context.Context {
	if dispatch, ok := AccountShareBillingDispatchFromContext(source); ok {
		return WithAccountShareBillingDispatch(ctx, dispatch)
	}
	return ctx
}

func AccountShareBillingDispatchFromContext(ctx context.Context) (*AccountShareBillingDispatch, bool) {
	if ctx != nil {
		if dispatch, _ := ctx.Value(accountShareBillingDispatchContextKey{}).(*AccountShareBillingDispatch); dispatch != nil {
			return dispatch, true
		}
	}
	requestCtx, ok := AccountShareModeRequestFromContext(ctx)
	if !ok || requestCtx.state == nil {
		return nil, false
	}
	requestCtx.state.mu.RLock()
	defer requestCtx.state.mu.RUnlock()
	dispatch := requestCtx.state.billingDispatch
	return dispatch, dispatch != nil
}

func (s *AccountShareModeService) BeginAccountShareBillingDispatch(
	ctx context.Context,
	input AccountShareBillingDispatchInput,
) (*AccountShareBillingDispatch, error) {
	if s == nil || s.repo == nil || s.billingIntentRepository == nil {
		return nil, ErrServiceUnavailable
	}
	requestCtx, ok := AccountShareModeRequestFromContext(ctx)
	if !ok || requestCtx.state == nil {
		return nil, ErrAccountShareModeGroupUnbound
	}
	var err error
	if input.APIKey == nil {
		if s.apiKeyRepo == nil {
			return nil, fmt.Errorf("%w: api key repository is unavailable", ErrAccountShareBillingIntentInvalid)
		}
		input.APIKey, err = s.apiKeyRepo.GetByID(ctx, requestCtx.APIKeyID)
		if err != nil {
			return nil, err
		}
	}
	if input.User == nil {
		if s.userRepo == nil {
			return nil, fmt.Errorf("%w: user repository is unavailable", ErrAccountShareBillingIntentInvalid)
		}
		input.User, err = s.userRepo.GetByID(ctx, requestCtx.UserID)
		if err != nil {
			return nil, err
		}
	}
	if input.APIKey == nil || input.User == nil || input.Account == nil {
		return nil, fmt.Errorf("%w: billing principal is incomplete", ErrAccountShareBillingIntentInvalid)
	}
	if input.APIKey.ID != requestCtx.APIKeyID || input.User.ID != requestCtx.UserID ||
		input.APIKey.UserID != input.User.ID {
		return nil, fmt.Errorf("%w: billing principal does not match the routing context", ErrAccountShareBillingIntentInvalid)
	}
	if input.APIKey.GroupID == nil || *input.APIKey.GroupID <= 0 {
		return nil, ErrAccountShareModeGroupUnbound
	}
	if input.AttemptNo <= 0 {
		return nil, fmt.Errorf("%w: attempt_no must be positive", ErrAccountShareBillingIntentInvalid)
	}
	clientRequestID := strings.TrimSpace(input.ClientRequestID)
	if clientRequestID == "" {
		clientRequestID = accountShareBillingClientRequestID(ctx)
	}
	if err := validateAccountShareBillingIdentifier("client_request_id", clientRequestID, 255); err != nil {
		return nil, err
	}

	membership, listing, err := s.ResolveActiveBindingForRequest(
		ctx,
		input.User.ID,
		input.APIKey.ID,
		*input.APIKey.GroupID,
	)
	if err != nil {
		return nil, err
	}
	if membership == nil || listing == nil ||
		membership.Status != AccountShareMembershipStatusActive ||
		membership.AccountID != input.Account.ID ||
		listing.AccountID != input.Account.ID {
		return nil, ErrAccountShareBillingBindingUnavailable
	}
	runtimeBindingRepo, ok := s.repo.(AccountShareRuntimeBindingRepository)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	binding, err := runtimeBindingRepo.GetOpenMembershipRuntimeBinding(ctx, membership.ID, input.Account.ID)
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.BindingID <= 0 || binding.ListingRevisionID <= 0 ||
		binding.TermsRevisionNumber <= 0 {
		return nil, ErrAccountShareBillingBindingUnavailable
	}
	terms := membership.TermsSnapshot
	if terms == nil || terms.ListingRevisionID != binding.ListingRevisionID {
		return nil, fmt.Errorf("%w: immutable membership terms snapshot is unavailable", ErrAccountShareBillingBindingUnavailable)
	}

	command, actorRole, err := s.buildAccountShareBillingDispatchCommand(ctx, input, membership, listing)
	if err != nil {
		return nil, err
	}
	dispatchID := accountShareBillingDispatchID(clientRequestID, input.APIKey.ID, input.AttemptNo)
	preparedInput := CreateAccountShareBillingIntentInput{
		RequestID:           "dispatch:" + dispatchID,
		ClientRequestID:     clientRequestID,
		DispatchID:          dispatchID,
		AttemptNo:           input.AttemptNo,
		APIKeyID:            input.APIKey.ID,
		MembershipID:        membership.ID,
		ListingID:           listing.ID,
		AccountID:           input.Account.ID,
		BindingID:           binding.BindingID,
		ListingRevisionID:   binding.ListingRevisionID,
		TermsRevisionNumber: binding.TermsRevisionNumber,
		ActorUserID:         input.User.ID,
		ActorRole:           actorRole,
		ConsumerUserID:      membership.ConsumerUserID,
		OwnerUserID:         listing.OwnerUserID,
		Command:             command,
	}
	state, created, err := s.billingIntentRepository.CreatePrepared(ctx, preparedInput)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, ErrAccountShareBillingIntentNotFound
	}
	if !created && state.Status != AccountShareBillingIntentStatusCreated {
		return nil, fmt.Errorf("%w: dispatch already reached status %q", ErrAccountShareBillingIntentStateConflict, state.Status)
	}

	barrier := newAccountShareBillingReleaseBarrier(accountShareBillingReleaseBarrierTimeout)
	lease, _ := ctx.Value(accountShareRuntimeLeaseContextKey{}).(*AccountShareRuntimeLease)
	if lease == nil {
		s.cancelAccountShareBillingDispatchBeforeForward(ctx, state, "runtime_lease_missing")
		return nil, ErrAccountShareRuntimeLeaseUnavailable
	}
	if err := lease.setAccountShareBillingBarrier(barrier); err != nil {
		s.cancelAccountShareBillingDispatchBeforeForward(ctx, state, "runtime_lease_barrier_failed")
		return nil, err
	}
	state, err = s.billingIntentRepository.MarkInFlight(ctx, AccountShareBillingIntentTransition{
		ID:                 state.ID,
		ExpectedStateToken: state.StateToken,
	})
	if err != nil {
		barrier.complete()
		s.cancelAccountShareBillingDispatchBeforeForward(ctx, state, "mark_in_flight_failed")
		return nil, err
	}
	if state == nil {
		barrier.complete()
		return nil, ErrAccountShareBillingIntentNotFound
	}
	dispatch := &AccountShareBillingDispatch{
		repository:  s.billingIntentRepository,
		barrier:     barrier,
		command:     command,
		recoveryCtx: lease.Context(),
		state:       *state,
	}
	requestCtx.state.mu.Lock()
	requestCtx.state.billingDispatch = dispatch
	requestCtx.state.mu.Unlock()
	return dispatch, nil
}

func (s *AccountShareModeService) buildAccountShareBillingDispatchCommand(
	ctx context.Context,
	input AccountShareBillingDispatchInput,
	membership *AccountShareMembership,
	listing *AccountShareListing,
) (AccountShareBillingCommand, string, error) {
	if membership == nil || listing == nil || membership.TermsSnapshot == nil {
		return AccountShareBillingCommand{}, "", ErrAccountShareBillingBindingUnavailable
	}
	requestedModel := strings.TrimSpace(input.RequestedModel)
	routedModel := strings.TrimSpace(input.RoutedModel)
	if requestedModel == "" || routedModel == "" {
		return AccountShareBillingCommand{}, "", fmt.Errorf("%w: requested and routed model are required", ErrAccountShareBillingIntentInvalid)
	}
	if input.RequestType == RequestTypeUnknown {
		return AccountShareBillingCommand{}, "", fmt.Errorf("%w: request_type is required", ErrAccountShareBillingIntentInvalid)
	}
	groupID := *input.APIKey.GroupID
	billingModel := accountShareBillingModelForDispatch(input.ChannelUsageFields, requestedModel, routedModel)
	if s.modelPricingResolver == nil {
		return AccountShareBillingCommand{}, "", ErrServiceUnavailable
	}
	if !s.modelPricingResolver.HasConfiguredPricing(ctx, PricingInput{Model: billingModel, GroupID: &groupID}) {
		return AccountShareBillingCommand{}, "", fmt.Errorf(
			"%w for account share billing model: %s",
			ErrModelPricingUnavailable,
			billingModel,
		)
	}
	var subscriptionID *int64
	billingType := BillingTypeBalance
	if input.APIKey.Group != nil && input.APIKey.Group.IsSubscriptionType() {
		if input.Subscription == nil || input.Subscription.ID <= 0 {
			return AccountShareBillingCommand{}, "", ErrSubscriptionNotFound
		}
		billingType = BillingTypeSubscription
		id := input.Subscription.ID
		subscriptionID = &id
	}

	ownerSelfUse := IsAccountShareModeOwnerSelfUse(membership, listing)
	rateMultiplier := membership.TermsSnapshot.RateMultiplier
	actorRole := "consumer"
	if ownerSelfUse {
		actorRole = "owner"
		var err error
		rateMultiplier, err = s.ResolveOwnerSelfUseMultiplier(ctx)
		if err != nil {
			return AccountShareBillingCommand{}, "", err
		}
	}
	policy, err := s.ResolvePolicy(ctx)
	if err != nil {
		return AccountShareBillingCommand{}, "", err
	}
	ownerRatio, inviteRatio, platformRatio := accountShareBillingRevenueRatios(0, 0)
	var policyID *int64
	policyVersion := 0
	if !ownerSelfUse && policy != nil {
		id := policy.ID
		policyID = &id
		policyVersion = policy.Version
		ownerRatio, inviteRatio, platformRatio = accountShareBillingRevenueRatios(
			policy.OwnerShareRatio,
			policy.InviteShareRatio,
		)
	}
	channelID := optionalPositiveAccountShareBillingID(input.ChannelUsageFields.ChannelID)
	command := AccountShareBillingCommand{
		SchemaVersion:         AccountShareBillingCommandSchemaV3,
		RequestPayloadHash:    strings.ToLower(strings.TrimSpace(input.RequestPayloadHash)),
		GroupID:               &groupID,
		SubscriptionID:        subscriptionID,
		AccountType:           input.Account.Type,
		RequestedModel:        requestedModel,
		RoutedModel:           routedModel,
		InboundEndpoint:       strings.TrimSpace(input.InboundEndpoint),
		UpstreamEndpoint:      strings.TrimSpace(input.UpstreamEndpoint),
		RequestType:           input.RequestType.String(),
		ServiceTier:           strings.TrimSpace(input.ServiceTier),
		ReasoningEffort:       strings.TrimSpace(input.ReasoningEffort),
		BillingType:           int16(billingType),
		PreferPointsBilling:   input.User.PreferPointsBilling,
		RateMultiplier:        accountShareBillingDecimal(rateMultiplier),
		RateMultiplierSource:  RateMultiplierSourceAccountShare,
		AccountRateMultiplier: accountShareBillingDecimal(input.Account.BillingRateMultiplier()),
		HourlyRate:            accountShareBillingDecimal(membership.HourlyRateSnapshot),
		OwnerShareRatio:       ownerRatio,
		InviteShareRatio:      inviteRatio,
		PlatformShareRatio:    platformRatio,
		PolicyID:              policyID,
		PolicyVersion:         policyVersion,
		ChannelID:             channelID,
		ModelMappingChain:     strings.TrimSpace(input.ChannelUsageFields.ModelMappingChain),
		SettlementEnabled:     !ownerSelfUse,
		ShareModeSnapshot:     NormalizeAccountShareMode(input.Account.ShareMode),
		ShareStatusSnapshot:   NormalizeAccountShareStatus(input.Account.ShareStatus),
		SharePlatformSnapshot: strings.TrimSpace(input.Account.Platform),
	}
	if _, err := normalizeAccountShareBillingCommand(command); err != nil {
		return AccountShareBillingCommand{}, "", err
	}
	return command, actorRole, nil
}

func accountShareBillingRevenueRatios(ownerRatio, inviteRatio float64) (string, string, string) {
	owner := decimal.NewFromFloat(ownerRatio)
	invite := decimal.NewFromFloat(inviteRatio)
	platform := decimal.NewFromInt(1).Sub(owner).Sub(invite)
	if platform.IsNegative() {
		platform = decimal.Zero
	}
	return owner.String(), invite.String(), platform.String()
}

func accountShareBillingModelForDispatch(fields ChannelUsageFields, requestedModel, routedModel string) string {
	switch strings.TrimSpace(fields.BillingModelSource) {
	case BillingModelSourceRequested:
		if model := strings.TrimSpace(fields.OriginalModel); model != "" {
			return model
		}
		if model := strings.TrimSpace(requestedModel); model != "" {
			return model
		}
	case BillingModelSourceChannelMapped:
		if model := strings.TrimSpace(fields.ChannelMappedModel); model != "" {
			return model
		}
	}
	if model := strings.TrimSpace(routedModel); model != "" {
		return model
	}
	return strings.TrimSpace(requestedModel)
}

func buildAccountShareBillingIntentReadyInput(
	state AccountShareBillingIntentState,
	dispatchCommand AccountShareBillingCommand,
	command *UsageBillingCommand,
) (MarkAccountShareBillingIntentReadyInput, error) {
	if command == nil || command.UsageLog == nil {
		return MarkAccountShareBillingIntentReadyInput{}, fmt.Errorf("%w: usage billing command is incomplete", ErrAccountShareBillingIntentInvalid)
	}
	if state.ID <= 0 || state.StateToken <= 0 ||
		state.APIKeyID != command.APIKeyID ||
		state.MembershipID <= 0 ||
		command.UserID <= 0 ||
		command.AccountID <= 0 {
		return MarkAccountShareBillingIntentReadyInput{}, fmt.Errorf("%w: durable billing identity mismatch", ErrAccountShareBillingIntentInvalid)
	}
	log := command.UsageLog
	durationMilliseconds := int64Value(log.DurationMs)
	firstTokenMilliseconds := int64PointerFromInt(log.FirstTokenMs)
	videoDurationSeconds := int64PointerFromInt(log.VideoDurationSeconds)
	accountStatsCost, err := optionalAccountShareBillingDecimal(log.AccountStatsCost)
	if err != nil {
		return MarkAccountShareBillingIntentReadyInput{}, err
	}
	baseCharge, hourlyCharge, totalCharge := 0.0, 0.0, 0.0
	if dispatchCommand.SettlementEnabled {
		baseCharge = log.ActualCost
		totalCharge = baseCharge
	}
	usage := AccountShareBillingUsagePayloadV2{
		SchemaVersion:              AccountShareBillingUsageSchemaV2,
		UsageOccurredAt:            command.UsageOccurredAt,
		Model:                      strings.TrimSpace(log.Model),
		UpstreamModel:              dereferenceString(log.UpstreamModel),
		ServiceTier:                dereferenceString(log.ServiceTier),
		ReasoningEffort:            dereferenceString(log.ReasoningEffort),
		InputTokens:                int64(log.InputTokens),
		OutputTokens:               int64(log.OutputTokens),
		CacheCreationTokens:        int64(log.CacheCreationTokens),
		CacheCreation5mTokens:      int64(log.CacheCreation5mTokens),
		CacheCreation1hTokens:      int64(log.CacheCreation1hTokens),
		CacheReadTokens:            int64(log.CacheReadTokens),
		ImageInputTokens:           int64(log.ImageInputTokens),
		ImageOutputTokens:          int64(log.ImageOutputTokens),
		ImageCount:                 int64(log.ImageCount),
		ImageSize:                  dereferenceString(log.ImageSize),
		MediaType:                  dereferenceString(log.MediaType),
		VideoCount:                 int64(log.VideoCount),
		VideoResolution:            dereferenceString(log.VideoResolution),
		VideoDurationSeconds:       videoDurationSeconds,
		DurationMilliseconds:       durationMilliseconds,
		FirstTokenMilliseconds:     firstTokenMilliseconds,
		BillingTier:                dereferenceString(log.BillingTier),
		BillingMode:                dereferenceString(log.BillingMode),
		CacheTTLOverridden:         log.CacheTTLOverridden,
		AppliedRateMultiplier:      accountShareBillingDecimal(log.RateMultiplier),
		InputCost:                  accountShareBillingDecimal(log.InputCost),
		OutputCost:                 accountShareBillingDecimal(log.OutputCost),
		CacheCreationCost:          accountShareBillingDecimal(log.CacheCreationCost),
		CacheReadCost:              accountShareBillingDecimal(log.CacheReadCost),
		ImageInputCost:             accountShareBillingDecimal(log.ImageInputCost),
		ImageOutputCost:            accountShareBillingDecimal(log.ImageOutputCost),
		TotalCost:                  accountShareBillingDecimal(log.TotalCost),
		ActualCost:                 accountShareBillingDecimal(log.ActualCost),
		AccountStatsCost:           accountStatsCost,
		BalanceCost:                accountShareBillingDecimal(command.BalanceCost),
		SubscriptionCost:           accountShareBillingDecimal(command.SubscriptionCost),
		PrivateGroupCommissionCost: accountShareBillingDecimal(command.PrivateGroupCommissionCost),
		APIKeyQuotaCost:            accountShareBillingDecimal(command.APIKeyQuotaCost),
		APIKeyRateLimitCost:        accountShareBillingDecimal(command.APIKeyRateLimitCost),
		AccountQuotaCost:           accountShareBillingDecimal(command.AccountQuotaCost),
		BaseCharge:                 accountShareBillingDecimal(baseCharge),
		HourlyCharge:               accountShareBillingDecimal(hourlyCharge),
		TotalCharge:                accountShareBillingDecimal(totalCharge),
	}
	if usage.UsageOccurredAt.IsZero() {
		usage.UsageOccurredAt = time.Now().UTC()
	}
	if _, err := normalizeAccountShareBillingUsage(usage); err != nil {
		return MarkAccountShareBillingIntentReadyInput{}, err
	}
	return MarkAccountShareBillingIntentReadyInput{
		AccountShareBillingIntentTransition: AccountShareBillingIntentTransition{
			ID:                 state.ID,
			ExpectedStateToken: state.StateToken,
		},
		Usage: usage,
		ResponseSummary: AccountShareBillingResponseSummaryV1{
			SchemaVersion:     AccountShareBillingResponseSchemaV1,
			HTTPStatus:        200,
			ProviderRequestID: strings.TrimSpace(log.RequestID),
			Streamed:          log.RequestType == RequestTypeStream || log.RequestType == RequestTypeWSV2 || log.Stream,
		},
	}, nil
}

func accountShareBillingCommandFromContext(ctx context.Context) (AccountShareBillingCommand, bool) {
	dispatch, ok := AccountShareBillingDispatchFromContext(ctx)
	if !ok {
		return AccountShareBillingCommand{}, false
	}
	dispatch.mu.Lock()
	defer dispatch.mu.Unlock()
	return dispatch.command, true
}

func accountShareBillingRateMultiplierFromContext(ctx context.Context) (float64, bool, error) {
	command, ok := accountShareBillingCommandFromContext(ctx)
	if !ok {
		return 0, false, nil
	}
	value, err := decimal.NewFromString(strings.TrimSpace(command.RateMultiplier))
	if err != nil || value.IsNegative() {
		return 0, true, fmt.Errorf(
			"%w: durable rate multiplier is invalid",
			ErrAccountShareBillingIntentInvalid,
		)
	}
	multiplier, _ := value.Float64()
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) || (!value.IsZero() && multiplier == 0) {
		return 0, true, fmt.Errorf(
			"%w: durable rate multiplier is outside float64 range",
			ErrAccountShareBillingIntentInvalid,
		)
	}
	return multiplier, true, nil
}

func accountShareBillingClientRequestID(ctx context.Context) string {
	if ctx != nil {
		if value, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
		if value, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(value) != "" {
			return "local:" + strings.TrimSpace(value)
		}
	}
	return ""
}

func accountShareBillingDispatchID(clientRequestID string, apiKeyID int64, attemptNo int) string {
	identity := fmt.Sprintf("%s\n%d\n%d", strings.TrimSpace(clientRequestID), apiKeyID, attemptNo)
	return uuid.NewSHA1(accountShareBillingDispatchNamespace, []byte(identity)).String()
}

func (s *AccountShareModeService) cancelAccountShareBillingDispatchBeforeForward(
	ctx context.Context,
	state *AccountShareBillingIntentState,
	reasonCode string,
) {
	if s == nil || s.billingIntentRepository == nil || state == nil ||
		state.Status != AccountShareBillingIntentStatusCreated || state.ID <= 0 || state.StateToken <= 0 {
		return
	}
	if _, err := s.billingIntentRepository.CancelCreated(ctx, AccountShareBillingIntentTransition{
		ID:                 state.ID,
		ExpectedStateToken: state.StateToken,
	}, reasonCode, "upstream dispatch was not started"); err != nil {
		log.Printf(
			"account_share_billing: cancel created dispatch deferred to stale recovery intent_id=%d err=%v",
			state.ID,
			err,
		)
	}
}

func accountShareBillingDecimal(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return "-1"
	}
	return decimal.NewFromFloat(value).String()
}

func optionalAccountShareBillingDecimal(value *float64) (*string, error) {
	if value == nil {
		return nil, nil
	}
	encoded := accountShareBillingDecimal(*value)
	if encoded == "-1" {
		return nil, fmt.Errorf("%w: account_stats_cost is invalid", ErrAccountShareBillingIntentInvalid)
	}
	return &encoded, nil
}

func optionalPositiveAccountShareBillingID(value int64) *int64 {
	if value <= 0 {
		return nil
	}
	return &value
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func int64Value(value *int) int64 {
	if value == nil {
		return 0
	}
	return int64(*value)
}

func int64PointerFromInt(value *int) *int64 {
	if value == nil {
		return nil
	}
	out := int64(*value)
	return &out
}

func (s *GatewayService) BeginAccountShareBillingDispatch(
	ctx context.Context,
	input AccountShareBillingDispatchInput,
) (*AccountShareBillingDispatch, error) {
	if s == nil || s.accountShareModeService == nil {
		return nil, ErrServiceUnavailable
	}
	return s.accountShareModeService.BeginAccountShareBillingDispatch(ctx, input)
}

func (s *OpenAIGatewayService) BeginAccountShareBillingDispatch(
	ctx context.Context,
	input AccountShareBillingDispatchInput,
) (*AccountShareBillingDispatch, error) {
	if s == nil || s.accountShareModeService == nil {
		return nil, ErrServiceUnavailable
	}
	return s.accountShareModeService.BeginAccountShareBillingDispatch(ctx, input)
}

func markAccountShareBillingDispatchReady(ctx context.Context, command *UsageBillingCommand) (bool, error) {
	dispatch, ok := AccountShareBillingDispatchFromContext(ctx)
	if !ok {
		return false, nil
	}
	if command == nil {
		dispatch.barrier.complete()
		return true, fmt.Errorf("%w: usage billing command is nil", ErrAccountShareBillingIntentInvalid)
	}
	return true, dispatch.markReady(ctx, command)
}

func FailAccountShareBillingDispatchWithoutUsage(
	ctx context.Context,
	errorCode string,
	errorMessage string,
) (bool, error) {
	dispatch, ok := AccountShareBillingDispatchFromContext(ctx)
	if !ok {
		return false, nil
	}
	billingCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	return true, dispatch.failWithoutUsage(billingCtx, errorCode, errorMessage)
}

func isAccountShareBillingDispatchError(err error) bool {
	return errors.Is(err, ErrAccountShareBillingIntentInvalid) ||
		errors.Is(err, ErrAccountShareBillingIntentNotFound) ||
		errors.Is(err, ErrAccountShareBillingIntentConflict) ||
		errors.Is(err, ErrAccountShareBillingIntentStateConflict) ||
		errors.Is(err, ErrAccountShareBillingBindingUnavailable) ||
		errors.Is(err, ErrAccountShareBillingPreTerminalCommit)
}

const (
	accountShareBillingRecordUsageMaxAttempts = 3
	accountShareBillingRecordUsageRetryBase   = 50 * time.Millisecond
)

// retryAccountShareBillingRecordUsage adds a small bounded retry around the
// asynchronous usage-recording path used by durable account-share dispatches.
// Other billing modes preserve their existing single-attempt behavior.
func retryAccountShareBillingRecordUsage(
	ctx context.Context,
	operation func(context.Context) error,
) error {
	if operation == nil {
		return fmt.Errorf("%w: record usage operation is required", ErrAccountShareBillingIntentInvalid)
	}
	if _, durable := AccountShareBillingDispatchFromContext(ctx); !durable {
		return operation(ctx)
	}

	var lastErr error
	for attempt := 1; attempt <= accountShareBillingRecordUsageMaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = operation(ctx)
		if lastErr == nil || !isRetryableAccountShareBillingRecordUsageError(lastErr) {
			return lastErr
		}
		if attempt == accountShareBillingRecordUsageMaxAttempts {
			break
		}

		delay := accountShareBillingRecordUsageRetryBase * time.Duration(1<<(attempt-1))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func isRetryableAccountShareBillingRecordUsageError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrModelPricingUnavailable) ||
		errors.Is(err, ErrSubscriptionNotFound) ||
		errors.Is(err, ErrNoAvailableAccounts) ||
		errors.Is(err, ErrAccountShareMembershipNotFound) ||
		errors.Is(err, ErrAccountNotFound) ||
		errors.Is(err, ErrAPIKeyNotFound) ||
		errors.Is(err, ErrUserNotFound) ||
		errors.Is(err, ErrServiceUnavailable) ||
		isAccountShareBillingDispatchError(err) {
		return false
	}
	return true
}
