//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// 覆盖 Codex plan-gated 模型冷却链（上游 5aeb03018 + b5d9fd21b + 02fbcbe3a）：
// 1. ChatGPT OAuth 账号对 plan-gated 模型返回 400 时按 model-not-found 处理，写 30 分钟冷却。
// 2. 图片模型被文本端点拒绝时不写冷却（确定性端点错配，不是账号能力缺失）。
// 3. 请求从 /v1/images/* 入站时保留冷却（真实的能力缺失需要刹车）。
// 4. 守卫口径与冷却键一致（account.GetMappedModel 可把文本别名映射到 gpt-image-*）。

func TestIsOpenAICodexPlanGatedModelError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		statusCode int
		body       []byte
		want       bool
	}{
		{"400 codex plan gated detail", http.StatusBadRequest, []byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`), true},
		{"400 codex plan gated error message", http.StatusBadRequest, []byte(`{"error":{"message":"The 'gpt-5.4' model is not supported when using Codex with a ChatGPT account."}}`), true},
		{"400 unrelated invalid request", http.StatusBadRequest, []byte(`{"error":{"message":"Invalid schema for response_format 'agentic_plan'"}}`), false},
		{"404 with plan gated message", http.StatusNotFound, []byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`), false},
		{"400 empty body", http.StatusBadRequest, nil, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isOpenAICodexPlanGatedModelError(tt.statusCode, tt.body))
		})
	}
}

func openAICodexPlanGatedOAuthAccount() *Account {
	return &Account{
		ID:          202,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{},
	}
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelUsesModelRateLimit(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"gpt-5.6-sol",
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`),
	)

	require.True(t, handled)
	require.Zero(t, repo.tempCalls)
	require.Len(t, repo.modelRateLimitCalls, 1)
	call := repo.modelRateLimitCalls[0]
	require.Equal(t, account.ID, call.accountID)
	require.Equal(t, "gpt-5.6-sol", call.modelKey)
	require.Equal(t, upstreamCodexPlanGatedModelReason, call.reason)
	require.WithinDuration(t, time.Now().Add(upstreamCodexPlanGatedModelCooldown), call.resetAt, 5*time.Second)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelRespectsModelMapping(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-5.6-sol": "gpt-5.6-sol-upstream"}

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"gpt-5.6-sol",
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol-upstream' model is not supported when using Codex with a ChatGPT account."}`),
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "gpt-5.6-sol-upstream", repo.modelRateLimitCalls[0].modelKey)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedModelIgnoresAPIKeyAccount(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()
	account.Type = AccountTypeAPIKey

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"gpt-5.6-sol",
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`),
	)

	require.False(t, handled)
	require.Empty(t, repo.modelRateLimitCalls)
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedImageModelSkipsCooldown(t *testing.T) {
	t.Parallel()
	for _, model := range []string{"gpt-image-1", "gpt-image-1.5", "gpt-image-2"} {
		model := model
		t.Run(model, func(t *testing.T) {
			t.Parallel()
			repo := &rateLimitAccountRepoStub{}
			svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := openAICodexPlanGatedOAuthAccount()

			handled := svc.HandleUpstreamErrorForModel(
				context.Background(),
				account,
				model,
				http.StatusBadRequest,
				http.Header{},
				[]byte(`{"detail":"The '`+model+`' model is not supported when using Codex with a ChatGPT account."}`),
			)

			require.True(t, handled, "attempt should still fail over")
			require.Empty(t, repo.modelRateLimitCalls,
				"image models must not be cooled down: the account still serves them over /v1/images/*")
			require.Zero(t, repo.tempCalls)
		})
	}
}

func TestRateLimitService_HandleUpstreamError_CodexPlanGatedTextModelStillCoolsDown(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"gpt-5.6-sol",
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-5.6-sol' model is not supported when using Codex with a ChatGPT account."}`),
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1, "non-image plan-gated models keep the existing cooldown")
	require.Equal(t, upstreamCodexPlanGatedModelReason, repo.modelRateLimitCalls[0].reason)
}

// 请求本身就走 /v1/images/* 时必须保留冷却：真实的能力缺失需要刹车，
// 否则每个生图请求都会完整走一遍号池，对上游形成无上界的 400 放大。
func TestRateLimitService_HandleUpstreamError_CodexPlanGatedImageModelKeepsCooldownOnImagesEndpoint(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()

	handled := svc.HandleUpstreamErrorForModel(
		WithOpenAIImagesEndpoint(context.Background()),
		account,
		"gpt-image-2",
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-image-2' model is not supported when using Codex with a ChatGPT account."}`),
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1,
		"/v1/images/* 上的 plan-gated 拒绝是真实的能力缺失，必须保留冷却刹车")
	require.Equal(t, "gpt-image-2", repo.modelRateLimitCalls[0].modelKey)
	require.Equal(t, upstreamCodexPlanGatedModelReason, repo.modelRateLimitCalls[0].reason)
}

// 守卫口径必须与冷却键一致：冷却键走 account.GetMappedModel，账号可以把文本别名
// 映射到 gpt-image-*，只判请求模型会漏掉这种形态。
func TestRateLimitService_HandleUpstreamError_CodexPlanGatedImageModelSkipsCooldownViaModelMapping(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()
	account.Credentials["model_mapping"] = map[string]any{"my-draw-alias": "gpt-image-2"}

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"my-draw-alias",
		http.StatusBadRequest,
		http.Header{},
		[]byte(`{"detail":"The 'gpt-image-2' model is not supported when using Codex with a ChatGPT account."}`),
	)

	require.True(t, handled)
	require.Empty(t, repo.modelRateLimitCalls,
		"映射后的上游模型是图片模型，冷却键会写到 gpt-image-2 上，守卫必须一并识别")
}

// 404 model-not-found 分支不受守卫影响：即使是图片模型也照常冷却。
func TestRateLimitService_HandleUpstreamError_ModelNotFoundImageModelStillCoolsDown(t *testing.T) {
	t.Parallel()
	repo := &rateLimitAccountRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	account := openAICodexPlanGatedOAuthAccount()

	handled := svc.HandleUpstreamErrorForModel(
		context.Background(),
		account,
		"gpt-image-2",
		http.StatusNotFound,
		http.Header{},
		[]byte(`{"error":{"message":"The model 'gpt-image-2' does not exist","code":"model_not_found"}}`),
	)

	require.True(t, handled)
	require.Len(t, repo.modelRateLimitCalls, 1, "守卫只作用于 codex plan-gated 分支")
	require.Equal(t, upstreamModelNotFoundReason, repo.modelRateLimitCalls[0].reason)
}
