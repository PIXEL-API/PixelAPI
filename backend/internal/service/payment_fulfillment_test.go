//go:build unit

package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type paymentFulfillmentTestProvider struct {
	key            string
	supportedTypes []payment.PaymentType
}

type paymentFulfillmentRedeemRepo struct {
	*paymentOrderLifecycleRedeemRepo
	getByIDErr         error
	getByCodeErr       error
	getByCodeErrOnCall int
	getByCodeCalls     int
}

type paymentFulfillmentBillingCache struct {
	mockBillingCache
	invalidateSubscriptionErr error
}

func (c *paymentFulfillmentBillingCache) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	return c.invalidateSubscriptionErr
}

func (r *paymentFulfillmentRedeemRepo) GetByID(ctx context.Context, id int64) (*RedeemCode, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	return r.paymentOrderLifecycleRedeemRepo.GetByID(ctx, id)
}

func (r *paymentFulfillmentRedeemRepo) GetByCode(ctx context.Context, code string) (*RedeemCode, error) {
	r.getByCodeCalls++
	if r.getByCodeErr != nil && r.getByCodeCalls == r.getByCodeErrOnCall {
		return nil, r.getByCodeErr
	}
	return r.paymentOrderLifecycleRedeemRepo.GetByCode(ctx, code)
}

func (p paymentFulfillmentTestProvider) Name() string        { return p.key }
func (p paymentFulfillmentTestProvider) ProviderKey() string { return p.key }
func (p paymentFulfillmentTestProvider) SupportedTypes() []payment.PaymentType {
	return p.supportedTypes
}
func (p paymentFulfillmentTestProvider) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}
func (p paymentFulfillmentTestProvider) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	panic("unexpected call")
}
func (p paymentFulfillmentTestProvider) VerifyNotification(ctx context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}
func (p paymentFulfillmentTestProvider) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

// ---------------------------------------------------------------------------
// resolveRedeemAction — pure idempotency decision logic
// ---------------------------------------------------------------------------

func TestResolveRedeemAction_CodeNotFound(t *testing.T) {
	t.Parallel()
	action := resolveRedeemAction(nil, ErrRedeemCodeNotFound)
	assert.Equal(t, redeemActionCreate, action, "only an explicit not-found result should create")
}

func TestResolveRedeemAction_LookupError(t *testing.T) {
	t.Parallel()
	action := resolveRedeemAction(nil, errors.New("db connection lost"))
	assert.Equal(t, redeemActionAbort, action, "unknown lookup errors must abort")
}

func TestResolveRedeemAction_LookupErrorWithNonNilCode(t *testing.T) {
	t.Parallel()
	// Edge case: both code and error are non-nil (shouldn't happen in practice,
	// but the function should still treat error as authoritative)
	code := &RedeemCode{Status: StatusUnused}
	action := resolveRedeemAction(code, errors.New("partial error"))
	assert.Equal(t, redeemActionAbort, action, "non-nil unknown error should abort regardless of code")
}

func TestResolveRedeemAction_CodeExistsAndUsed(t *testing.T) {
	t.Parallel()
	code := &RedeemCode{
		Code:   "test-code-123",
		Status: StatusUsed,
		Type:   RedeemTypeBalance,
		Value:  10.0,
	}
	action := resolveRedeemAction(code, nil)
	assert.Equal(t, redeemActionSkipCompleted, action, "used code should skip to completed")
}

func TestResolveRedeemAction_CodeExistsAndUnused(t *testing.T) {
	t.Parallel()
	code := &RedeemCode{
		Code:   "test-code-456",
		Status: StatusUnused,
		Type:   RedeemTypeBalance,
		Value:  25.0,
	}
	action := resolveRedeemAction(code, nil)
	assert.Equal(t, redeemActionRedeem, action, "unused code should skip creation and proceed to redeem")
}

func TestResolveRedeemAction_CodeExistsWithExpiredStatus(t *testing.T) {
	t.Parallel()
	// A code with a non-standard status (neither "unused" nor "used")
	// should NOT be treated as used, so it falls through to redeemActionRedeem.
	code := &RedeemCode{
		Code:   "expired-code",
		Status: StatusExpired,
	}
	action := resolveRedeemAction(code, nil)
	assert.Equal(t, redeemActionRedeem, action, "expired-status code is not IsUsed(), should redeem")
}

// ---------------------------------------------------------------------------
// Table-driven comprehensive test
// ---------------------------------------------------------------------------

