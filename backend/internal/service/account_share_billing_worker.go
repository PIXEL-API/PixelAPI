package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	AccountShareBillingWorkerDefaultMaxAttempts     = 8
	AccountShareBillingWorkerDefaultLeaseDuration   = 30 * time.Second
	AccountShareBillingWorkerDefaultRetryBase       = 5 * time.Second
	AccountShareBillingWorkerDefaultRetryMax        = 5 * time.Minute
	AccountShareBillingWorkerDefaultDrainSoftBudget = 10 * time.Second
	AccountShareBillingWorkerMaxDrainBatches        = 20
	accountShareBillingWorkerFingerprintNamespace   = "account-share-billing-v2"
)

var ErrAccountShareBillingWorkerInvalid = errors.New("invalid account share billing worker configuration")

type AccountShareBillingWorkerConfig struct {
	WorkerID      string
	BatchSize     int
	LeaseDuration time.Duration
	MaxAttempts   int
	RetryBase     time.Duration
	RetryMax      time.Duration
}

type AccountShareBillingWorkerRunResult struct {
	Batches         int
	Claimed         int
	Settled         int
	RetryScheduled  int
	NeedsAttention  int
	BudgetExhausted bool
	BacklogLikely   bool
}

type AccountShareBillingWorker struct {
	intentRepository    AccountShareBillingIntentRepository
	billingRepository   UsageBillingRepository
	postCommitFinalizer UsageBillingPostCommitFinalizer
	config              AccountShareBillingWorkerConfig
	now                 func() time.Time
}

func NewAccountShareBillingWorker(
	intentRepository AccountShareBillingIntentRepository,
	billingRepository UsageBillingRepository,
	postCommitFinalizer UsageBillingPostCommitFinalizer,
	config AccountShareBillingWorkerConfig,
) (*AccountShareBillingWorker, error) {
	if intentRepository == nil || billingRepository == nil || postCommitFinalizer == nil {
		return nil, fmt.Errorf("%w: repositories and post-commit finalizer are required", ErrAccountShareBillingWorkerInvalid)
	}
	config.WorkerID = strings.TrimSpace(config.WorkerID)
	if config.LeaseDuration == 0 {
		config.LeaseDuration = AccountShareBillingWorkerDefaultLeaseDuration
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = AccountShareBillingWorkerDefaultMaxAttempts
	}
	if config.RetryBase == 0 {
		config.RetryBase = AccountShareBillingWorkerDefaultRetryBase
	}
	if config.RetryMax == 0 {
		config.RetryMax = AccountShareBillingWorkerDefaultRetryMax
	}
	if config.MaxAttempts < 1 || config.RetryBase < 0 || config.RetryMax < 0 {
		return nil, fmt.Errorf("%w: max_attempts must be positive", ErrAccountShareBillingWorkerInvalid)
	}
	if config.RetryMax < config.RetryBase {
		return nil, fmt.Errorf("%w: retry_max must not be shorter than retry_base", ErrAccountShareBillingWorkerInvalid)
	}
	claim, err := NormalizeAccountShareBillingClaim(ClaimAccountShareBillingIntentsInput{
		WorkerID:      config.WorkerID,
		Limit:         config.BatchSize,
		LeaseDuration: config.LeaseDuration,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAccountShareBillingWorkerInvalid, err)
	}
	config.WorkerID = claim.WorkerID
	config.BatchSize = claim.Limit
	config.LeaseDuration = claim.LeaseDuration
	return &AccountShareBillingWorker{
		intentRepository:    intentRepository,
		billingRepository:   billingRepository,
		postCommitFinalizer: postCommitFinalizer,
		config:              config,
		now:                 time.Now,
	}, nil
}

