//go:build unit

package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func TestApplyWeChatPaymentResumeClaims(t *testing.T) {
	t.Parallel()

	req := CreateOrderRequest{
		Amount:      0,
		PaymentType: payment.TypeWxpay,
		OrderType:   payment.OrderTypeBalance,
	}

	err := applyWeChatPaymentResumeClaims(&req, &service.WeChatPaymentResumeClaims{
		OpenID:      "openid-123",
		PaymentType: payment.TypeWxpay,
		Amount:      "12.50",
		OrderType:   payment.OrderTypeSubscription,
		PlanID:      7,
	})
	if err != nil {
		t.Fatalf("applyWeChatPaymentResumeClaims returned error: %v", err)
	}
	if req.OpenID != "openid-123" {
		t.Fatalf("openid = %q, want %q", req.OpenID, "openid-123")
	}
	if req.Amount != 12.5 {
		t.Fatalf("amount = %v, want 12.5", req.Amount)
	}
	if req.OrderType != payment.OrderTypeSubscription {
		t.Fatalf("order_type = %q, want %q", req.OrderType, payment.OrderTypeSubscription)
	}
	if req.PlanID != 7 {
		t.Fatalf("plan_id = %d, want 7", req.PlanID)
	}
}

func TestApplyWeChatPaymentResumeClaimsRejectsPaymentTypeMismatch(t *testing.T) {
	t.Parallel()

	req := CreateOrderRequest{
		PaymentType: payment.TypeAlipay,
	}

	err := applyWeChatPaymentResumeClaims(&req, &service.WeChatPaymentResumeClaims{
		OpenID:      "openid-123",
		PaymentType: payment.TypeWxpay,
		Amount:      "12.50",
		OrderType:   payment.OrderTypeBalance,
	})
	if err == nil {
		t.Fatal("applyWeChatPaymentResumeClaims should reject mismatched payment types")
	}
}

func TestVerifyOrderPublicReturnsGone(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	db, err := sql.Open("sqlite", "file:payment_handler_public_verify?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })

	paymentSvc := service.NewPaymentService(client, payment.NewRegistry(), nil, nil, nil, nil, nil, nil)
	h := NewPaymentHandler(paymentSvc, nil, nil)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/payment/public/orders/verify",
		bytes.NewBufferString(`{"out_trade_no":"legacy-order-no"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.VerifyOrderPublic(ctx)

	require.Equal(t, http.StatusGone, recorder.Code)

	var resp response.Response
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, http.StatusGone, resp.Code)
	require.Equal(t, "PAYMENT_PUBLIC_ORDER_VERIFY_REMOVED", resp.Reason)
	require.Contains(t, resp.Message, "removed")
}