func TestResolveRedeemAction_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		code     *RedeemCode
		err      error
		expected redeemAction
	}{
		{
			name:     "nil code, nil error — invalid repository result",
			code:     nil,
			err:      nil,
			expected: redeemActionAbort,
		},
		{
			name:     "nil code, explicit not found — create",
			code:     nil,
			err:      ErrRedeemCodeNotFound,
			expected: redeemActionCreate,
		},
		{
			name:     "nil code, generic DB error — abort",
			code:     nil,
			err:      errors.New("connection refused"),
			expected: redeemActionAbort,
		},
		{
			name:     "code exists, used — previous run completed redeem",
			code:     &RedeemCode{Status: StatusUsed},
			err:      nil,
			expected: redeemActionSkipCompleted,
		},
		{
			name:     "code exists, unused — previous run created code but crashed before redeem",
			code:     &RedeemCode{Status: StatusUnused},
			err:      nil,
			expected: redeemActionRedeem,
		},
		{
			name:     "code exists but unknown error also set — abort",
			code:     &RedeemCode{Status: StatusUsed},
			err:      errors.New("unexpected"),
			expected: redeemActionAbort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveRedeemAction(tt.code, tt.err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// redeemAction enum value sanity
// ---------------------------------------------------------------------------

func TestRedeemAction_DistinctValues(t *testing.T) {
	t.Parallel()
	// Ensure the three actions have distinct values (iota correctness)
	assert.NotEqual(t, redeemActionCreate, redeemActionRedeem)
	assert.NotEqual(t, redeemActionCreate, redeemActionSkipCompleted)
	assert.NotEqual(t, redeemActionCreate, redeemActionAbort)
	assert.NotEqual(t, redeemActionRedeem, redeemActionSkipCompleted)
	assert.NotEqual(t, redeemActionRedeem, redeemActionAbort)
	assert.NotEqual(t, redeemActionSkipCompleted, redeemActionAbort)
}

// ---------------------------------------------------------------------------
// RedeemCode.IsUsed / CanUse interaction with resolveRedeemAction
// ---------------------------------------------------------------------------

func TestResolveRedeemAction_IsUsedCanUseConsistency(t *testing.T) {
	t.Parallel()

	usedCode := &RedeemCode{Status: StatusUsed}
	unusedCode := &RedeemCode{Status: StatusUnused}

	// Verify our decision function is consistent with the domain model methods
	assert.True(t, usedCode.IsUsed())
	assert.False(t, usedCode.CanUse())
	assert.Equal(t, redeemActionSkipCompleted, resolveRedeemAction(usedCode, nil))

	assert.False(t, unusedCode.IsUsed())
	assert.True(t, unusedCode.CanUse())
	assert.Equal(t, redeemActionRedeem, resolveRedeemAction(unusedCode, nil))
}

func TestExpectedNotificationProviderKeyPrefersOrderInstanceProvider(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeAlipay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKey(registry, payment.TypeAlipay, "", payment.TypeEasyPay),
	)
}

func TestExpectedNotificationProviderKeyUsesRegistryMappingForLegacyOrders(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeEasyPay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKey(registry, payment.TypeAlipay, "", ""),
	)
}

func TestExpectedNotificationProviderKeyFallsBackToPaymentType(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		payment.TypeWxpay,
		expectedNotificationProviderKey(nil, payment.TypeWxpay, "", ""),
	)
}

func TestExpectedNotificationProviderKeyPrefersOrderSnapshotProviderKey(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeAlipay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKey(registry, payment.TypeAlipay, payment.TypeEasyPay, ""),
	)
}

func TestExpectedNotificationProviderKeyForOrderUsesSnapshotProviderKey(t *testing.T) {
	t.Parallel()

	registry := payment.NewRegistry()
	registry.Register(paymentFulfillmentTestProvider{
		key:            payment.TypeAlipay,
		supportedTypes: []payment.PaymentType{payment.TypeAlipay},
	})

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version": 1,
			"provider_key":   payment.TypeEasyPay,
		},
	}

	assert.Equal(t,
		payment.TypeEasyPay,
		expectedNotificationProviderKeyForOrder(registry, order, ""),
	)
}