func (w *AccountShareBillingWorker) RunOnce(ctx context.Context) (AccountShareBillingWorkerRunResult, error) {
	var runResult AccountShareBillingWorkerRunResult
	if w == nil || w.intentRepository == nil || w.billingRepository == nil || w.postCommitFinalizer == nil || w.now == nil {
		return runResult, fmt.Errorf("%w: worker is not initialized", ErrAccountShareBillingWorkerInvalid)
	}
	if err := ctx.Err(); err != nil {
		return runResult, err
	}
	items, err := w.intentRepository.ClaimReady(ctx, ClaimAccountShareBillingIntentsInput{
		WorkerID:      w.config.WorkerID,
		Limit:         w.config.BatchSize,
		LeaseDuration: w.config.LeaseDuration,
	})
	if err != nil {
		return runResult, err
	}
	runResult.Claimed = len(items)

	var processErrors []error
	for i := range items {
		outcome, processErr := w.processClaimed(ctx, &items[i])
		switch outcome {
		case accountShareBillingWorkerOutcomeSettled:
			runResult.Settled++
		case accountShareBillingWorkerOutcomeRetryScheduled:
			runResult.RetryScheduled++
		case accountShareBillingWorkerOutcomeNeedsAttention:
			runResult.NeedsAttention++
		}
		if processErr != nil {
			processErrors = append(processErrors, fmt.Errorf("process billing intent %d: %w", items[i].ID, processErr))
		}
		if err := ctx.Err(); err != nil {
			processErrors = append(processErrors, err)
			break
		}
	}
	return runResult, errors.Join(processErrors...)
}

func (w *AccountShareBillingWorker) RunUntilDrained(
	ctx context.Context,
	softBudget time.Duration,
	beforeBatch func(context.Context) error,
) (AccountShareBillingWorkerRunResult, error) {
	var result AccountShareBillingWorkerRunResult
	if w == nil || w.intentRepository == nil || w.billingRepository == nil || w.postCommitFinalizer == nil || w.now == nil {
		return result, fmt.Errorf("%w: worker is not initialized", ErrAccountShareBillingWorkerInvalid)
	}
	if beforeBatch == nil {
		return result, fmt.Errorf("%w: before_batch is required", ErrAccountShareBillingWorkerInvalid)
	}
	if softBudget < 0 {
		return result, fmt.Errorf("%w: soft_budget must not be negative", ErrAccountShareBillingWorkerInvalid)
	}
	if softBudget == 0 {
		softBudget = AccountShareBillingWorkerDefaultDrainSoftBudget
	}

	startedAt := w.now()
	for result.Batches < AccountShareBillingWorkerMaxDrainBatches {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if result.Batches > 0 && w.now().Sub(startedAt) >= softBudget {
			result.BudgetExhausted = true
			result.BacklogLikely = true
			return result, nil
		}
		if err := beforeBatch(ctx); err != nil {
			return result, err
		}

		batch, err := w.RunOnce(ctx)
		result.Batches++
		result.Claimed += batch.Claimed
		result.Settled += batch.Settled
		result.RetryScheduled += batch.RetryScheduled
		result.NeedsAttention += batch.NeedsAttention
		result.BacklogLikely = batch.Claimed >= w.config.BatchSize
		if err != nil {
			return result, err
		}
		if !result.BacklogLikely {
			return result, nil
		}
	}
	return result, nil
}

type accountShareBillingWorkerOutcome int

const (
	accountShareBillingWorkerOutcomeNone accountShareBillingWorkerOutcome = iota
	accountShareBillingWorkerOutcomeSettled
	accountShareBillingWorkerOutcomeRetryScheduled
	accountShareBillingWorkerOutcomeNeedsAttention
)

