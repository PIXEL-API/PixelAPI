//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateGeminiTestPayload_ImageModelFallsBackToTextOnly(t *testing.T) {
	t.Parallel()

	payload := createGeminiTestPayload("gemini-2.5-flash-image", "draw a tiny robot")

	var parsed struct {
		Contents []struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}

	require.NoError(t, json.Unmarshal(payload, &parsed))
	require.Len(t, parsed.Contents, 1)
	require.Len(t, parsed.Contents[0].Parts, 1)
	require.Equal(t, "draw a tiny robot", parsed.Contents[0].Parts[0].Text)
	require.NotContains(t, string(payload), "responseModalities")
	require.NotContains(t, string(payload), "imageConfig")
}

func TestProcessGeminiStream_EmitsImageEvent(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	ctx, recorder := newTestContext()
	svc := &AccountTestService{}

	stream := strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"},{\"inlineData\":{\"mimeType\":\"image/png\",\"data\":\"QUJD\"}}]}}]}\n\ndata: [DONE]\n\n")

	err := svc.processGeminiStream(ctx, stream)
	require.NoError(t, err)

	body := recorder.Body.String()
	require.Contains(t, body, "\"type\":\"content\"")
	require.Contains(t, body, "\"text\":\"ok\"")
	require.Contains(t, body, "\"type\":\"image\"")
	require.Contains(t, body, "\"image_url\":\"data:image/png;base64,QUJD\"")
	require.Contains(t, body, "\"mime_type\":\"image/png\"")
}

// TestReconcileGeminiTestErrorState 验证 Gemini 账号"测试连接"返回错误状态码时，
// 账号状态被正确同步（401/403 -> error，429 -> rate-limited），
// 与 Claude/OpenAI 测试路径及运行时网关行为对齐。
func TestReconcileGeminiTestErrorState(t *testing.T) {
	const accountID int64 = 77

	t.Run("401 标记账号为 error", func(t *testing.T) {
		repo := &openAIAccountTestRepo{}
		svc := &AccountTestService{accountRepo: repo}
		account := &Account{ID: accountID, Platform: PlatformGemini, Type: AccountTypeOAuth}

		svc.reconcileGeminiTestErrorState(context.Background(), account, http.StatusUnauthorized, nil, []byte(`{"error":"unauthorized"}`))

		require.Equal(t, accountID, repo.setErrorID)
		require.Contains(t, repo.setErrorMsg, "401")
		require.Nil(t, repo.rateLimitedAt, "401 不应触发限流")
	})

	t.Run("403 标记账号为 error", func(t *testing.T) {
		repo := &openAIAccountTestRepo{}
		svc := &AccountTestService{accountRepo: repo}
		account := &Account{ID: accountID, Platform: PlatformGemini, Type: AccountTypeOAuth}

		svc.reconcileGeminiTestErrorState(context.Background(), account, http.StatusForbidden, nil, []byte(`{"error":"forbidden"}`))

		require.Equal(t, accountID, repo.setErrorID)
		require.Contains(t, repo.setErrorMsg, "403")
	})

	t.Run("429 标记账号为限流", func(t *testing.T) {
		repo := &openAIAccountTestRepo{}
		svc := &AccountTestService{accountRepo: repo}
		account := &Account{ID: accountID, Platform: PlatformGemini, Type: AccountTypeOAuth}

		svc.reconcileGeminiTestErrorState(context.Background(), account, http.StatusTooManyRequests, nil, []byte(`{"error":{"message":"rate limited"}}`))

		require.Equal(t, accountID, repo.rateLimitedID)
		require.NotNil(t, repo.rateLimitedAt, "429 应设置限流重置时间")
		require.True(t, repo.rateLimitedAt.After(time.Now()), "限流重置时间应在未来")
		require.Zero(t, repo.setErrorID, "429 不应标记为 error")
	})

	t.Run("500 等其它状态码不改变账号状态", func(t *testing.T) {
		repo := &openAIAccountTestRepo{}
		svc := &AccountTestService{accountRepo: repo}
		account := &Account{ID: accountID, Platform: PlatformGemini, Type: AccountTypeOAuth}

		svc.reconcileGeminiTestErrorState(context.Background(), account, http.StatusInternalServerError, nil, []byte("boom"))

		require.Zero(t, repo.setErrorID)
		require.Nil(t, repo.rateLimitedAt)
	})

	t.Run("APIKey 账号自定义错误码策略未命中时跳过", func(t *testing.T) {
		repo := &openAIAccountTestRepo{}
		svc := &AccountTestService{accountRepo: repo}
		// 自定义错误码仅对 APIKey 账号生效，此处仅处理 429，因此 401 应被跳过。
		account := &Account{
			ID:       accountID,
			Platform: PlatformGemini,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"custom_error_codes_enabled": true,
				"custom_error_codes":         []any{float64(429)},
			},
		}

		svc.reconcileGeminiTestErrorState(context.Background(), account, http.StatusUnauthorized, nil, []byte("nope"))

		require.Zero(t, repo.setErrorID, "自定义错误码未命中 401 时不应标记 error")
	})

	t.Run("nil accountRepo 不 panic", func(t *testing.T) {
		svc := &AccountTestService{}
		account := &Account{ID: accountID, Platform: PlatformGemini, Type: AccountTypeOAuth}
		require.NotPanics(t, func() {
			svc.reconcileGeminiTestErrorState(context.Background(), account, http.StatusUnauthorized, nil, []byte("x"))
		})
	})
}
