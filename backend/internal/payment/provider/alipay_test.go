//go:build unit

package provider

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/smartwalle/alipay/v3"
)

func TestIsTradeNotExist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "error containing ACQ.TRADE_NOT_EXIST returns true",
			err:  errors.New("alipay: sub_code=ACQ.TRADE_NOT_EXIST, sub_msg=交易不存在"),
			want: true,
		},
		{
			name: "error not containing the code returns false",
			err:  errors.New("alipay: sub_code=ACQ.SYSTEM_ERROR, sub_msg=系统错误"),
			want: false,
		},
		{
			name: "error with only partial match returns false",
			err:  errors.New("ACQ.TRADE_NOT"),
			want: false,
		},
		{
			name: "error with exact constant value returns true",
			err:  errors.New(alipayErrTradeNotExist),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isTradeNotExist(tt.err)
			if got != tt.want {
				t.Errorf("isTradeNotExist(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewAlipay(t *testing.T) {
	t.Parallel()

	validConfig := map[string]string{
		"appId":      "2021001234567890",
		"privateKey": "MIIEvQIBADANBgkqhkiG9w0BAQEFAASC...",
	}

	// helper to clone and override config fields
	withOverride := func(overrides map[string]string) map[string]string {
		cfg := make(map[string]string, len(validConfig))
		for k, v := range validConfig {
			cfg[k] = v
		}
		for k, v := range overrides {
			cfg[k] = v
		}
		return cfg
	}

	tests := []struct {
		name      string
		config    map[string]string
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid config succeeds",
			config:  validConfig,
			wantErr: false,
		},
		{
			name:      "missing appId",
			config:    withOverride(map[string]string{"appId": ""}),
			wantErr:   true,
			errSubstr: "appId",
		},
		{
			name:      "missing privateKey",
			config:    withOverride(map[string]string{"privateKey": ""}),
			wantErr:   true,
			errSubstr: "privateKey",
		},
		{
			name:      "nil config map returns error for appId",
			config:    map[string]string{},
			wantErr:   true,
			errSubstr: "appId",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewAlipay("test-instance", tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errSubstr != "" && !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got == nil {
				t.Fatal("expected non-nil Alipay instance")
			}
			if got.instanceID != "test-instance" {
				t.Errorf("instanceID = %q, want %q", got.instanceID, "test-instance")
			}
		})
	}
}

func TestCreateTradeUsesPagePayForDesktop(t *testing.T) {
	origPreCreate := alipayTradePreCreate
	origPagePay := alipayTradePagePay
	origWapPay := alipayTradeWapPay
	t.Cleanup(func() {
		alipayTradePreCreate = origPreCreate
		alipayTradePagePay = origPagePay
		alipayTradeWapPay = origWapPay
	})

	preCreateCalls := 0
	pagePayCalls := 0
	wapPayCalls := 0
	alipayTradePreCreate = func(ctx context.Context, client *alipay.Client, param alipay.TradePreCreate) (*alipay.TradePreCreateRsp, error) {
		preCreateCalls++
		return nil, errors.New("merchant does not have FACE_TO_FACE_PAYMENT")
	}
	alipayTradePagePay = func(client *alipay.Client, param alipay.TradePagePay) (*url.URL, error) {
		pagePayCalls++
		if param.OutTradeNo != "sub2_100" {
			t.Fatalf("out_trade_no = %q, want %q", param.OutTradeNo, "sub2_100")
		}
		if param.NotifyURL != "https://merchant.example.com/api/v1/payment/webhook/alipay" {
			t.Fatalf("notify_url = %q", param.NotifyURL)
		}
		return url.Parse("https://openapi.alipay.com/gateway.do?page-pay")
	}
	alipayTradeWapPay = func(client *alipay.Client, param alipay.TradeWapPay) (*url.URL, error) {
		wapPayCalls++
		return url.Parse("https://openapi.alipay.com/gateway.do?wap-pay")
	}

	provider := &Alipay{}
	resp, err := provider.createDesktopTrade(context.Background(), &alipay.Client{}, payment.CreatePaymentRequest{
		OrderID: "sub2_100",
		Amount:  "88.00",
		Subject: "Balance recharge",
	}, "https://merchant.example.com/api/v1/payment/webhook/alipay", "https://merchant.example.com/payment/result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preCreateCalls != 1 {
		t.Fatalf("precreate calls = %d, want 1", preCreateCalls)
	}
	if pagePayCalls != 1 {
		t.Fatalf("page pay calls = %d, want 1", pagePayCalls)
	}
	if wapPayCalls != 0 {
		t.Fatalf("wap pay calls = %d, want 0", wapPayCalls)
	}
	if resp.PayURL == "" {
		t.Fatal("expected pay_url for desktop page pay")
	}
	if resp.QRCode != resp.PayURL {
		t.Fatalf("qr_code = %q, want same as pay_url %q", resp.QRCode, resp.PayURL)
	}
}

func TestCreateTradeUsesWapPayForMobile(t *testing.T) {
	origWapPay := alipayTradeWapPay
	t.Cleanup(func() {
		alipayTradeWapPay = origWapPay
	})

	wapPayCalls := 0
	alipayTradeWapPay = func(client *alipay.Client, param alipay.TradeWapPay) (*url.URL, error) {
		wapPayCalls++
		if param.ReturnURL != "https://merchant.example.com/payment/result" {
			t.Fatalf("return_url = %q", param.ReturnURL)
		}
		return url.Parse("https://openapi.alipay.com/gateway.do?wap-pay")
	}

	provider := &Alipay{}
	resp, err := provider.createWapTrade(&alipay.Client{}, payment.CreatePaymentRequest{
		OrderID:  "sub2_101",
		Amount:   "18.00",
		Subject:  "Balance recharge",
		IsMobile: true,
	}, "https://merchant.example.com/api/v1/payment/webhook/alipay", "https://merchant.example.com/payment/result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wapPayCalls != 1 {
		t.Fatalf("wap pay calls = %d, want 1", wapPayCalls)
	}
	if resp.PayURL == "" {
		t.Fatal("expected pay_url for mobile wap pay")
	}
}

func TestCreateTradeUsesPrecreateForDesktopWhenAvailable(t *testing.T) {
	origPreCreate := alipayTradePreCreate
	origPagePay := alipayTradePagePay
	t.Cleanup(func() {
		alipayTradePreCreate = origPreCreate
		alipayTradePagePay = origPagePay
	})

	preCreateCalls := 0
	pagePayCalls := 0
	alipayTradePreCreate = func(ctx context.Context, client *alipay.Client, param alipay.TradePreCreate) (*alipay.TradePreCreateRsp, error) {
		preCreateCalls++
		if param.ProductCode != alipayProductCodePreCreate {
			t.Fatalf("product_code = %q, want %q", param.ProductCode, alipayProductCodePreCreate)
		}
		return &alipay.TradePreCreateRsp{
			Error:  alipay.Error{Code: alipay.CodeSuccess},
			QRCode: "https://qr.alipay.example.com/precreate-token",
		}, nil
	}
	alipayTradePagePay = func(client *alipay.Client, param alipay.TradePagePay) (*url.URL, error) {
		pagePayCalls++
		return url.Parse("https://openapi.alipay.com/gateway.do?page-pay")
	}

	provider := &Alipay{}
	resp, err := provider.createDesktopTrade(context.Background(), &alipay.Client{}, payment.CreatePaymentRequest{
		OrderID: "sub2_102",
		Amount:  "66.00",
		Subject: "Balance recharge",
	}, "https://merchant.example.com/api/v1/payment/webhook/alipay", "https://merchant.example.com/payment/result")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preCreateCalls != 1 {
		t.Fatalf("precreate calls = %d, want 1", preCreateCalls)
	}
	if pagePayCalls != 0 {
		t.Fatalf("page pay calls = %d, want 0", pagePayCalls)
	}
	if resp.QRCode != "https://qr.alipay.example.com/precreate-token" {
		t.Fatalf("qr_code = %q", resp.QRCode)
	}
	if resp.PayURL != "" {
		t.Fatalf("pay_url = %q, want empty for precreate", resp.PayURL)
	}
}

func TestCreateJSAPITradeUsesTradeCreate(t *testing.T) {
	origTradeCreate := alipayTradeCreate
	t.Cleanup(func() {
		alipayTradeCreate = origTradeCreate
	})

	tradeCreateCalls := 0
	alipayTradeCreate = func(ctx context.Context, client *alipay.Client, param alipay.TradeCreate) (*alipay.TradeCreateRsp, error) {
		tradeCreateCalls++
		if param.ProductCode != alipayProductCodeJSAPI {
			t.Fatalf("product_code = %q, want %q", param.ProductCode, alipayProductCodeJSAPI)
		}
		if param.OutTradeNo != "sub2_jsapi" {
			t.Fatalf("out_trade_no = %q", param.OutTradeNo)
		}
		if param.BuyerOpenId != "buyer-openid" {
			t.Fatalf("buyer_open_id = %q", param.BuyerOpenId)
		}
		if param.OpAppId != "202100opappid" || param.OpBuyerOpenId != "buyer-openid" {
			t.Fatalf("op app fields mismatch: op_app_id=%q op_buyer_open_id=%q", param.OpAppId, param.OpBuyerOpenId)
		}
		if param.NotifyURL != "https://merchant.example.com/api/v1/payment/webhook/alipay" {
			t.Fatalf("notify_url = %q", param.NotifyURL)
		}
		return &alipay.TradeCreateRsp{
			Error:      alipay.Error{Code: alipay.CodeSuccess},
			TradeNo:    "2026070122001400000000000001",
			OutTradeNo: "sub2_jsapi",
		}, nil
	}

	provider := &Alipay{config: map[string]string{"appId": "202100merchantappid", "opAppId": "202100opappid"}}
	resp, err := provider.createJSAPITrade(context.Background(), &alipay.Client{}, payment.CreatePaymentRequest{
		OrderID:     "sub2_jsapi",
		Amount:      "18.00",
		Subject:     "Balance recharge",
		BuyerOpenID: "buyer-openid",
	}, "https://merchant.example.com/api/v1/payment/webhook/alipay")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tradeCreateCalls != 1 {
		t.Fatalf("trade create calls = %d, want 1", tradeCreateCalls)
	}
	if resp.ResultType != payment.CreatePaymentResultJSAPIReady {
		t.Fatalf("result type = %q", resp.ResultType)
	}
	if resp.AlipayJSAPI == nil || resp.AlipayJSAPI.TradeNO != "2026070122001400000000000001" {
		t.Fatalf("unexpected alipay jsapi payload: %+v", resp.AlipayJSAPI)
	}
	if resp.AlipayJSAPI.AppID != "202100merchantappid" {
		t.Fatalf("alipay jsapi appId = %q, want %q", resp.AlipayJSAPI.AppID, "202100merchantappid")
	}
}

func TestCreateJSAPITradeRequiresBuyerIdentity(t *testing.T) {
	t.Parallel()

	provider := &Alipay{}
	_, err := provider.createJSAPITrade(context.Background(), &alipay.Client{}, payment.CreatePaymentRequest{
		OrderID: "sub2_jsapi",
		Amount:  "18.00",
		Subject: "Balance recharge",
	}, "https://merchant.example.com/api/v1/payment/webhook/alipay")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "buyerId or buyerOpenId") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestAlipayMerchantIdentityMetadata(t *testing.T) {
	t.Parallel()

	provider := &Alipay{
		config: map[string]string{
			"appId": "2021001234567890",
		},
	}

	metadata := provider.MerchantIdentityMetadata()
	if metadata["app_id"] != "2021001234567890" {
		t.Fatalf("app_id = %q, want %q", metadata["app_id"], "2021001234567890")
	}
}

func TestParseAlipayAmount(t *testing.T) {
	t.Parallel()

	amount, err := parseAlipayAmount("", "88.00", "77.00")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amount != 88 {
		t.Fatalf("amount = %v, want 88", amount)
	}

	if _, err := parseAlipayAmount("", "not-a-number"); err == nil {
		t.Fatal("expected error when no valid amount field exists")
	}
}

// 不变式回归：alipay 的 Refund() 绝不能返回 pending。
//
// 支付宝没有实现 payment.RefundQueryProvider，而 REFUND_PENDING 的唯一出口
// QueryAndFinalizeRefund 会在类型断言处直接 400。一旦 alipay 退款落进
// REFUND_PENDING，订单就没有任何代码出口：回查不了、也不能重发
// （refundInitiableStatuses 刻意排除该状态），只能改库。
//
// 历史成因：改造前 Refund() 按 fund_change != "Y" 判 pending，而 fund_change
// 的语义是「本次调用是否发生资金变化」——幂等重试一笔已成功的退款会返回 N。
func TestAlipayNeverImplementsRefundQueryProviderWithoutPending(t *testing.T) {
	var a any = &Alipay{}

	_, queryable := a.(payment.RefundQueryProvider)
	if queryable {
		t.Skip("alipay 已实现 RefundQueryProvider，本不变式可以放宽")
	}

	// 未实现回查 ⇒ 源码里不允许出现 ProviderStatusPending。
	src, err := os.ReadFile("alipay.go")
	if err != nil {
		t.Fatalf("read alipay.go: %v", err)
	}
	body := string(src)
	refundIdx := strings.Index(body, "func (a *Alipay) Refund(")
	if refundIdx < 0 {
		t.Fatal("cannot locate Alipay.Refund")
	}
	// 只看 Refund 函数体到下一个顶层 func 之间的片段。
	rest := body[refundIdx:]
	if end := strings.Index(rest[1:], "\nfunc "); end >= 0 {
		rest = rest[:end+1]
	}
	if strings.Contains(rest, "ProviderStatusPending") {
		t.Fatal("Alipay.Refund 会返回 pending，但 Alipay 未实现 RefundQueryProvider —— " +
			"这类订单会永久卡死在 REFUND_PENDING。要么让它不返回 pending，要么实现 QueryRefund。")
	}
}

// 三家实现了 QueryRefund 的 provider 必须满足 RefundQueryProvider 接口。
func TestRefundQueryProviderImplementors(t *testing.T) {
	var _ payment.RefundQueryProvider = (*Stripe)(nil)
	var _ payment.RefundQueryProvider = (*Wxpay)(nil)
	var _ payment.RefundQueryProvider = (*Airwallex)(nil)
}