func (w *AccountShareBillingWorker) processClaimed(
	ctx context.Context,
	item *AccountShareBillingIntentWorkItem,
) (accountShareBillingWorkerOutcome, error) {
	if item == nil {
		return accountShareBillingWorkerOutcomeNone, fmt.Errorf("%w: claimed work item is nil", ErrAccountShareBillingIntentInvalid)
	}
	if item.Status != AccountShareBillingIntentStatusProcessing ||
		item.StateToken <= 0 ||
		item.LeaseToken <= 0 ||
		strings.TrimSpace(item.LeaseOwner) != w.config.WorkerID {
		return accountShareBillingWorkerOutcomeNone, ErrAccountShareBillingIntentLeaseLost
	}
	transition := AccountShareBillingIntentLeaseTransition{
		ID:                 item.ID,
		ExpectedStateToken: item.StateToken,
		LeaseToken:         item.LeaseToken,
		WorkerID:           w.config.WorkerID,
	}

	command, err := BuildAccountShareUsageBillingCommand(*item)
	if err != nil {
		return w.markNeedsAttention(ctx, transition, "billing_payload_invalid", "durable billing payload is invalid")
	}

	applyResult, applyErr, heartbeatErr := w.applyWithLeaseHeartbeat(ctx, transition, command)
	if heartbeatErr != nil {
		return accountShareBillingWorkerOutcomeNone, heartbeatErr
	}
	if err := ctx.Err(); err != nil {
		return accountShareBillingWorkerOutcomeNone, err
	}
	if applyErr != nil {
		return w.handleApplyFailure(ctx, item.AttemptCount, transition, applyErr)
	}
	if applyResult == nil {
		return w.markNeedsAttention(ctx, transition, "billing_result_invalid", "billing repository returned no result")
	}

	_, err = w.intentRepository.MarkSettled(ctx, MarkAccountShareBillingIntentSettledInput{
		AccountShareBillingIntentLeaseTransition: transition,
		UsageLogID:                               applyResult.UsageLogID,
	})
	if err != nil {
		// Apply may already have committed. Never downgrade the intent here:
		// the fenced lease must expire and replay the idempotent command.
		return accountShareBillingWorkerOutcomeNone, err
	}
	return accountShareBillingWorkerOutcomeSettled, nil
}

func (w *AccountShareBillingWorker) applyWithLeaseHeartbeat(
	ctx context.Context,
	transition AccountShareBillingIntentLeaseTransition,
	command *UsageBillingCommand,
) (*UsageBillingApplyResult, error, error) {
	applyCtx, cancelApply := context.WithCancel(ctx)
	defer cancelApply()
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan error, 1)
	go func() {
		heartbeatDone <- w.renewLeaseUntilStopped(applyCtx, transition, stopHeartbeat, cancelApply)
	}()

	result, applyErr := w.billingRepository.Apply(applyCtx, command)
	if applyErr == nil && result != nil {
		applyErr = w.postCommitFinalizer.Finalize(applyCtx, command, result)
	}
	close(stopHeartbeat)
	heartbeatErr := <-heartbeatDone
	if errors.Is(heartbeatErr, context.Canceled) && ctx.Err() == nil {
		heartbeatErr = nil
	}
	return result, applyErr, heartbeatErr
}

func (w *AccountShareBillingWorker) renewLeaseUntilStopped(
	ctx context.Context,
	transition AccountShareBillingIntentLeaseTransition,
	stop <-chan struct{},
	cancelApply context.CancelFunc,
) error {
	interval := w.config.LeaseDuration / 3
	if interval <= 0 {
		return fmt.Errorf("%w: lease heartbeat interval is invalid", ErrAccountShareBillingWorkerInvalid)
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			if _, err := w.intentRepository.RenewProcessingLease(ctx, transition, w.config.LeaseDuration); err != nil {
				cancelApply()
				return err
			}
			timer.Reset(interval)
		}
	}
}

