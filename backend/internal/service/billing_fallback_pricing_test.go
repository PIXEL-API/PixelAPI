//go:build unit

package service

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureBillingLog 临时把标准库 log 输出重定向到 buffer，返回捕获到的内容。
// 生产链路上这些 warn 会经 log 桥接落到 ops_system_logs，所以这里直接断言行数。
func captureBillingLog(t *testing.T, fn func()) string {
	t.Helper()

	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	origPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
		log.SetPrefix(origPrefix)
	})

	fn()

	log.SetOutput(origOut)
	log.SetFlags(origFlags)
	log.SetPrefix(origPrefix)
	return buf.String()
}

func countFallbackWarnLines(out, model string) int {
	needle := "[Billing] Using fallback pricing for model: " + model
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == needle {
			n++
		}
	}
	return n
}

// TestGetFallbackPricing_ChineseProviders 覆盖新补的国产 LLM 兜底定价条目。
// 这些模型此前在本 fork 里完全没有兜底价，GetModelPricing 会直接返回
// ErrModelPricingUnavailable，导致命中兜底路径时计费不准。
func TestGetFallbackPricing_ChineseProviders(t *testing.T) {
	svc := newTestBillingService()

	tests := []struct {
		name            string
		model           string
		expectInput     float64
		expectOutput    float64
		expectCacheRead float64
	}{
		// DeepSeek V4
		{name: "deepseek v4 pro", model: "deepseek-v4-pro", expectInput: 4.35e-7, expectOutput: 8.7e-7, expectCacheRead: 3.625e-9},
		{name: "deepseek v4 flash", model: "deepseek-v4-flash", expectInput: 1.4e-7, expectOutput: 2.8e-7, expectCacheRead: 2.8e-9},
		{name: "deepseek chat alias maps to v4 flash", model: "deepseek-chat", expectInput: 1.4e-7, expectOutput: 2.8e-7, expectCacheRead: 2.8e-9},
		{name: "deepseek reasoner alias maps to v4 flash", model: "deepseek-reasoner", expectInput: 1.4e-7, expectOutput: 2.8e-7, expectCacheRead: 2.8e-9},

		// 智谱 GLM
		{name: "glm 5.2 does not fall into glm-5", model: "glm-5.2", expectInput: 1.4e-6, expectOutput: 4.4e-6, expectCacheRead: 0.26e-6},
		{name: "glm 5.1", model: "glm-5.1", expectInput: 1.4e-6, expectOutput: 4.4e-6, expectCacheRead: 0.26e-6},
		{name: "glm 5", model: "glm-5", expectInput: 1e-6, expectOutput: 3.2e-6, expectCacheRead: 0.2e-6},
		{name: "glm 5 turbo", model: "glm-5-turbo", expectInput: 1.2e-6, expectOutput: 4e-6, expectCacheRead: 0.24e-6},
		{name: "glm 4.7", model: "glm-4.7", expectInput: 0.6e-6, expectOutput: 2.2e-6, expectCacheRead: 0.11e-6},
		{name: "glm 4.7 flashx wins over flash", model: "glm-4.7-flashx", expectInput: 0.07e-6, expectOutput: 0.4e-6, expectCacheRead: 0.01e-6},
		{name: "glm 4.7 flash is free tier", model: "glm-4.7-flash", expectInput: 0, expectOutput: 0, expectCacheRead: 0},
		{name: "glm 4.6", model: "glm-4.6", expectInput: 0.6e-6, expectOutput: 2.2e-6, expectCacheRead: 0.11e-6},
		{name: "glm 4.5", model: "glm-4.5", expectInput: 0.6e-6, expectOutput: 2.2e-6, expectCacheRead: 0.11e-6},
		{name: "glm 4.5 x", model: "glm-4.5-x", expectInput: 2.2e-6, expectOutput: 8.9e-6, expectCacheRead: 0.45e-6},
		{name: "glm 4.5 airx wins over air", model: "glm-4.5-airx", expectInput: 1.1e-6, expectOutput: 4.5e-6, expectCacheRead: 0.22e-6},
		{name: "glm 4.5 air", model: "glm-4.5-air", expectInput: 0.2e-6, expectOutput: 1.1e-6, expectCacheRead: 0.03e-6},
		{name: "glm 4.5 flash is free tier", model: "glm-4.5-flash", expectInput: 0, expectOutput: 0, expectCacheRead: 0},
		{name: "glm 4 32b", model: "glm-4-32b-0414-128k", expectInput: 0.1e-6, expectOutput: 0.1e-6, expectCacheRead: 0},
		{name: "glm case insensitive via GetModelPricing lowering", model: "glm-4.6", expectInput: 0.6e-6, expectOutput: 2.2e-6, expectCacheRead: 0.11e-6},

		// 月之暗面 Kimi
		{name: "kimi k3 exact", model: "kimi-k3", expectInput: 3e-6, expectOutput: 15e-6, expectCacheRead: 0.30e-6},
		{name: "kimi k3 1m context suffix strips to k3", model: "kimi-k3[1m]", expectInput: 3e-6, expectOutput: 15e-6, expectCacheRead: 0.30e-6},
		{name: "kimi k3 bare code alias", model: "k3", expectInput: 3e-6, expectOutput: 15e-6, expectCacheRead: 0.30e-6},
		{name: "kimi k3 256k code alias", model: "k3-256k", expectInput: 3e-6, expectOutput: 15e-6, expectCacheRead: 0.30e-6},
		{name: "kimi k3 path suffix", model: "moonshot/kimi-k3", expectInput: 3e-6, expectOutput: 15e-6, expectCacheRead: 0.30e-6},
		{name: "kimi k2.6", model: "kimi-k2.6", expectInput: 0.95e-6, expectOutput: 4e-6, expectCacheRead: 0.15e-6},
		{name: "kimi for coding", model: "kimi-for-coding", expectInput: 0.95e-6, expectOutput: 4e-6, expectCacheRead: 0.15e-6},
		{name: "kimi k2.5", model: "kimi-k2.5", expectInput: 0.60e-6, expectOutput: 3e-6, expectCacheRead: 0.098e-6},
		{name: "kimi k2 thinking", model: "kimi-k2-thinking", expectInput: 0.56e-6, expectOutput: 2.24e-6, expectCacheRead: 0.14e-6},
		{name: "kimi k2", model: "kimi-k2", expectInput: 0.56e-6, expectOutput: 2.24e-6, expectCacheRead: 0.14e-6},
		{name: "kimi k2 0905 falls back to k2", model: "kimi-k2-0905-preview", expectInput: 0.56e-6, expectOutput: 2.24e-6, expectCacheRead: 0.14e-6},

		// MiniMax
		{name: "minimax m3", model: "minimax-m3", expectInput: 0.60e-6, expectOutput: 2.40e-6, expectCacheRead: 0.12e-6},
		{name: "minimax m2.7 highspeed wins over m2.7", model: "minimax-m2.7-highspeed", expectInput: 0.60e-6, expectOutput: 2.40e-6, expectCacheRead: 0.06e-6},
		{name: "minimax m2.7", model: "minimax-m2.7", expectInput: 0.30e-6, expectOutput: 1.20e-6, expectCacheRead: 0.06e-6},
		{name: "minimax m2.5", model: "minimax-m2.5", expectInput: 0.30e-6, expectOutput: 1.20e-6, expectCacheRead: 0.03e-6},
		{name: "minimax m2.1", model: "minimax-m2.1", expectInput: 0.30e-6, expectOutput: 1.20e-6, expectCacheRead: 0.03e-6},
		{name: "minimax m2", model: "minimax-m2", expectInput: 0.30e-6, expectOutput: 1.20e-6, expectCacheRead: 0.03e-6},

		// 火山方舟豆包 embedding
		{name: "doubao embedding vision", model: "doubao-embedding-vision", expectInput: 0.098e-6, expectOutput: 0, expectCacheRead: 0},
		{name: "doubao embedding vision versioned alias", model: "doubao-embedding-vision-251215", expectInput: 0.098e-6, expectOutput: 0, expectCacheRead: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pricing := svc.getFallbackPricing(tt.model)
			require.NotNil(t, pricing, "model %s should have fallback pricing", tt.model)
			require.InDelta(t, tt.expectInput, pricing.InputPricePerToken, 1e-12, "input price")
			require.InDelta(t, tt.expectOutput, pricing.OutputPricePerToken, 1e-12, "output price")
			require.InDelta(t, tt.expectCacheRead, pricing.CacheReadPricePerToken, 1e-12, "cache read price")
		})
	}
}