func TestValidateProviderNotificationMetadataRejectsWxpaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeWxpay,
		ProviderSnapshot: map[string]any{
			"schema_version":  1,
			"merchant_app_id": "wx-app-expected",
			"merchant_id":     "mch-expected",
			"currency":        "CNY",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeWxpay, map[string]string{
		"appid":       "wx-app-other",
		"mchid":       "mch-expected",
		"currency":    "CNY",
		"trade_state": "SUCCESS",
	})
	assert.ErrorContains(t, err, "wxpay appid mismatch")
}

func TestValidateProviderNotificationMetadataAllowsLegacyOrdersWithoutSnapshotFields(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeWxpay,
		ProviderSnapshot: map[string]any{
			"schema_version":       1,
			"provider_instance_id": "9",
			"provider_key":         payment.TypeWxpay,
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeWxpay, map[string]string{
		"appid":       "wx-app-runtime",
		"mchid":       "mch-runtime",
		"currency":    "CNY",
		"trade_state": "SUCCESS",
	})
	assert.NoError(t, err)
}

func TestParseLegacyPaymentOrderID(t *testing.T) {
	t.Parallel()

	oid, ok := parseLegacyPaymentOrderID("sub2_42", &dbent.NotFoundError{})
	assert.True(t, ok)
	assert.EqualValues(t, 42, oid)

	_, ok = parseLegacyPaymentOrderID("42", &dbent.NotFoundError{})
	assert.False(t, ok)

	_, ok = parseLegacyPaymentOrderID("sub2_42", errors.New("db down"))
	assert.False(t, ok)
}

func TestIsValidProviderAmount(t *testing.T) {
	t.Parallel()

	assert.True(t, isValidProviderAmount(0.01))
	assert.False(t, isValidProviderAmount(0))
	assert.False(t, isValidProviderAmount(-1))
	assert.False(t, isValidProviderAmount(math.NaN()))
	assert.False(t, isValidProviderAmount(math.Inf(1)))
}

func TestValidateProviderNotificationMetadataRejectsAlipaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":  2,
			"merchant_app_id": "alipay-app-expected",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeAlipay, map[string]string{
		"app_id": "alipay-app-other",
	})
	assert.ErrorContains(t, err, "alipay app_id mismatch")
}

func TestValidateProviderNotificationMetadataRejectsAlipayMissingMetadata(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":  2,
			"merchant_app_id": "alipay-app-expected",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeAlipay, nil)
	assert.ErrorContains(t, err, "alipay app_id missing")
}

func TestValidateProviderNotificationMetadataRejectsEasyPaySnapshotMismatch(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version": 2,
			"merchant_id":    "pid-expected",
		},
	}

	err := validateProviderNotificationMetadata(order, payment.TypeEasyPay, map[string]string{
		"pid": "pid-other",
	})
	assert.ErrorContains(t, err, "easypay pid mismatch")
}

func TestPaymentFulfillmentAlreadyProcessedReturnsReloadError(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	svc := &PaymentService{entClient: client}

	err := svc.alreadyProcessed(ctx, &dbent.PaymentOrder{ID: 987654321})

	require.Error(t, err)
	require.ErrorContains(t, err, "reload already processed order")
}

func TestPaymentFulfillmentAlreadyProcessedRejectsUnknownState(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusCancelled, "PAYMENT-FULFILLMENT-CANCELLED", 25)
	svc := &PaymentService{entClient: client}

	err := svc.alreadyProcessed(ctx, order)

	require.Error(t, err)
	require.ErrorContains(t, err, OrderStatusCancelled)
}

func TestBalanceFulfillmentRecoversCommittedRedeemAfterReloadFailure(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-FULFILLMENT-COMMITTED", 25)
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID, Balance: 0}}
	redeemRepo := newPaymentFulfillmentRedeemRepo(order)
	redeemRepo.getByIDErr = errors.New("reload failed after commit")
	svc := newPaymentFulfillmentService(client, redeemRepo, userRepo)

	err := svc.ExecuteBalanceFulfillment(ctx, order.ID)

	require.NoError(t, err)
	completed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, completed.Status)
	require.Equal(t, order.Amount, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
	require.Zero(t, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestBalanceFulfillmentFailureReconciliationCompletesCommittedRedeem(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-FULFILLMENT-RECONCILE", 25)
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID, Balance: order.Amount}}
	redeemRepo := newPaymentFulfillmentRedeemRepo(order)
	usedBy := order.UserID
	code := redeemRepo.codesByCode[order.RechargeCode]
	code.Status = StatusUsed
	code.UsedBy = &usedBy
	svc := newPaymentFulfillmentService(client, redeemRepo, userRepo)
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, lease)

	err = svc.reconcileBalanceFulfillmentFailure(ctx, order, lease, errors.New("ambiguous post-commit failure"))

	require.NoError(t, err)
	completed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, completed.Status)
	require.Zero(t, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestBalanceFulfillmentLeaseGuardBlocksStaleWorkerBeforeRedeem(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusRecharging, "PAYMENT-FULFILLMENT-STALE-WORKER", 25)
	userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID, Balance: 0}}
	redeemRepo := newPaymentFulfillmentRedeemRepo(order)
	svc := newPaymentFulfillmentService(client, redeemRepo, userRepo)
	staleLease := &paymentFulfillmentLease{version: order.UpdatedAt.Add(-time.Minute)}

	err := svc.doBalance(ctx, order, staleLease)

	require.Error(t, err)
	require.ErrorContains(t, err, "payment fulfillment lease was lost")
	require.Empty(t, redeemRepo.useCalls)
	require.Equal(t, float64(0), userRepo.getByIDUser.Balance)
	current, getErr := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusRecharging, current.Status)
}