func (w *AccountShareBillingWorker) handleApplyFailure(
	ctx context.Context,
	attemptCount int,
	transition AccountShareBillingIntentLeaseTransition,
	applyErr error,
) (accountShareBillingWorkerOutcome, error) {
	switch {
	case errors.Is(applyErr, ErrUsageBillingRequestConflict):
		return w.markNeedsAttention(ctx, transition, "billing_fingerprint_conflict", "billing idempotency fingerprint conflicts with the committed request")
	case errors.Is(applyErr, ErrAccountShareBillingSnapshotMismatch):
		return w.markNeedsAttention(ctx, transition, "billing_snapshot_conflict", "billing snapshot conflicts with the settlement target")
	case errors.Is(applyErr, ErrAccountShareMembershipNotFound),
		errors.Is(applyErr, ErrAccountNotFound),
		errors.Is(applyErr, ErrAPIKeyNotFound),
		errors.Is(applyErr, ErrUserNotFound),
		errors.Is(applyErr, ErrSubscriptionNotFound):
		return w.markNeedsAttention(ctx, transition, "billing_reference_missing", "a billing snapshot reference is no longer available")
	case errors.Is(applyErr, ErrAccountShareBillingIntentInvalid),
		errors.Is(applyErr, ErrAccountShareBillingIntentConflict):
		return w.markNeedsAttention(ctx, transition, "billing_payload_invalid", "durable billing payload cannot be applied")
	}
	if attemptCount >= w.config.MaxAttempts {
		return w.markNeedsAttention(ctx, transition, "billing_retry_exhausted", "temporary billing failures exceeded the retry limit")
	}
	retryAt := w.now().UTC().Add(w.retryDelay(attemptCount))
	_, err := w.intentRepository.MarkFailed(ctx, MarkAccountShareBillingIntentFailedInput{
		AccountShareBillingIntentLeaseTransition: transition,
		ErrorCode:                                "billing_apply_temporary",
		ErrorMessage:                             "billing settlement failed temporarily",
		RetryAt:                                  &retryAt,
	})
	if err != nil {
		return accountShareBillingWorkerOutcomeNone, err
	}
	return accountShareBillingWorkerOutcomeRetryScheduled, nil
}

func (w *AccountShareBillingWorker) markNeedsAttention(
	ctx context.Context,
	transition AccountShareBillingIntentLeaseTransition,
	code string,
	message string,
) (accountShareBillingWorkerOutcome, error) {
	_, err := w.intentRepository.MarkFailed(ctx, MarkAccountShareBillingIntentFailedInput{
		AccountShareBillingIntentLeaseTransition: transition,
		ErrorCode:                                code,
		ErrorMessage:                             message,
		NeedsAttention:                           true,
	})
	if err != nil {
		return accountShareBillingWorkerOutcomeNone, err
	}
	return accountShareBillingWorkerOutcomeNeedsAttention, nil
}

func (w *AccountShareBillingWorker) retryDelay(attemptCount int) time.Duration {
	delay := w.config.RetryBase
	for attempt := 1; attempt < attemptCount && delay < w.config.RetryMax; attempt++ {
		if delay > w.config.RetryMax/2 {
			return w.config.RetryMax
		}
		delay *= 2
	}
	if delay > w.config.RetryMax {
		return w.config.RetryMax
	}
	return delay
}