// TestGetFallbackPricing_ChineseProviders_ImageInputPrice 单独断言豆包图文差别定价，
// 因为它是本批唯一使用 ImageInputPricePerToken 的条目。
func TestGetFallbackPricing_ChineseProviders_ImageInputPrice(t *testing.T) {
	svc := newTestBillingService()

	pricing := svc.getFallbackPricing("doubao-embedding-vision")
	require.NotNil(t, pricing)
	require.InDelta(t, 0.098e-6, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, 0.252e-6, pricing.ImageInputPricePerToken, 1e-12)
	require.Greater(t, pricing.ImageInputPricePerToken, pricing.InputPricePerToken,
		"豆包图片输入应比文本输入贵，否则说明图文档位配反了")
}

// TestGetModelPricing_ChineseProviders_EndToEnd 走完整 GetModelPricing 链路，
// 确认新条目经 applyModelSpecificPricingPolicy 后价格不被改写
// （该 policy 只针对 OpenAI GPT-5.4/5.5/5.6 族，国产模型应原样透传）。
func TestGetModelPricing_ChineseProviders_EndToEnd(t *testing.T) {
	svc := newTestBillingService()

	cases := []struct {
		model        string
		expectInput  float64
		expectOutput float64
	}{
		{model: "glm-4.6", expectInput: 0.6e-6, expectOutput: 2.2e-6},
		{model: "GLM-5.2", expectInput: 1.4e-6, expectOutput: 4.4e-6}, // 入口 ToLower
		{model: "kimi-k2", expectInput: 0.56e-6, expectOutput: 2.24e-6},
		{model: "kimi-k3", expectInput: 3e-6, expectOutput: 15e-6},
		{model: "minimax-m3", expectInput: 0.60e-6, expectOutput: 2.40e-6},
		{model: "deepseek-v4-pro", expectInput: 4.35e-7, expectOutput: 8.7e-7},
		{model: "doubao-embedding-vision", expectInput: 0.098e-6, expectOutput: 0},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(tc.model)
			require.NoError(t, err, "%s 应有可用定价，不能再返回 ErrModelPricingUnavailable", tc.model)
			require.NotNil(t, pricing)
			require.InDelta(t, tc.expectInput, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tc.expectOutput, pricing.OutputPricePerToken, 1e-12)
			// 国产模型不应被套上 OpenAI 长上下文策略
			require.Zero(t, pricing.LongContextInputThreshold,
				"%s 不应被 applyModelSpecificPricingPolicy 加上长上下文阈值", tc.model)
		})
	}
}

