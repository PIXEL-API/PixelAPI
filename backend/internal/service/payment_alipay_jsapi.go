package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/payment/provider"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/smartwalle/alipay/v3"
)

type AlipayJSAPIStartRequest struct {
	ResumeToken string
	AuthCode    string
	BuyerID     string
	BuyerOpenID string
}

var alipaySystemOauthToken = func(ctx context.Context, client *alipay.Client, param alipay.SystemOauthToken) (*alipay.SystemOauthTokenRsp, error) {
	return client.SystemOauthToken(ctx, param)
}

func (s *PaymentService) StartAlipayJSAPIByResumeToken(ctx context.Context, req AlipayJSAPIStartRequest) (*CreateOrderResponse, error) {
	resumeToken := strings.TrimSpace(req.ResumeToken)
	if resumeToken == "" {
		return nil, infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token is required")
	}

	result, err, _ := s.alipayJSAPIStartSF.Do(resumeToken, func() (any, error) {
		req.ResumeToken = resumeToken
		return s.startAlipayJSAPIByResumeToken(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	resp, ok := result.(*CreateOrderResponse)
	if !ok || resp == nil {
		return nil, fmt.Errorf("alipay jsapi start returned invalid response")
	}
	return resp, nil
}

func (s *PaymentService) startAlipayJSAPIByResumeToken(ctx context.Context, req AlipayJSAPIStartRequest) (*CreateOrderResponse, error) {
	resumeToken := strings.TrimSpace(req.ResumeToken)
	claims, err := s.paymentResume().ParseToken(resumeToken)
	if err != nil {
		return nil, err
	}
	order, err := s.entClient.PaymentOrder.Get(ctx, claims.OrderID)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, infraerrors.NotFound("NOT_FOUND", "order not found")
		}
		return nil, fmt.Errorf("get alipay jsapi order: %w", err)
	}
	if err := validateAlipayJSAPIResumeClaims(order, claims); err != nil {
		return nil, err
	}
	if err := validateAlipayJSAPIOrderStartable(order); err != nil {
		return nil, err
	}
	if tradeNo := strings.TrimSpace(order.PaymentTradeNo); tradeNo != "" {
		resp := buildCreateOrderResponse(order, CreateOrderRequest{PaymentType: payment.TypeAlipay}, order.PayAmount, &payment.InstanceSelection{
			InstanceID:  strings.TrimSpace(psStringValue(order.ProviderInstanceID)),
			ProviderKey: payment.TypeAlipay,
			PaymentMode: "jsapi",
		}, &payment.CreatePaymentResponse{
			TradeNo:    tradeNo,
			ResultType: payment.CreatePaymentResultJSAPIReady,
			AlipayJSAPI: &payment.AlipayJSAPIPayload{
				TradeNO: tradeNo,
				AppID:   alipayJSAPIAppIDForOrder(order, nil),
			},
		}, payment.CreatePaymentResultJSAPIReady)
		resp.ResumeToken = resumeToken
		return resp, nil
	}

	snapshot := psOrderProviderSnapshot(order)
	if snapshot == nil {
		return nil, infraerrors.BadRequest("INVALID_RESUME_TOKEN", "payment order provider snapshot is missing")
	}
	inst, err := s.resolveSnapshotOrderProviderInstance(ctx, order, snapshot)
	if err != nil {
		return nil, fmt.Errorf("resolve alipay jsapi provider instance: %w", err)
	}
	if inst == nil || !strings.EqualFold(strings.TrimSpace(inst.ProviderKey), payment.TypeAlipay) {
		return nil, infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token does not reference an alipay provider")
	}

	cfg, err := s.loadBalancer.GetInstanceConfig(ctx, int64(inst.ID))
	if err != nil {
		return nil, fmt.Errorf("load alipay jsapi provider config: %w", err)
	}
	cfg["paymentMode"] = snapshot.PaymentMode
	appID := alipayJSAPIAppIDForOrder(order, &payment.InstanceSelection{Config: cfg})
	prov, err := provider.CreateProvider(inst.ProviderKey, strconv.FormatInt(int64(inst.ID), 10), cfg)
	if err != nil {
		return nil, fmt.Errorf("create alipay jsapi provider: %w", err)
	}
	alipayProvider, ok := prov.(*provider.Alipay)
	if !ok {
		return nil, fmt.Errorf("selected provider is not alipay")
	}

	buyerID := strings.TrimSpace(req.BuyerID)
	buyerOpenID := strings.TrimSpace(req.BuyerOpenID)
	if buyerID == "" && buyerOpenID == "" {
		tokenRsp, err := exchangeAlipayJSAPIAuthCode(ctx, alipayProvider, strings.TrimSpace(req.AuthCode))
		if err != nil {
			return nil, err
		}
		buyerID = strings.TrimSpace(tokenRsp.UserId)
		buyerOpenID = strings.TrimSpace(tokenRsp.OpenId)
	}
	if buyerID == "" && buyerOpenID == "" {
		return nil, infraerrors.BadRequest("ALIPAY_JSAPI_BUYER_REQUIRED", "alipay jsapi requires buyer_id or buyer_open_id")
	}

	providerReq := payment.CreatePaymentRequest{
		OrderID:     order.OutTradeNo,
		Amount:      payment.FormatAmountForCurrency(order.PayAmount, PaymentOrderCurrency(order)),
		PaymentType: payment.TypeAlipay,
		Subject:     alipayJSAPIOrderSubject(order),
		NotifyURL:   cfg["notifyUrl"],
		BuyerID:     buyerID,
		BuyerOpenID: buyerOpenID,
		ClientIP:    order.ClientIP,
		IsMobile:    true,
	}
	pr, err := prov.CreatePayment(ctx, providerReq)
	if err != nil {
		if replayResp, replayErr := s.buildExistingAlipayJSAPIStartResponse(ctx, order.ID, resumeToken, appID); replayErr == nil && replayResp != nil {
			return replayResp, nil
		}
		return nil, classifyCreatePaymentError(CreateOrderRequest{PaymentType: payment.TypeAlipay}, payment.TypeAlipay, err)
	}
	if pr == nil || pr.AlipayJSAPI == nil || strings.TrimSpace(pr.AlipayJSAPI.TradeNO) == "" {
		return nil, fmt.Errorf("alipay jsapi provider returned empty trade_no")
	}
	if pr.AlipayJSAPI.AppID = strings.TrimSpace(pr.AlipayJSAPI.AppID); pr.AlipayJSAPI.AppID == "" {
		pr.AlipayJSAPI.AppID = appID
	}

	updated, err := s.entClient.PaymentOrder.UpdateOneID(order.ID).
		SetPaymentTradeNo(pr.TradeNo).
		ClearPayURL().
		ClearQrCode().
		SetNillableProviderInstanceID(psNilIfEmpty(strconv.FormatInt(int64(inst.ID), 10))).
		SetNillableProviderKey(psNilIfEmpty(inst.ProviderKey)).
		Save(ctx)
	if err != nil {
		return nil, &paymentProviderOrderPossiblyCreatedError{err: fmt.Errorf("update alipay jsapi order with payment details: %w", err)}
	}

	s.writeAuditLog(ctx, order.ID, "ALIPAY_JSAPI_TRADE_CREATED", "public:alipay_jsapi", map[string]any{
		"tradeNo":     pr.TradeNo,
		"paymentType": payment.TypeAlipay,
		"paymentMode": "jsapi",
	})

	resp := buildCreateOrderResponse(updated, CreateOrderRequest{PaymentType: payment.TypeAlipay}, updated.PayAmount, &payment.InstanceSelection{
		InstanceID:  strconv.FormatInt(int64(inst.ID), 10),
		ProviderKey: inst.ProviderKey,
		Config:      cfg,
		PaymentMode: snapshot.PaymentMode,
	}, pr, payment.CreatePaymentResultJSAPIReady)
	resp.ResumeToken = resumeToken
	return resp, nil
}

func (s *PaymentService) buildExistingAlipayJSAPIStartResponse(ctx context.Context, orderID int64, resumeToken, appID string) (*CreateOrderResponse, error) {
	order, err := s.entClient.PaymentOrder.Get(ctx, orderID)
	if err != nil {
		return nil, err
	}
	tradeNo := strings.TrimSpace(order.PaymentTradeNo)
	if tradeNo == "" {
		return nil, nil
	}
	if appID = strings.TrimSpace(appID); appID == "" {
		appID = alipayJSAPIAppIDForOrder(order, nil)
	}
	resp := buildCreateOrderResponse(order, CreateOrderRequest{PaymentType: payment.TypeAlipay}, order.PayAmount, &payment.InstanceSelection{
		InstanceID:  strings.TrimSpace(psStringValue(order.ProviderInstanceID)),
		ProviderKey: payment.TypeAlipay,
		PaymentMode: "jsapi",
	}, &payment.CreatePaymentResponse{
		TradeNo:    tradeNo,
		ResultType: payment.CreatePaymentResultJSAPIReady,
		AlipayJSAPI: &payment.AlipayJSAPIPayload{
			TradeNO: tradeNo,
			AppID:   appID,
		},
	}, payment.CreatePaymentResultJSAPIReady)
	resp.ResumeToken = resumeToken
	return resp, nil
}

func validateAlipayJSAPIResumeClaims(order *dbent.PaymentOrder, claims *ResumeTokenClaims) error {
	if order == nil || claims == nil {
		return infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token does not match the payment order")
	}
	if claims.UserID > 0 && order.UserID != claims.UserID {
		return invalidResumeTokenMatchError()
	}
	if claims.PaymentType != "" && NormalizeVisibleMethod(order.PaymentType) != NormalizeVisibleMethod(claims.PaymentType) {
		return invalidResumeTokenMatchError()
	}
	snapshot := psOrderProviderSnapshot(order)
	if snapshot == nil {
		return infraerrors.BadRequest("INVALID_RESUME_TOKEN", "payment order provider snapshot is missing")
	}
	if claims.ProviderInstanceID != "" && snapshot.ProviderInstanceID != claims.ProviderInstanceID {
		return invalidResumeTokenMatchError()
	}
	if claims.ProviderKey != "" && !strings.EqualFold(snapshot.ProviderKey, claims.ProviderKey) {
		return invalidResumeTokenMatchError()
	}
	if !strings.EqualFold(snapshot.ProviderKey, payment.TypeAlipay) {
		return infraerrors.BadRequest("INVALID_RESUME_TOKEN", "resume token does not reference an alipay provider")
	}
	if !strings.EqualFold(snapshot.PaymentMode, "jsapi") {
		return infraerrors.BadRequest("INVALID_ALIPAY_JSAPI_ORDER", "payment order is not configured for alipay jsapi")
	}
	return nil
}

func validateAlipayJSAPIOrderStartable(order *dbent.PaymentOrder) error {
	if order == nil {
		return infraerrors.NotFound("NOT_FOUND", "order not found")
	}
	if NormalizeVisibleMethod(order.PaymentType) != payment.TypeAlipay {
		return infraerrors.BadRequest("INVALID_ALIPAY_JSAPI_ORDER", "payment order is not an alipay order")
	}
	if order.Status != OrderStatusPending {
		return infraerrors.BadRequest("INVALID_ALIPAY_JSAPI_ORDER", "payment order is not pending")
	}
	if !order.ExpiresAt.IsZero() && time.Now().After(order.ExpiresAt) {
		return infraerrors.BadRequest("ORDER_EXPIRED", "payment order has expired")
	}
	return nil
}

func exchangeAlipayJSAPIAuthCode(ctx context.Context, provider *provider.Alipay, authCode string) (*alipay.SystemOauthTokenRsp, error) {
	authCode = strings.TrimSpace(authCode)
	if authCode == "" {
		return nil, infraerrors.BadRequest("ALIPAY_AUTH_CODE_REQUIRED", "alipay auth_code is required")
	}
	client, err := provider.Client()
	if err != nil {
		return nil, fmt.Errorf("create alipay oauth client: %w", err)
	}
	rsp, err := alipaySystemOauthToken(ctx, client, alipay.SystemOauthToken{
		GrantType: "authorization_code",
		Code:      authCode,
	})
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("ALIPAY_AUTH_FAILED", "alipay auth code exchange failed").WithCause(err)
	}
	if rsp == nil {
		return nil, infraerrors.ServiceUnavailable("ALIPAY_AUTH_FAILED", "alipay auth code exchange returned empty response")
	}
	if strings.TrimSpace(rsp.AccessToken) == "" || (strings.TrimSpace(rsp.UserId) == "" && strings.TrimSpace(rsp.OpenId) == "") {
		if rsp.IsFailure() {
			return nil, infraerrors.ServiceUnavailable("ALIPAY_AUTH_FAILED", "alipay auth code exchange failed").WithMetadata(map[string]string{
				"sub_code": strings.TrimSpace(rsp.SubCode),
			})
		}
		return nil, infraerrors.ServiceUnavailable("ALIPAY_AUTH_FAILED", "alipay auth code exchange returned no buyer identity")
	}
	return rsp, nil
}

func alipayJSAPIOrderSubject(order *dbent.PaymentOrder) string {
	if order == nil {
		return "Sub2API Payment"
	}
	switch order.OrderType {
	case payment.OrderTypeSubscription:
		return "Sub2API Subscription"
	case payment.OrderTypeShop:
		return "Sub2API Store Order"
	default:
		return "Sub2API " + payment.FormatAmountForCurrency(order.Amount, PaymentOrderCurrency(order)) + " " + PaymentOrderCurrency(order)
	}
}
