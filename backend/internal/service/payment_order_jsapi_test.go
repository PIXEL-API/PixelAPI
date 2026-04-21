package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

const jsapiTestEncryptionKey = "0123456789abcdef0123456789abcdef"

func TestSelectCreateOrderInstancePrefersJSAPICompatibleWxpayInstance(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)

	compatibleConfig := mustEncryptJSAPITestConfig(t, map[string]string{
		"appId":       "wx-merchant-app",
		"mpAppId":     "wx-mp-app",
		"mchId":       "mch-compatible",
		"privateKey":  "private-key",
		"apiV3Key":    jsapiTestEncryptionKey,
		"publicKey":   "public-key",
		"publicKeyId": "key-compatible",
		"certSerial":  "serial-compatible",
	})
	incompatibleConfig := mustEncryptJSAPITestConfig(t, map[string]string{
		"appId":       "wx-merchant-other",
		"mpAppId":     "wx-mp-other",
		"mchId":       "mch-incompatible",
		"privateKey":  "private-key",
		"apiV3Key":    jsapiTestEncryptionKey,
		"publicKey":   "public-key",
		"publicKeyId": "key-incompatible",
		"certSerial":  "serial-incompatible",
	})

	compatible, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-compatible").
		SetConfig(compatibleConfig).
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		SetSortOrder(1).
		Save(ctx)
	if err != nil {
		t.Fatalf("create compatible wxpay instance: %v", err)
	}
	_, err = client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeWxpay).
		SetName("wxpay-incompatible").
		SetConfig(incompatibleConfig).
		SetSupportedTypes("wxpay").
		SetEnabled(true).
		SetSortOrder(2).
		Save(ctx)
	if err != nil {
		t.Fatalf("create incompatible wxpay instance: %v", err)
	}

	configService := &PaymentConfigService{
		entClient: client,
		settingRepo: &paymentConfigSettingRepoStub{values: map[string]string{
			SettingPaymentVisibleMethodWxpayEnabled:    "true",
			SettingPaymentVisibleMethodWxpaySource:     VisibleMethodSourceOfficialWechat,
			SettingKeyWeChatConnectEnabled:             "true",
			SettingKeyWeChatConnectAppID:               "wx-mp-app",
			SettingKeyWeChatConnectAppSecret:           "wechat-secret",
			SettingKeyWeChatConnectMode:                "mp",
			SettingKeyWeChatConnectScopes:              "snsapi_base",
			SettingKeyWeChatConnectRedirectURL:         "https://api.example.com/api/v1/auth/oauth/wechat/callback",
			SettingKeyWeChatConnectFrontendRedirectURL: "/auth/wechat/callback",
		}},
		encryptionKey: []byte(jsapiTestEncryptionKey),
	}
	loadBalancer := newVisibleMethodLoadBalancer(
		payment.NewDefaultLoadBalancer(client, []byte(jsapiTestEncryptionKey)),
		configService,
	)
	svc := &PaymentService{
		entClient:     client,
		loadBalancer:  loadBalancer,
		configService: configService,
	}

	sel, err := svc.selectCreateOrderInstance(ctx, CreateOrderRequest{
		PaymentType:     payment.TypeWxpay,
		OpenID:          "openid-123",
		IsWeChatBrowser: true,
	}, &PaymentConfig{LoadBalanceStrategy: string(payment.StrategyRoundRobin)}, 12.5)
	if err != nil {
		t.Fatalf("selectCreateOrderInstance returned error: %v", err)
	}
	if sel == nil {
		t.Fatal("expected selected instance, got nil")
	}
	expectedInstanceID := fmt.Sprintf("%d", compatible.ID)
	if sel.InstanceID != expectedInstanceID {
		t.Fatalf("selected instance id = %q, want %q", sel.InstanceID, expectedInstanceID)
	}
}

func mustEncryptJSAPITestConfig(t *testing.T, config map[string]string) string {
	t.Helper()

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	encrypted, err := payment.Encrypt(string(data), []byte(jsapiTestEncryptionKey))
	if err != nil {
		t.Fatalf("encrypt config: %v", err)
	}
	return encrypted
}
