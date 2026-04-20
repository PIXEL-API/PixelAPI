//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

const webhookProviderTestEncryptionKey = "0123456789abcdef0123456789abcdef"

type webhookProviderTestDouble struct {
	key   string
	types []payment.PaymentType
}

func (p webhookProviderTestDouble) Name() string                          { return p.key }
func (p webhookProviderTestDouble) ProviderKey() string                   { return p.key }
func (p webhookProviderTestDouble) SupportedTypes() []payment.PaymentType { return p.types }
func (p webhookProviderTestDouble) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}
func (p webhookProviderTestDouble) QueryOrder(context.Context, string) (*payment.QueryOrderResponse, error) {
	panic("unexpected call")
}
func (p webhookProviderTestDouble) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}
func (p webhookProviderTestDouble) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func encryptWebhookProviderConfig(t *testing.T, config map[string]string) string {
	t.Helper()

	data, err := json.Marshal(config)
	require.NoError(t, err)

	encrypted, err := payment.Encrypt(string(data), []byte(webhookProviderTestEncryptionKey))
	require.NoError(t, err)
	return encrypted
}

func newWebhookProviderTestLoadBalancer(client *dbent.Client) payment.LoadBalancer {
	return payment.NewDefaultLoadBalancer(client, []byte(webhookProviderTestEncryptionKey))
}

func TestGetOrderProviderInstanceResolvesUniqueLegacyProviderKey(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("stripe-a").
		SetConfig(encryptWebhookProviderConfig(t, map[string]string{"secretKey": "sk_test_legacy_provider_key"})).
		SetSupportedTypes("stripe").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	providerKey := payment.TypeStripe
	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeStripe,
		ProviderKey: &providerKey,
	}

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	got, err := svc.getOrderProviderInstance(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, inst.ID, got.ID)
}

func TestGetOrderProviderInstanceResolvesUniqueLegacyPaymentType(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-a").
		SetConfig("{}").
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeWxpayDirect,
	}

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	got, err := svc.getOrderProviderInstance(ctx, order)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, inst.ID, got.ID)
}

func TestGetOrderProviderInstanceLeavesAmbiguousLegacyOrderUnresolved(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeEasyPay).
		SetName("easypay-a").
		SetConfig("{}").
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-a").
		SetConfig("{}").
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeWxpay,
	}

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	got, err := svc.getOrderProviderInstance(ctx, order)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetOrderProviderInstanceLeavesLegacyProviderKeyUnresolvedWhenHistoricalInstancesConflict(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("stripe-disabled-legacy").
		SetConfig("{}").
		SetSupportedTypes("stripe").
		SetEnabled(false).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("stripe-enabled-current").
		SetConfig("{}").
		SetSupportedTypes("stripe").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	providerKey := payment.TypeStripe
	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeStripe,
		ProviderKey: &providerKey,
	}

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	got, err := svc.getOrderProviderInstance(ctx, order)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetOrderProviderInstanceLeavesProviderKeyMatchUnresolvedWhenTypeNotSupported(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-only").
		SetConfig("{}").
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	providerKey := payment.TypeWxpay
	order := &dbent.PaymentOrder{
		PaymentType: payment.TypeAlipayDirect,
		ProviderKey: &providerKey,
	}

	svc := &PaymentService{
		entClient:    client,
		loadBalancer: newWebhookProviderTestLoadBalancer(client),
	}

	got, err := svc.getOrderProviderInstance(ctx, order)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetWebhookProviderRejectsAmbiguousRegistryFallback(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	wxpayConfigA := encryptWebhookProviderConfig(t, map[string]string{
		"appId":       "wx-app-a",
		"mchId":       "mch-a",
		"privateKey":  "private-key-a",
		"apiV3Key":    webhookProviderTestEncryptionKey,
		"publicKey":   "public-key-a",
		"publicKeyId": "public-key-id-a",
		"certSerial":  "cert-serial-a",
	})
	wxpayConfigB := encryptWebhookProviderConfig(t, map[string]string{
		"appId":       "wx-app-b",
		"mchId":       "mch-b",
		"privateKey":  "private-key-b",
		"apiV3Key":    webhookProviderTestEncryptionKey,
		"publicKey":   "public-key-b",
		"publicKeyId": "public-key-id-b",
		"certSerial":  "cert-serial-b",
	})
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-a").
		SetConfig(wxpayConfigA).
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-b").
		SetConfig(wxpayConfigB).
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:       client,
		loadBalancer:    newWebhookProviderTestLoadBalancer(client),
		registry:        payment.NewRegistry(),
		providersLoaded: true,
	}

	providers, err := svc.GetWebhookProviders(ctx, payment.TypeWxpay, "")
	require.NoError(t, err)
	require.Len(t, providers, 2)
}

func TestGetWebhookProvidersRejectAmbiguousFallbackForNonWxpay(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-a").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeAlipay).
		SetName("alipay-b").
		SetConfig("{}").
		SetSupportedTypes("alipay").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{
		entClient:       client,
		registry:        payment.NewRegistry(),
		providersLoaded: true,
	}

	_, err = svc.GetWebhookProviders(ctx, payment.TypeAlipay, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ambiguous")
}

func TestGetWebhookProviderAllowsSingleInstanceRegistryFallback(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	_, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName("stripe-a").
		SetConfig("{}").
		SetSupportedTypes("stripe").
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(webhookProviderTestDouble{
		key:   payment.TypeStripe,
		types: []payment.PaymentType{payment.TypeStripe},
	})

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	providers, err := svc.GetWebhookProviders(ctx, payment.TypeStripe, "")
	require.NoError(t, err)
	require.Len(t, providers, 1)
	prov := providers[0]
	require.Equal(t, payment.TypeStripe, prov.ProviderKey())
}

func TestGetWebhookProviderRejectsRegistryFallbackForPinnedOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("webhook@example.com").
		SetPasswordHash("hash").
		SetUsername("webhook").
		Save(ctx)
	require.NoError(t, err)

	pinnedInstanceID := "999"
	_, err = client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("TEST-RECHARGE").
		SetOutTradeNo("sub2_test_pinned_order").
		SetPaymentType(payment.TypeWxpay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(pinnedInstanceID).
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(webhookProviderTestDouble{
		key:   payment.TypeWxpay,
		types: []payment.PaymentType{payment.TypeWxpay},
	})

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	_, err = svc.GetWebhookProviders(ctx, payment.TypeWxpay, "sub2_test_pinned_order")
	require.Error(t, err)
	require.Contains(t, err.Error(), "provider instance")
}
