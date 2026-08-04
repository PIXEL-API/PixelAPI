package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	stripe "github.com/stripe/stripe-go/v85"
	"github.com/stripe/stripe-go/v85/webhook"
)

// Stripe constants.
const (
	stripeCurrency            = "cny"
	stripeEventPaymentSuccess = "payment_intent.succeeded"
	stripeEventPaymentFailed  = "payment_intent.payment_failed"
)

// Stripe implements the payment.CancelableProvider interface for Stripe payments.
type Stripe struct {
	instanceID string
	config     map[string]string

	mu          sync.Mutex
	initialized bool
	sc          *stripe.Client
}

// NewStripe creates a new Stripe provider instance.
func NewStripe(instanceID string, config map[string]string) (*Stripe, error) {
	if config["secretKey"] == "" {
		return nil, fmt.Errorf("stripe config missing required key: secretKey")
	}
	return &Stripe{
		instanceID: instanceID,
		config:     config,
	}, nil
}

func (s *Stripe) ensureInit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		s.sc = stripe.NewClient(s.config["secretKey"])
		s.initialized = true
	}
}

// GetPublishableKey returns the publishable key for frontend use.
func (s *Stripe) GetPublishableKey() string {
	return s.config["publishableKey"]
}

func (s *Stripe) Name() string        { return "Stripe" }
func (s *Stripe) ProviderKey() string { return payment.TypeStripe }
func (s *Stripe) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{payment.TypeStripe}
}

// stripePaymentMethodTypes maps our PaymentType to Stripe payment_method_types.
var stripePaymentMethodTypes = map[string][]string{
	payment.TypeCard:   {"card"},
	payment.TypeAlipay: {"alipay"},
	payment.TypeWxpay:  {"wechat_pay"},
	payment.TypeLink:   {"link"},
}

// CreatePayment creates a Stripe PaymentIntent.
func (s *Stripe) CreatePayment(ctx context.Context, req payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	s.ensureInit()

	amountInCents, err := payment.YuanToFen(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	// Collect all Stripe payment_method_types from the instance's configured sub-methods
	methods := resolveStripeMethodTypes(req.InstanceSubMethods)

	pmTypes := make([]*string, len(methods))
	for i, m := range methods {
		pmTypes[i] = stripe.String(m)
	}

	params := &stripe.PaymentIntentCreateParams{
		Amount:             stripe.Int64(amountInCents),
		Currency:           stripe.String(stripeCurrency),
		PaymentMethodTypes: pmTypes,
		Description:        stripe.String(req.Subject),
		Metadata:           map[string]string{"orderId": req.OrderID},
	}

	// WeChat Pay requires payment_method_options with client type
	if hasStripeMethod(methods, "wechat_pay") {
		params.PaymentMethodOptions = &stripe.PaymentIntentCreatePaymentMethodOptionsParams{
			WeChatPay: &stripe.PaymentIntentCreatePaymentMethodOptionsWeChatPayParams{
				Client: stripe.String("web"),
			},
		}
	}

	params.SetIdempotencyKey(fmt.Sprintf("pi-%s", req.OrderID))
	params.Context = ctx

	pi, err := s.sc.V1PaymentIntents.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe create payment: %w", err)
	}

	return &payment.CreatePaymentResponse{
		TradeNo:      pi.ID,
		ClientSecret: pi.ClientSecret,
	}, nil
}

// QueryOrder retrieves a PaymentIntent by ID.
func (s *Stripe) QueryOrder(ctx context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	s.ensureInit()

	pi, err := s.sc.V1PaymentIntents.Retrieve(ctx, tradeNo, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe query order: %w", err)
	}

	status := payment.ProviderStatusPending
	switch pi.Status {
	case stripe.PaymentIntentStatusSucceeded:
		status = payment.ProviderStatusPaid
	case stripe.PaymentIntentStatusCanceled:
		status = payment.ProviderStatusFailed
	}

	return &payment.QueryOrderResponse{
		TradeNo:  pi.ID,
		Status:   status,
		Amount:   payment.FenToYuan(pi.Amount),
		Metadata: stripePaymentIntentMetadata(pi),
	}, nil
}

// VerifyNotification verifies a Stripe webhook event.
func (s *Stripe) VerifyNotification(_ context.Context, rawBody string, headers map[string]string) (*payment.PaymentNotification, error) {
	s.ensureInit()

	webhookSecret := s.config["webhookSecret"]
	if webhookSecret == "" {
		return nil, fmt.Errorf("stripe webhookSecret not configured")
	}

	sig := headers["stripe-signature"]
	if sig == "" {
		return nil, fmt.Errorf("stripe notification missing stripe-signature header")
	}

	event, err := webhook.ConstructEvent([]byte(rawBody), sig, webhookSecret)
	if err != nil {
		return nil, fmt.Errorf("stripe verify notification: %w", err)
	}

	switch event.Type {
	case stripeEventPaymentSuccess:
		return parseStripePaymentIntent(&event, payment.ProviderStatusSuccess, rawBody)
	case stripeEventPaymentFailed:
		return parseStripePaymentIntent(&event, payment.ProviderStatusFailed, rawBody)
	}

	return nil, nil
}

func parseStripePaymentIntent(event *stripe.Event, status string, rawBody string) (*payment.PaymentNotification, error) {
	var pi stripe.PaymentIntent
	if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
		return nil, fmt.Errorf("stripe parse payment_intent: %w", err)
	}
	return &payment.PaymentNotification{
		TradeNo:  pi.ID,
		OrderID:  pi.Metadata["orderId"],
		Amount:   payment.FenToYuan(pi.Amount),
		Status:   status,
		RawData:  rawBody,
		Metadata: stripePaymentIntentMetadata(&pi),
	}, nil
}

