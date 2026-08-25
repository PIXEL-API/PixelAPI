package service

import (
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func accountModelWhitelistKeys(account *Account) []string {
	if account == nil {
		return nil
	}
	mapping := account.GetModelMapping()
	models := make([]string, 0, len(mapping))
	for model := range mapping {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

// AvailableTestModels 返回账号「测试连接」流程可选的模型列表，管理员端与用户端共用。
//
// 返回值是各平台各自的模型切片类型（openai.Model / geminicli.Model / xai.Model /
// claude.Model / antigravity.ClaudeModel），直接 JSON 序列化给前端即可——前端只依赖
// id 与 display_name 两个字段。平台不受支持时返回 ok=false，调用方应回 400。
func AvailableTestModels(account *Account) (models any, ok bool) {
	// OpenAI：自动透传会绕过常规模型改写，测试/模型列表也回落到默认模型集。
	if account.IsOpenAI() {
		if account.OwnerUserID != nil {
			models := accountModelWhitelistKeys(account)
			out := make([]openai.Model, 0, len(models))
			for _, requestedModel := range models {
				model := openai.Model{ID: requestedModel, Object: "model", Type: "model", DisplayName: requestedModel}
				for _, defaultModel := range openai.DefaultModels {
					if defaultModel.ID == requestedModel {
						model = defaultModel
						break
					}
				}
				out = append(out, model)
			}
			return out, true
		}
		if account.IsOpenAIPassthroughEnabled() {
			return openai.DefaultModels, true
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return openai.DefaultModels, true
		}

		out := make([]openai.Model, 0, len(mapping))
		for requestedModel := range mapping {
			var found bool
			for _, dm := range openai.DefaultModels {
				if dm.ID == requestedModel {
					out = append(out, dm)
					found = true
					break
				}
			}
			if !found {
				out = append(out, openai.Model{
					ID:          requestedModel,
					Object:      "model",
					Type:        "model",
					DisplayName: requestedModel,
				})
			}
		}
		return out, true
	}

	// Gemini
	if account.IsGemini() {
		// 个人账号始终使用号主严格白名单；平台管理的 Google One 账号
		// 使用运行时解析后的显式 mapping 或保守默认目录。
		if account.OwnerUserID != nil || account.IsGeminiGoogleOne() {
			models := accountModelWhitelistKeys(account)
			out := make([]geminicli.Model, 0, len(models))
			for _, requestedModel := range models {
				model := geminicli.Model{ID: requestedModel, Type: "model", DisplayName: requestedModel}
				for _, defaultModel := range geminicli.DefaultModels {
					if defaultModel.ID == requestedModel {
						model = defaultModel
						break
					}
				}
				out = append(out, model)
			}
			return out, true
		}
		// OAuth 账号直接给默认模型集。
		if account.IsOAuth() {
			return geminicli.DefaultModels, true
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return geminicli.DefaultModels, true
		}

		out := make([]geminicli.Model, 0, len(mapping))
		for requestedModel := range mapping {
			var found bool
			for _, dm := range geminicli.DefaultModels {
				if dm.ID == requestedModel {
					out = append(out, dm)
					found = true
					break
				}
			}
			if !found {
				out = append(out, geminicli.Model{
					ID:          requestedModel,
					Type:        "model",
					DisplayName: requestedModel,
					CreatedAt:   "",
				})
			}
		}
		return out, true
	}

	// Antigravity：复用 antigravity.DefaultModels()，与 /v1/models 端点保持同步。
	if account.Platform == PlatformAntigravity {
		if account.OwnerUserID != nil {
			models := accountModelWhitelistKeys(account)
			defaults := antigravity.DefaultModels()
			out := make([]antigravity.ClaudeModel, 0, len(models))
			for _, requestedModel := range models {
				model := antigravity.ClaudeModel{ID: requestedModel, Type: "model", DisplayName: requestedModel}
				for _, defaultModel := range defaults {
					if defaultModel.ID == requestedModel {
						model = defaultModel
						break
					}
				}
				out = append(out, model)
			}
			return out, true
		}
		return antigravity.DefaultModels(), true
	}

	// Grok/xAI
	if account.Platform == PlatformGrok {
		if account.OwnerUserID != nil {
			models := accountModelWhitelistKeys(account)
			defaults := xai.DefaultModels()
			out := make([]xai.Model, 0, len(models))
			for _, requestedModel := range models {
				model := xai.Model{ID: requestedModel, Object: "model", OwnedBy: "xai", DisplayName: requestedModel}
				for _, defaultModel := range defaults {
					if defaultModel.ID == requestedModel {
						model = defaultModel
						break
					}
				}
				out = append(out, model)
			}
			return out, true
		}
		rawMapping, _ := account.Credentials["model_mapping"].(map[string]any)
		if len(rawMapping) == 0 {
			return xai.DefaultModels(), true
		}

		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return xai.DefaultModels(), true
		}

		defaultModels := xai.DefaultModels()
		out := make([]xai.Model, 0, len(mapping))
		for requestedModel := range mapping {
			var found bool
			for _, dm := range defaultModels {
				if dm.ID == requestedModel {
					out = append(out, dm)
					found = true
					break
				}
			}
			if !found {
				out = append(out, xai.Model{
					ID:          requestedModel,
					Object:      "model",
					OwnedBy:     "xai",
					DisplayName: requestedModel,
				})
			}
		}
		return out, true
	}

	// Opencode
	if account.Platform == PlatformOpencode {
		mapping := account.GetModelMapping()
		out := make([]openai.Model, 0, len(mapping))
		for requestedModel := range mapping {
			out = append(out, openai.Model{
				ID:          requestedModel,
				Object:      "model",
				Type:        "model",
				DisplayName: requestedModel,
			})
		}
		return out, true
	}

	if !account.IsAnthropic() {
		return nil, false
	}

	// Claude/Anthropic
	// OAuth / Setup-Token 账号给默认模型集。
	if account.OwnerUserID != nil {
		models := accountModelWhitelistKeys(account)
		out := make([]claude.Model, 0, len(models))
		for _, requestedModel := range models {
			model := claude.Model{ID: requestedModel, Type: "model", DisplayName: requestedModel}
			for _, defaultModel := range claude.DefaultModels {
				if defaultModel.ID == requestedModel {
					model = defaultModel
					break
				}
			}
			out = append(out, model)
		}
		return out, true
	}
	if account.IsOAuth() {
		return claude.DefaultModels, true
	}

	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return claude.DefaultModels, true
	}

	out := make([]claude.Model, 0, len(mapping))
	for requestedModel := range mapping {
		var found bool
		for _, dm := range claude.DefaultModels {
			if dm.ID == requestedModel {
				out = append(out, dm)
				found = true
				break
			}
		}
		if !found {
			out = append(out, claude.Model{
				ID:          requestedModel,
				Type:        "model",
				DisplayName: requestedModel,
				CreatedAt:   "",
			})
		}
	}
	return out, true
}