func TestBalanceFulfillmentVerificationUnavailableKeepsRecharging(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-FULFILLMENT-VERIFY-ERROR", 25)
	userRepo := &mockUserRepo{
		getByIDUser: &User{ID: order.UserID, Balance: 0},
		getByIDErr:  errors.New("user lookup failed before redemption"),
	}
	redeemRepo := newPaymentFulfillmentRedeemRepo(order)
	redeemRepo.getByCodeErr = errors.New("verification database unavailable")
	redeemRepo.getByCodeErrOnCall = 3
	svc := newPaymentFulfillmentService(client, redeemRepo, userRepo)

	err := svc.ExecuteBalanceFulfillment(ctx, order.ID)

	require.Error(t, err)
	require.ErrorContains(t, err, "verification database unavailable")
	current, getErr := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusRecharging, current.Status)
	require.Equal(t, float64(0), userRepo.getByIDUser.Balance)
	require.Zero(t, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestBalanceFulfillmentRejectsUsedCodeThatDoesNotBelongToOrder(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RedeemCode)
	}{
		{
			name: "different user",
			mutate: func(code *RedeemCode) {
				otherUserID := int64(999)
				code.UsedBy = &otherUserID
			},
		},
		{
			name: "different type",
			mutate: func(code *RedeemCode) {
				code.Type = RedeemTypePoints
			},
		},
		{
			name: "different amount",
			mutate: func(code *RedeemCode) {
				code.Value++
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-FULFILLMENT-MISMATCH-"+tt.name, 25)
			userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID, Balance: 0}}
			redeemRepo := newPaymentFulfillmentRedeemRepo(order)
			usedBy := order.UserID
			code := redeemRepo.codesByCode[order.RechargeCode]
			code.Status = StatusUsed
			code.UsedBy = &usedBy
			tt.mutate(code)
			svc := newPaymentFulfillmentService(client, redeemRepo, userRepo)

			err := svc.ExecuteBalanceFulfillment(ctx, order.ID)

			require.Error(t, err)
			current, getErr := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, getErr)
			require.Equal(t, OrderStatusFailed, current.Status)
			require.Equal(t, float64(0), userRepo.getByIDUser.Balance)
			require.Equal(t, 1, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
		})
	}
}