func BuildAccountShareUsageBillingCommand(item AccountShareBillingIntentWorkItem) (*UsageBillingCommand, error) {
	for name, value := range map[string]int64{
		"intent_id":             item.ID,
		"api_key_id":            item.APIKeyID,
		"membership_id":         item.MembershipID,
		"listing_id":            item.ListingID,
		"account_id":            item.AccountID,
		"binding_id":            item.BindingID,
		"listing_revision_id":   item.ListingRevisionID,
		"terms_revision_number": item.TermsRevisionNumber,
		"consumer_user_id":      item.ConsumerUserID,
		"owner_user_id":         item.OwnerUserID,
	} {
		if value <= 0 {
			return nil, fmt.Errorf("%w: %s must be positive", ErrAccountShareBillingIntentInvalid, name)
		}
	}
	requestID := strings.TrimSpace(item.RequestID)
	if err := validateAccountShareBillingIdentifier("request_id", requestID, 255); err != nil {
		return nil, err
	}
	command, err := normalizePersistedAccountShareBillingCommand(item.Command)
	if err != nil {
		return nil, err
	}
	usage, err := normalizeAccountShareBillingUsage(item.Usage)
	if err != nil {
		return nil, err
	}
	commandHash, err := canonicalAccountShareBillingCommandHash(command)
	if err != nil {
		return nil, err
	}
	usageHash, err := canonicalAccountShareBillingHash(usage)
	if err != nil {
		return nil, err
	}
	requestFingerprint, err := buildAccountShareBillingRequestFingerprint(CreateAccountShareBillingIntentInput{
		RequestID:           requestID,
		ClientRequestID:     item.ClientRequestID,
		DispatchID:          item.DispatchID,
		AttemptNo:           item.AttemptNo,
		APIKeyID:            item.APIKeyID,
		MembershipID:        item.MembershipID,
		ListingID:           item.ListingID,
		AccountID:           item.AccountID,
		BindingID:           item.BindingID,
		ListingRevisionID:   item.ListingRevisionID,
		TermsRevisionNumber: item.TermsRevisionNumber,
		ActorUserID:         item.ActorUserID,
		ActorRole:           item.ActorRole,
		ConsumerUserID:      item.ConsumerUserID,
		OwnerUserID:         item.OwnerUserID,
	}, commandHash)
	if err != nil {
		return nil, err
	}
	if commandHash != strings.TrimSpace(item.CommandHash) ||
		usageHash != strings.TrimSpace(item.UsageHash) ||
		requestFingerprint != strings.TrimSpace(item.RequestFingerprint) {
		return nil, fmt.Errorf("%w: durable billing snapshot hash mismatch", ErrAccountShareBillingIntentInvalid)
	}

	requestType, err := ParseUsageRequestType(command.RequestType)
	if err != nil || requestType == RequestTypeUnknown {
		return nil, fmt.Errorf("%w: invalid request_type", ErrAccountShareBillingIntentInvalid)
	}
	stream, openAIWSMode := ApplyLegacyRequestFields(requestType, false, false)

	rateMultiplier, err := accountShareBillingFloat64("rate_multiplier", command.RateMultiplier)
	if err != nil {
		return nil, err
	}
	accountRateMultiplier, err := accountShareBillingFloat64("account_rate_multiplier", command.AccountRateMultiplier)
	if err != nil {
		return nil, err
	}
	hourlyRate, err := accountShareBillingFloat64("hourly_rate", command.HourlyRate)
	if err != nil {
		return nil, err
	}
	ownerShareRatio, err := accountShareBillingFloat64("owner_share_ratio", command.OwnerShareRatio)
	if err != nil {
		return nil, err
	}
	inviteShareRatio, err := accountShareBillingFloat64("invite_share_ratio", command.InviteShareRatio)
	if err != nil {
		return nil, err
	}
	platformShareRatio, err := accountShareBillingFloat64("platform_share_ratio", command.PlatformShareRatio)
	if err != nil {
		return nil, err
	}
	appliedRateMultiplier, err := accountShareBillingFloat64("applied_rate_multiplier", usage.AppliedRateMultiplier)
	if err != nil {
		return nil, err
	}

	inputCost, err := accountShareBillingFloat64("input_cost", usage.InputCost)
	if err != nil {
		return nil, err
	}
	outputCost, err := accountShareBillingFloat64("output_cost", usage.OutputCost)
	if err != nil {
		return nil, err
	}
	cacheCreationCost, err := accountShareBillingFloat64("cache_creation_cost", usage.CacheCreationCost)
	if err != nil {
		return nil, err
	}
	cacheReadCost, err := accountShareBillingFloat64("cache_read_cost", usage.CacheReadCost)
	if err != nil {
		return nil, err
	}
	imageInputCost, err := accountShareBillingFloat64("image_input_cost", usage.ImageInputCost)
	if err != nil {
		return nil, err
	}
	imageOutputCost, err := accountShareBillingFloat64("image_output_cost", usage.ImageOutputCost)
	if err != nil {
		return nil, err
	}
	totalCost, err := accountShareBillingFloat64("total_cost", usage.TotalCost)
	if err != nil {
		return nil, err
	}
	actualCost, err := accountShareBillingFloat64("actual_cost", usage.ActualCost)
	if err != nil {
		return nil, err
	}
	accountStatsCost, err := accountShareBillingOptionalFloat64("account_stats_cost", usage.AccountStatsCost)
	if err != nil {
		return nil, err
	}
	balanceCost, err := accountShareBillingFloat64("balance_cost", usage.BalanceCost)
	if err != nil {
		return nil, err
	}
	subscriptionCost, err := accountShareBillingFloat64("subscription_cost", usage.SubscriptionCost)
	if err != nil {
		return nil, err
	}
	privateGroupCommissionCost, err := accountShareBillingFloat64("private_group_commission_cost", usage.PrivateGroupCommissionCost)
	if err != nil {
		return nil, err
	}
	apiKeyQuotaCost, err := accountShareBillingFloat64("api_key_quota_cost", usage.APIKeyQuotaCost)
	if err != nil {
		return nil, err
	}
	apiKeyRateLimitCost, err := accountShareBillingFloat64("api_key_rate_limit_cost", usage.APIKeyRateLimitCost)
	if err != nil {
		return nil, err
	}
	accountQuotaCost, err := accountShareBillingFloat64("account_quota_cost", usage.AccountQuotaCost)
	if err != nil {
		return nil, err
	}
	baseCharge, err := accountShareBillingFloat64("base_charge", usage.BaseCharge)
	if err != nil {
		return nil, err
	}
	hourlyCharge, err := accountShareBillingFloat64("hourly_charge", usage.HourlyCharge)
	if err != nil {
		return nil, err
	}
	totalCharge, err := accountShareBillingFloat64("total_charge", usage.TotalCharge)
	if err != nil {
		return nil, err
	}

	inputTokens, err := accountShareBillingInt("input_tokens", usage.InputTokens)
	if err != nil {
		return nil, err
	}
	outputTokens, err := accountShareBillingInt("output_tokens", usage.OutputTokens)
	if err != nil {
		return nil, err
	}
	cacheCreationTokens, err := accountShareBillingInt("cache_creation_tokens", usage.CacheCreationTokens)
	if err != nil {
		return nil, err
	}
	cacheCreation5mTokens, err := accountShareBillingInt("cache_creation_5m_tokens", usage.CacheCreation5mTokens)
	if err != nil {
		return nil, err
	}
	cacheCreation1hTokens, err := accountShareBillingInt("cache_creation_1h_tokens", usage.CacheCreation1hTokens)
	if err != nil {
		return nil, err
	}
	cacheReadTokens, err := accountShareBillingInt("cache_read_tokens", usage.CacheReadTokens)
	if err != nil {
		return nil, err
	}
	imageInputTokens, err := accountShareBillingInt("image_input_tokens", usage.ImageInputTokens)
	if err != nil {
		return nil, err
	}
	imageOutputTokens, err := accountShareBillingInt("image_output_tokens", usage.ImageOutputTokens)
	if err != nil {
		return nil, err
	}
	imageCount, err := accountShareBillingInt("image_count", usage.ImageCount)
	if err != nil {
		return nil, err
	}
	videoCount, err := accountShareBillingInt("video_count", usage.VideoCount)
	if err != nil {
		return nil, err
	}
	durationMilliseconds, err := accountShareBillingInt("duration_ms", usage.DurationMilliseconds)
	if err != nil {
		return nil, err
	}
	firstTokenMilliseconds, err := accountShareBillingOptionalInt("first_token_ms", usage.FirstTokenMilliseconds)
	if err != nil {
		return nil, err
	}
	videoDurationSeconds, err := accountShareBillingOptionalInt("video_duration_seconds", usage.VideoDurationSeconds)
	if err != nil {
		return nil, err
	}

	serviceTier := usage.ServiceTier
	if serviceTier == "" {
		serviceTier = command.ServiceTier
	}
	reasoningEffort := usage.ReasoningEffort
	if reasoningEffort == "" {
		reasoningEffort = command.ReasoningEffort
	}
	groupID := cloneAccountShareBillingInt64(command.GroupID)
	subscriptionID := cloneAccountShareBillingInt64(command.SubscriptionID)
	channelID := cloneAccountShareBillingInt64(command.ChannelID)
	policyID := cloneAccountShareBillingInt64(command.PolicyID)
	ownerUserID := item.OwnerUserID

	usageLog := &UsageLog{
		UserID:                item.ConsumerUserID,
		APIKeyID:              item.APIKeyID,
		AccountID:             item.AccountID,
		RequestID:             requestID,
		Model:                 usage.Model,
		RequestedModel:        command.RequestedModel,
		UpstreamModel:         optionalAccountShareBillingString(usage.UpstreamModel, usage.Model),
		ChannelID:             channelID,
		ModelMappingChain:     optionalAccountShareBillingString(command.ModelMappingChain, ""),
		BillingTier:           optionalAccountShareBillingString(usage.BillingTier, ""),
		BillingMode:           optionalAccountShareBillingString(usage.BillingMode, ""),
		ServiceTier:           optionalAccountShareBillingString(serviceTier, ""),
		ReasoningEffort:       optionalAccountShareBillingString(reasoningEffort, ""),
		InboundEndpoint:       optionalAccountShareBillingString(command.InboundEndpoint, ""),
		UpstreamEndpoint:      optionalAccountShareBillingString(command.UpstreamEndpoint, ""),
		GroupID:               groupID,
		SubscriptionID:        subscriptionID,
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CacheCreationTokens:   cacheCreationTokens,
		CacheReadTokens:       cacheReadTokens,
		CacheCreation5mTokens: cacheCreation5mTokens,
		CacheCreation1hTokens: cacheCreation1hTokens,
		ImageInputTokens:      imageInputTokens,
		ImageInputCost:        imageInputCost,
		ImageOutputTokens:     imageOutputTokens,
		ImageOutputCost:       imageOutputCost,
		InputCost:             inputCost,
		OutputCost:            outputCost,
		CacheCreationCost:     cacheCreationCost,
		CacheReadCost:         cacheReadCost,
		TotalCost:             totalCost,
		ActualCost:            actualCost,
		RateMultiplier:        appliedRateMultiplier,
		RateMultiplierSource:  command.RateMultiplierSource,
		AccountRateMultiplier: &accountRateMultiplier,
		AccountStatsCost:      accountStatsCost,
		BillingType:           int8(command.BillingType),
		RequestType:           requestType,
		Stream:                stream,
		OpenAIWSMode:          openAIWSMode,
		DurationMs:            &durationMilliseconds,
		FirstTokenMs:          firstTokenMilliseconds,
		CacheTTLOverridden:    usage.CacheTTLOverridden,
		ImageCount:            imageCount,
		ImageSize:             optionalAccountShareBillingString(usage.ImageSize, ""),
		MediaType:             optionalAccountShareBillingString(usage.MediaType, ""),
		VideoCount:            videoCount,
		VideoResolution:       optionalAccountShareBillingString(usage.VideoResolution, ""),
		VideoDurationSeconds:  videoDurationSeconds,
		CreatedAt:             usage.UsageOccurredAt,
	}

	billingFingerprint := hashAccountShareBillingPayload([]byte(strings.Join([]string{
		accountShareBillingWorkerFingerprintNamespace,
		requestFingerprint,
		commandHash,
		usageHash,
	}, "\n")))
	var accountShareModeSettlement *AccountShareModeBillingSnapshot
	if command.SettlementEnabled {
		accountShareModeSettlement = &AccountShareModeBillingSnapshot{
			MembershipID:       item.MembershipID,
			ListingID:          item.ListingID,
			AccountID:          item.AccountID,
			OwnerUserID:        item.OwnerUserID,
			ConsumerUserID:     item.ConsumerUserID,
			APIKeyID:           item.APIKeyID,
			BaseCharge:         baseCharge,
			HourlyCharge:       hourlyCharge,
			TotalCharge:        totalCharge,
			RateMultiplier:     rateMultiplier,
			HourlyRate:         hourlyRate,
			PolicyID:           policyID,
			PolicyVersion:      command.PolicyVersion,
			OwnerShareRatio:    ownerShareRatio,
			InviteShareRatio:   inviteShareRatio,
			PlatformShareRatio: platformShareRatio,
			DurationMs:         durationMilliseconds,
		}
	}
	return &UsageBillingCommand{
		RequestID:                  requestID,
		APIKeyID:                   item.APIKeyID,
		RequestFingerprint:         billingFingerprint,
		RequestPayloadHash:         command.RequestPayloadHash,
		UserID:                     item.ConsumerUserID,
		AccountID:                  item.AccountID,
		GroupID:                    groupID,
		SubscriptionID:             subscriptionID,
		AccountType:                command.AccountType,
		Model:                      usage.Model,
		ServiceTier:                serviceTier,
		ReasoningEffort:            reasoningEffort,
		BillingType:                int8(command.BillingType),
		InputTokens:                inputTokens,
		OutputTokens:               outputTokens,
		CacheCreationTokens:        cacheCreationTokens,
		CacheReadTokens:            cacheReadTokens,
		ImageCount:                 imageCount,
		MediaType:                  usage.MediaType,
		BalanceCost:                balanceCost,
		PreferPointsBilling:        command.PreferPointsBilling,
		SubscriptionCost:           subscriptionCost,
		PrivateGroupCommissionCost: privateGroupCommissionCost,
		APIKeyQuotaCost:            apiKeyQuotaCost,
		APIKeyRateLimitCost:        apiKeyRateLimitCost,
		AccountQuotaCost:           accountQuotaCost,
		ShareSnapshotCaptured:      true,
		ShareOwnerUserID:           &ownerUserID,
		ShareModeSnapshot:          command.ShareModeSnapshot,
		ShareStatusSnapshot:        command.ShareStatusSnapshot,
		SharePlatform:              command.SharePlatformSnapshot,
		UsageOccurredAt:            usage.UsageOccurredAt,
		AccountShareModeSettlement: accountShareModeSettlement,
		UsageLog:                   usageLog,
	}, nil
}