func stripePaymentIntentMetadata(pi *stripe.PaymentIntent) map[string]string {
	metadata := map[string]string{}
	if pi != nil && strings.TrimSpace(string(pi.Currency)) != "" {
		metadata["currency"] = strings.ToUpper(strings.TrimSpace(string(pi.Currency)))
	}
	return metadata
}

// Refund creates a Stripe refund.
func (s *Stripe) Refund(ctx context.Context, req payment.RefundRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	amountInCents, err := payment.YuanToFen(req.Amount)
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	params := &stripe.RefundCreateParams{
		PaymentIntent: stripe.String(req.TradeNo),
		Amount:        stripe.Int64(amountInCents),
		Reason:        stripe.String(string(stripe.RefundReasonRequestedByCustomer)),
	}
	// 幂等键：退款重试（网关超时、管理员重复点击、REFUND_FAILED 后重试）不能变成第二笔退款。
	// 带上金额是为了让「改额后重新发起」被视作另一笔请求，而不是被 Stripe 当成重放返回旧结果。
	params.SetIdempotencyKey(fmt.Sprintf("re-%s-%d", req.OrderID, amountInCents))
	params.Context = ctx

	r, err := s.sc.V1Refunds.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("stripe refund: %w", err)
	}

	refundStatus := payment.ProviderStatusPending
	if r.Status == stripe.RefundStatusSucceeded {
		refundStatus = payment.ProviderStatusSuccess
	}

	return &payment.RefundResponse{
		RefundID: r.ID,
		Status:   refundStatus,
	}, nil
}

// QueryRefund retrieves a previously created Stripe refund by its refund ID
// (the `re_xxx` returned by Refund and persisted as payment_orders.refund_trade_no).
//
// 语义约定：网关调用失败、鉴权失败、退款单号不存在都返回 error，绝不降级成
// ProviderStatusFailed——「退款确实失败了」会让订单进 REFUND_FAILED 并回滚扣减，
// 而「我查不到」必须让订单留在 REFUND_PENDING 等人工核对。
// 同理，Stripe 后续新增的未知 status 一律落到 pending，不擅自判死。
func (s *Stripe) QueryRefund(ctx context.Context, req payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	s.ensureInit()

	// 本地与上游的分叉点：上游在 RefundID 为空时会按 PaymentIntent 列取「最近一笔退款」
	// 兜底，那是因为上游没有落库退款单号。本地迁移 264 已持久化 refund_trade_no，
	// 列取兜底只会在多笔部分退款时挑错单子，故这里直接报错，不猜。
	refundID := strings.TrimSpace(req.RefundID)
	if refundID == "" {
		return nil, fmt.Errorf("stripe query refund: missing refund id")
	}

	r, err := s.sc.V1Refunds.Retrieve(ctx, refundID, nil)
	if err != nil {
		return nil, fmt.Errorf("stripe query refund: %w", err)
	}
	if r == nil {
		return nil, fmt.Errorf("stripe query refund: empty response for refund %s", refundID)
	}

	// 防串单：退款单号若被错记成别的订单的退款，宁可报错等人工，也不能把
	// 另一笔订单的成功状态回写到本订单。两侧 ID 有任一为空时跳过校验。
	if tradeNo := strings.TrimSpace(req.TradeNo); tradeNo != "" && r.PaymentIntent != nil && r.PaymentIntent.ID != "" {
		if r.PaymentIntent.ID != tradeNo {
			return nil, fmt.Errorf(
				"stripe query refund: refund %s belongs to payment intent %s, not %s",
				refundID, r.PaymentIntent.ID, tradeNo,
			)
		}
	}

	return &payment.RefundResponse{
		RefundID: r.ID,
		Status:   stripeRefundProviderStatus(r.Status),
	}, nil
}

// stripeRefundProviderStatus maps a Stripe refund status onto the three
// provider-level statuses the service layer understands. Unknown values are
// treated as pending so a future Stripe status never silently fails an order.
func stripeRefundProviderStatus(status stripe.RefundStatus) string {
	switch status {
	case stripe.RefundStatusSucceeded:
		return payment.ProviderStatusSuccess
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		return payment.ProviderStatusFailed
	case stripe.RefundStatusPending, stripe.RefundStatusRequiresAction:
		return payment.ProviderStatusPending
	default:
		return payment.ProviderStatusPending
	}
}

// resolveStripeMethodTypes converts instance supported_types (comma-separated)
// into Stripe API payment_method_types. Falls back to ["card"] if empty.
func resolveStripeMethodTypes(instanceSubMethods string) []string {
	if instanceSubMethods == "" {
		return []string{"card"}
	}
	var methods []string
	for _, t := range strings.Split(instanceSubMethods, ",") {
		t = strings.TrimSpace(t)
		if mapped, ok := stripePaymentMethodTypes[t]; ok {
			methods = append(methods, mapped...)
		}
	}
	if len(methods) == 0 {
		return []string{"card"}
	}
	return methods
}

// hasStripeMethod checks if the given Stripe method list contains the target method.
func hasStripeMethod(methods []string, target string) bool {
	for _, m := range methods {
		if m == target {
			return true
		}
	}
	return false
}

// CancelPayment cancels a pending PaymentIntent.
func (s *Stripe) CancelPayment(ctx context.Context, tradeNo string) error {
	s.ensureInit()

	_, err := s.sc.V1PaymentIntents.Cancel(ctx, tradeNo, nil)
	if err != nil {
		return fmt.Errorf("stripe cancel payment: %w", err)
	}
	return nil
}

// Ensure interface compliance.
var (
	_ payment.Provider            = (*Stripe)(nil)
	_ payment.CancelableProvider  = (*Stripe)(nil)
	_ payment.RefundQueryProvider = (*Stripe)(nil)
)
