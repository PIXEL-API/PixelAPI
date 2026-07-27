package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var ErrUsageBillingPostCommitInvalid = errors.New("invalid usage billing post-commit finalizer configuration")

// UsageBillingPostCommitFinalizer closes the non-transactional side effects that
// must follow a durable usage-billing transaction. Implementations must be
// replay-safe because Apply may have committed before an intent lease is lost.
type UsageBillingPostCommitFinalizer interface {
	Finalize(ctx context.Context, command *UsageBillingCommand, result *UsageBillingApplyResult) error
}

type usageBillingCacheInvalidator interface {
	InvalidateUserBalance(ctx context.Context, userID int64) error
	InvalidateSubscription(ctx context.Context, userID, groupID int64) error
	InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error
}

type usageBillingLastUsedScheduler interface {
	ScheduleLastUsedUpdate(accountID int64)
}

type usageBillingUserReader interface {
	GetByID(ctx context.Context, id int64) (*User, error)
}

type usageBillingAccountReader interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
}

type usageBillingNotificationService interface {
	CheckBalanceAfterDeduction(ctx context.Context, user *User, oldBalance, cost float64)
	CheckAccountQuotaAfterIncrement(ctx context.Context, account *Account, cost float64, quotaState *AccountQuotaState)
}

type usageBillingPostCommitFinalizer struct {
	billingCache         usageBillingCacheInvalidator
	lastUsedScheduler    usageBillingLastUsedScheduler
	authCacheInvalidator APIKeyAuthCacheInvalidator
	userReader           usageBillingUserReader
	accountReader        usageBillingAccountReader
	notificationService  usageBillingNotificationService
}

func NewUsageBillingPostCommitFinalizer(
	billingCache usageBillingCacheInvalidator,
	lastUsedScheduler usageBillingLastUsedScheduler,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	userReader usageBillingUserReader,
	accountReader usageBillingAccountReader,
	notificationService usageBillingNotificationService,
) (UsageBillingPostCommitFinalizer, error) {
	if billingCache == nil ||
		lastUsedScheduler == nil ||
		authCacheInvalidator == nil ||
		userReader == nil ||
		accountReader == nil ||
		notificationService == nil {
		return nil, fmt.Errorf("%w: all dependencies are required", ErrUsageBillingPostCommitInvalid)
	}
	return &usageBillingPostCommitFinalizer{
		billingCache:         billingCache,
		lastUsedScheduler:    lastUsedScheduler,
		authCacheInvalidator: authCacheInvalidator,
		userReader:           userReader,
		accountReader:        accountReader,
		notificationService:  notificationService,
	}, nil
}

func (f *usageBillingPostCommitFinalizer) Finalize(
	ctx context.Context,
	command *UsageBillingCommand,
	result *UsageBillingApplyResult,
) error {
	if f == nil ||
		f.billingCache == nil ||
		f.lastUsedScheduler == nil ||
		f.authCacheInvalidator == nil ||
		f.userReader == nil ||
		f.accountReader == nil ||
		f.notificationService == nil {
		return fmt.Errorf("%w: finalizer is not initialized", ErrUsageBillingPostCommitInvalid)
	}
	if command == nil || result == nil {
		return fmt.Errorf("%w: command and result are required", ErrUsageBillingPostCommitInvalid)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var finalizeErrors []error
	for _, userID := range durableUsageBillingBalanceCacheUserIDs(command, result) {
		if err := f.billingCache.InvalidateUserBalance(ctx, userID); err != nil {
			finalizeErrors = append(finalizeErrors, fmt.Errorf("invalidate balance cache for user %d: %w", userID, err))
		}
	}

	if command.SubscriptionCost > 0 {
		if command.GroupID == nil || *command.GroupID <= 0 {
			finalizeErrors = append(finalizeErrors, fmt.Errorf("%w: subscription group identity is missing", ErrUsageBillingPostCommitInvalid))
		} else if err := f.billingCache.InvalidateSubscription(ctx, command.UserID, *command.GroupID); err != nil {
			finalizeErrors = append(finalizeErrors, fmt.Errorf(
				"invalidate subscription cache for user %d group %d: %w",
				command.UserID,
				*command.GroupID,
				err,
			))
		}
	}

	if command.APIKeyRateLimitCost > 0 {
		if err := f.billingCache.InvalidateAPIKeyRateLimit(ctx, command.APIKeyID); err != nil {
			finalizeErrors = append(finalizeErrors, fmt.Errorf(
				"invalidate api key rate-limit cache for key %d: %w",
				command.APIKeyID,
				err,
			))
		}
	}

	if command.APIKeyQuotaCost > 0 || command.BalanceCost > 0 {
		f.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, command.UserID)
	}
	f.lastUsedScheduler.ScheduleLastUsedUpdate(command.AccountID)

	// Notifications are best-effort and only emitted for the transaction that
	// actually applied. Replays still perform every idempotent cache invalidation
	// above, but must not send duplicate threshold notifications.
	if result.Applied {
		f.notifyBalanceDeduction(ctx, command, result)
		f.notifyAccountQuota(ctx, command, result)
	}

	return errors.Join(finalizeErrors...)
}

func durableUsageBillingBalanceCacheUserIDs(
	command *UsageBillingCommand,
	result *UsageBillingApplyResult,
) []int64 {
	if command == nil || result == nil {
		return nil
	}
	userIDs := make([]int64, 0, 6)
	if command.BalanceCost > 0 || command.PrivateGroupCommissionCost > 0 {
		userIDs = append(userIDs, command.UserID)
	}
	if command.ShareOwnerUserID != nil {
		userIDs = append(userIDs, *command.ShareOwnerUserID)
	}
	if snapshot := command.AccountShareModeSettlement; snapshot != nil {
		userIDs = append(userIDs, snapshot.OwnerUserID)
	}
	userIDs = append(userIDs, result.BalanceCreditUserIDs...)
	return uniquePositiveInt64s(userIDs)
}

func (f *usageBillingPostCommitFinalizer) notifyBalanceDeduction(
	ctx context.Context,
	command *UsageBillingCommand,
	result *UsageBillingApplyResult,
) {
	deducted := result.BalanceDeducted
	if result.CommissionDeducted > 0 {
		deducted = result.CommissionDeducted
	}
	if deducted <= 0 {
		return
	}
	user, err := f.userReader.GetByID(ctx, command.UserID)
	if err != nil {
		slog.Warn("usage billing post-commit balance notification skipped",
			"user_id", command.UserID,
			"error", err,
		)
		return
	}
	oldBalance := user.Balance + deducted
	if result.NewBalance != nil {
		oldBalance = *result.NewBalance + deducted
	}
	f.notificationService.CheckBalanceAfterDeduction(ctx, user, oldBalance, deducted)
}

func (f *usageBillingPostCommitFinalizer) notifyAccountQuota(
	ctx context.Context,
	command *UsageBillingCommand,
	result *UsageBillingApplyResult,
) {
	if command.AccountQuotaCost <= 0 {
		return
	}
	account, err := f.accountReader.GetByID(ctx, command.AccountID)
	if err != nil {
		slog.Warn("usage billing post-commit account quota notification skipped",
			"account_id", command.AccountID,
			"error", err,
		)
		return
	}
	f.notificationService.CheckAccountQuotaAfterIncrement(
		ctx,
		account,
		command.AccountQuotaCost,
		result.QuotaState,
	)
}