// TestGetFallbackPricing_ChineseProviders_Whitelist 保证白名单语义没有被放宽：
// 未收录的国产模型仍然不返回兜底价，避免误计价。
func TestGetFallbackPricing_ChineseProviders_Whitelist(t *testing.T) {
	svc := newTestBillingService()

	unknown := []string{
		"qwen-max",
		"qwen3-coder",
		"hunyuan-turbos",
		"doubao-pro-32k",
		"doubao-embedding", // 纯文本 embedding，官方另有价目，未收录
		"moonshot-v1-8k",   // Moonshot V1 多 tier，未收录
		"kimi-k30",         // 不存在的型号，不能被 k3 规则误命中
		"kimi-k3-turbo",    // 非官方 alias，不精确匹配则不兜底
		"minimax-text-01",  // 非 M 系列
		"deepseek-v3",      // V3 未收录
		"glm-3-turbo",      // 老版本未收录
	}

	for _, model := range unknown {
		t.Run(model, func(t *testing.T) {
			require.Nil(t, svc.getFallbackPricing(model),
				"model %s 不在兜底白名单内，必须返回 nil 而不是被子串误命中", model)
		})
	}
}

// TestGetModelPricing_FallbackWarnDedup 验证兜底定价告警按模型去重：
// 同一模型每进程只打一条，不同模型各打一条（不失明）。
func TestGetModelPricing_FallbackWarnDedup(t *testing.T) {
	svc := newTestBillingService()

	out := captureBillingLog(t, func() {
		for i := 0; i < 50; i++ {
			pricing, err := svc.GetModelPricing("glm-4.6")
			require.NoError(t, err)
			require.NotNil(t, pricing)
		}
	})

	require.Equal(t, 1, countFallbackWarnLines(out, "glm-4.6"),
		"同一模型的兜底告警应只打一条，实际日志：\n%s", out)

	// 大小写变体在 GetModelPricing 入口已被 ToLower，视为同一条目，不应重复告警。
	out = captureBillingLog(t, func() {
		_, err := svc.GetModelPricing("GLM-4.6")
		require.NoError(t, err)
	})
	require.Equal(t, 0, countFallbackWarnLines(out, "glm-4.6"),
		"大小写变体不应再次告警，实际日志：\n%s", out)

	// 另一个模型仍然要告警一次——降噪不能变成失明。
	out = captureBillingLog(t, func() {
		for i := 0; i < 10; i++ {
			_, err := svc.GetModelPricing("kimi-k2")
			require.NoError(t, err)
		}
	})
	require.Equal(t, 1, countFallbackWarnLines(out, "kimi-k2"),
		"新模型仍应告警恰好一次，实际日志：\n%s", out)
}