func TestSubscriptionFulfillmentFailureReconciliationCompletesCommittedAssignment(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-SUBSCRIPTION-COMMITTED", 25)
	svc := &PaymentService{entClient: client}
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, lease)

	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("SUBSCRIPTION_ASSIGNED").
		SetDetail("{}").
		SetOperator("system").
		Save(ctx)
	require.NoError(t, err)

	err = svc.reconcileSubscriptionFulfillmentFailure(ctx, order, lease, errors.New("ambiguous post-commit failure"))

	require.NoError(t, err)
	completed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, completed.Status)
	require.Equal(t, 1, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "SUBSCRIPTION_SUCCESS"))
	require.Zero(t, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestSubscriptionFulfillmentFailureReconciliationVerificationErrorKeepsRecharging(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-SUBSCRIPTION-VERIFY-ERROR", 25)
	svc := &PaymentService{entClient: client}
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, lease)

	cause := errors.New("subscription fulfillment failed")
	queryCtx, cancel := context.WithCancel(ctx)
	cancel()
	err = svc.reconcileSubscriptionFulfillmentFailure(queryCtx, order, lease, cause)

	require.Error(t, err)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "verify subscription fulfillment after failure")
	current, getErr := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusRecharging, current.Status)
	require.Zero(t, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestSubscriptionFulfillmentFailureReconciliationCompletionErrorKeepsRecharging(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-SUBSCRIPTION-COMPLETE-ERROR", 25)
	svc := &PaymentService{entClient: client}
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, lease)

	_, err = client.PaymentAuditLog.Create().
		SetOrderID(strconv.FormatInt(order.ID, 10)).
		SetAction("SUBSCRIPTION_ASSIGNED").
		SetDetail("{}").
		SetOperator("system").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetUpdatedAt(lease.version.Add(time.Second)).
		Save(ctx)
	require.NoError(t, err)

	cause := errors.New("ambiguous post-commit failure")
	err = svc.reconcileSubscriptionFulfillmentFailure(ctx, order, lease, cause)

	require.Error(t, err)
	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "complete verified subscription fulfillment")
	require.ErrorContains(t, err, "fulfillment lease was lost before completion")
	current, getErr := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, getErr)
	require.Equal(t, OrderStatusRecharging, current.Status)
	require.Zero(t, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestSubscriptionFulfillmentFailureReconciliationMarksFailedOnlyWithoutCommitAudit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentOrder(t, ctx, client, OrderStatusPaid, "PAYMENT-SUBSCRIPTION-NOT-COMMITTED", 25)
	svc := &PaymentService{entClient: client}
	lease, err := svc.acquirePaymentFulfillmentLease(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, lease)

	cause := errors.New("subscription assignment rejected")
	err = svc.reconcileSubscriptionFulfillmentFailure(ctx, order, lease, cause)

	require.ErrorIs(t, err, cause)
	failed, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusFailed, failed.Status)
	require.Equal(t, 1, countPaymentFulfillmentAudit(t, ctx, client, order.ID, "FULFILLMENT_FAILED"))
}

func TestPaymentSubscriptionCacheFailureAfterCommitIsNonFatal(t *testing.T) {
	billingCache := &paymentFulfillmentBillingCache{
		invalidateSubscriptionErr: errors.New("cache unavailable"),
	}
	billingCacheService := &BillingCacheService{cache: billingCache}
	subscriptionSvc := &SubscriptionService{billingCacheService: billingCacheService}

	svc := &PaymentService{subscriptionSvc: subscriptionSvc}
	svc.invalidatePaymentSubscriptionCachesAfterCommit(42, 7, 9)
}

func createPaymentFulfillmentOrder(t *testing.T, ctx context.Context, client *dbent.Client, status, rechargeCode string, amount float64) *dbent.PaymentOrder {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(rechargeCode + "@example.com").
		SetPasswordHash("hash").
		SetUsername(rechargeCode).
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(amount).
		SetPayAmount(amount).
		SetFeeRate(0).
		SetRechargeCode(rechargeCode).
		SetOutTradeNo("sub2_" + rechargeCode).
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("trade-" + rechargeCode).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(status).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)
	return order
}

func newPaymentFulfillmentRedeemRepo(order *dbent.PaymentOrder) *paymentFulfillmentRedeemRepo {
	return &paymentFulfillmentRedeemRepo{
		paymentOrderLifecycleRedeemRepo: &paymentOrderLifecycleRedeemRepo{
			codesByCode: map[string]*RedeemCode{
				order.RechargeCode: {
					ID:     1,
					Code:   order.RechargeCode,
					Type:   RedeemTypeBalance,
					Value:  order.Amount,
					Status: StatusUnused,
				},
			},
		},
	}
}

func newPaymentFulfillmentService(client *dbent.Client, redeemRepo RedeemCodeRepository, userRepo UserRepository) *PaymentService {
	return &PaymentService{
		entClient: client,
		redeemService: NewRedeemService(
			redeemRepo,
			userRepo,
			nil,
			nil,
			nil,
			client,
			nil,
		),
		userRepo: userRepo,
	}
}

func countPaymentFulfillmentAudit(t *testing.T, ctx context.Context, client *dbent.Client, orderID int64, action string) int {
	t.Helper()
	count, err := client.PaymentAuditLog.Query().Where(
		paymentauditlog.OrderIDEQ(strconv.FormatInt(orderID, 10)),
		paymentauditlog.ActionEQ(action),
	).Count(ctx)
	require.NoError(t, err)
	return count
}