func canonicalAccountShareBillingHash(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%w: marshal durable billing payload: %v", ErrAccountShareBillingIntentInvalid, err)
	}
	return hashAccountShareBillingPayload(raw), nil
}

func accountShareBillingFloat64(name string, value string) (float64, error) {
	parsed, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil || parsed.IsNegative() {
		return 0, fmt.Errorf("%w: %s must be a non-negative decimal", ErrAccountShareBillingIntentInvalid, name)
	}
	number, _ := parsed.Float64()
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%w: %s is outside float64 range", ErrAccountShareBillingIntentInvalid, name)
	}
	return number, nil
}

func accountShareBillingOptionalFloat64(name string, value *string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := accountShareBillingFloat64(name, *value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func accountShareBillingInt(name string, value int64) (int, error) {
	converted := int(value)
	if int64(converted) != value {
		return 0, fmt.Errorf("%w: %s is outside int range", ErrAccountShareBillingIntentInvalid, name)
	}
	return converted, nil
}

func accountShareBillingOptionalInt(name string, value *int64) (*int, error) {
	if value == nil {
		return nil, nil
	}
	converted, err := accountShareBillingInt(name, *value)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}

func cloneAccountShareBillingInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func optionalAccountShareBillingString(value string, equalTo string) *string {
	value = strings.TrimSpace(value)
	if value == "" || value == strings.TrimSpace(equalTo) {
		return nil
	}
	return &value
}