// TestGetModelPricing_FallbackWarnDedup_Concurrent 验证去重在并发下不会重复告警，
// 同时确保 -race 下无数据竞争。
func TestGetModelPricing_FallbackWarnDedup_Concurrent(t *testing.T) {
	svc := newTestBillingService()

	out := captureBillingLog(t, func() {
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					_, _ = svc.GetModelPricing("minimax-m2.7")
				}
			}()
		}
		wg.Wait()
	})

	require.Equal(t, 1, countFallbackWarnLines(out, "minimax-m2.7"),
		"并发下同一模型仍应只告警一条，实际日志：\n%s", out)
}

// TestFallbackWarnDedup_BoundedMemory 验证去重表有上界，不会被任意模型名撑爆。
// getFallbackPricing 对 Claude 族是宽匹配（任何含 "claude" 的名字都能命中），
// 模型名来自请求体，所以无界 sync.Map 是可被外部触发的内存增长点。
func TestFallbackWarnDedup_BoundedMemory(t *testing.T) {
	svc := newTestBillingService()

	total := fallbackWarnSeenMaxEntries + 500
	out := captureBillingLog(t, func() {
		for i := 0; i < total; i++ {
			_, err := svc.GetModelPricing(fmt.Sprintf("claude-attacker-%d", i))
			require.NoError(t, err)
		}
	})

	stored := svc.fallbackWarnSeenCount.Load()
	require.LessOrEqual(t, stored, int64(fallbackWarnSeenMaxEntries),
		"去重表条目数不得超过上界，否则存在内存增长风险")
	require.Equal(t, int64(fallbackWarnSeenMaxEntries), stored,
		"上界之前的模型都应被收录")

	// 达到上界后必须留下且只留下一条提示，说明后续首次告警被抑制。
	capNotices := strings.Count(out, "fallback pricing warn dedup table reached")
	require.Equal(t, 1, capNotices,
		"去重表满时应恰好提示一次，实际提示 %d 次", capNotices)

	// 超出上界的模型不再单独告警，日志总量因此被限制住。
	require.Equal(t, 0, countFallbackWarnLines(out, fmt.Sprintf("claude-attacker-%d", total-1)),
		"超出上界后不应继续为每个新模型打告警")
}
