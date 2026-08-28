package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 账号「测试连接」模型 resolver 错误。
var (
	// ErrAccountTestUnsupportedPlatform 平台不支持连接测试。
	ErrAccountTestUnsupportedPlatform = infraerrors.BadRequest("ACCOUNT_TEST_UNSUPPORTED_PLATFORM", "account platform does not support connection test")
	// ErrAccountTestModelCatalogEmpty 平台/分组没有任何已定价模型。
	ErrAccountTestModelCatalogEmpty = infraerrors.BadRequest("ACCOUNT_TEST_MODEL_CATALOG_EMPTY", "no priced models are configured for this account platform")
	// ErrAccountTestModelWhitelistMissing 个人账号未配置模型白名单。
	ErrAccountTestModelWhitelistMissing = infraerrors.BadRequest("ACCOUNT_TEST_MODEL_WHITELIST_MISSING", "personal account model whitelist is not configured")
	// ErrAccountTestModelNoPricedIntersection 账号白名单与定价目录无交集。
	ErrAccountTestModelNoPricedIntersection = infraerrors.BadRequest("ACCOUNT_TEST_MODEL_NO_PRICED_INTERSECTION", "none of the account models are priced")
	// ErrAccountTestProtocolNoModels 过滤图片/视频后没有可做文本连接测试的模型。
	ErrAccountTestProtocolNoModels = infraerrors.BadRequest("ACCOUNT_TEST_PROTOCOL_NO_SUPPORTED_MODELS", "no text models are available for the connection test")
	// ErrAccountTestBatchEmpty 批量测试未提供任何账号。
	ErrAccountTestBatchEmpty = infraerrors.BadRequest("ACCOUNT_TEST_BATCH_EMPTY", "no accounts provided for batch test")
	// ErrAccountTestModelNotAvailable 模型不在账号可测试集合中（计划测试/runner 校验用）。
	ErrAccountTestModelNotAvailable = infraerrors.BadRequest("ACCOUNT_TEST_MODEL_NOT_AVAILABLE", "model is not available in the account's testable models")
)

// AccountTestModelResolver 账号「测试连接」模型 resolver。
//
// 职责：把「渠道定价目录」「账号白名单/能力」「测试协议过滤」拆开，
// 统一生成可测试模型列表，并对非 ready 状态返回明确业务错误。
// 只依赖窄接口 PricedModelCatalog，不依赖整个 AccountService。
type AccountTestModelResolver struct {
	catalog PricedModelCatalog
}

// NewAccountTestModelResolver 创建 resolver。
func NewAccountTestModelResolver(catalog PricedModelCatalog) *AccountTestModelResolver {
	return &AccountTestModelResolver{catalog: catalog}
}

// ResolveTestModels 解析账号可做「测试连接」的模型列表。
//
// 返回统一结构的 []claude.Model（id/type/display_name/created_at，与前端
// ClaudeModel 契约一致）。非 ready 状态返回业务错误而非空数组。
func (r *AccountTestModelResolver) ResolveTestModels(ctx context.Context, account *Account) ([]claude.Model, error) {
	if r == nil || r.catalog == nil {
		return nil, ErrOwnedAccountModelCatalogUnavailable
	}
	if account == nil {
		return nil, infraerrors.BadRequest("ACCOUNT_TEST_ACCOUNT_REQUIRED", "account is required")
	}
	if !isAccountTestablePlatform(account.Platform) {
		return nil, ErrAccountTestUnsupportedPlatform
	}

	testable, err := r.resolveTestableModelIDs(ctx, account)
	if err != nil {
		return nil, err
	}
	return buildTestModels(account.Platform, testable)
}

// resolveTestableModelIDs 计算账号可测试的具体模型 ID（目录 ∩ 能力，未过滤图片、未装饰）。
func (r *AccountTestModelResolver) resolveTestableModelIDs(ctx context.Context, account *Account) ([]string, error) {
	catalogModels, err := r.resolveCatalogModelIDs(ctx, account)
	if err != nil {
		if infraerrors.IsBadRequest(err) {
			return nil, err
		}
		return nil, ErrOwnedAccountModelCatalogUnavailable.WithCause(err)
	}
	return r.resolveTestableModels(account, catalogModels)
}

// buildTestModels 过滤图片/视频并装饰元数据，输出统一的 []claude.Model。
func buildTestModels(platform string, modelIDs []string) ([]claude.Model, error) {
	textModels := filterTextTestModels(modelIDs)
	if len(textModels) == 0 {
		return nil, ErrAccountTestProtocolNoModels
	}

	out := make([]claude.Model, 0, len(textModels))
	for _, modelID := range textModels {
		out = append(out, decorateTestModel(platform, modelID))
	}
	return out, nil
}

// ResolveBatchTestModels 计算多个目标账号共同可测试的模型列表。
//
// 任一账号非 ready（白名单缺失、无定价交集等）都会让整个批量解析失败并返回
// 对应业务错误，不静默丢弃该账号；取交集结果不因账号顺序变化而改变。
func (r *AccountTestModelResolver) ResolveBatchTestModels(ctx context.Context, accounts []*Account) ([]claude.Model, error) {
	if r == nil || r.catalog == nil {
		return nil, ErrOwnedAccountModelCatalogUnavailable
	}
	if len(accounts) == 0 {
		return nil, ErrAccountTestBatchEmpty
	}
	if accounts[0] == nil {
		return nil, infraerrors.BadRequest("ACCOUNT_TEST_ACCOUNT_REQUIRED", "account is required")
	}
	if !isAccountTestablePlatform(accounts[0].Platform) {
		return nil, ErrAccountTestUnsupportedPlatform
	}

	platform := accounts[0].Platform
	for _, account := range accounts {
		if account == nil || account.Platform != platform {
			return nil, ErrAccountTestUnsupportedPlatform
		}
	}

	perAccount := make([][]string, 0, len(accounts))
	for _, account := range accounts {
		testable, err := r.resolveTestableModelIDs(ctx, account)
		if err != nil {
			return nil, err
		}
		perAccount = append(perAccount, testable)
	}

	intersection := intersectModelIDLists(perAccount)
	if len(intersection) == 0 {
		return nil, ErrAccountTestModelNoPricedIntersection
	}
	return buildTestModels(platform, intersection)
}

// intersectModelIDLists 取多个模型 ID 列表的交集，返回排序后的结果。
func intersectModelIDLists(lists [][]string) []string {
	if len(lists) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, list := range lists {
		seen := make(map[string]struct{}, len(list))
		for _, model := range list {
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			counts[model]++
		}
	}
	result := make([]string, 0)
	for model, count := range counts {
		if count == len(lists) {
			result = append(result, model)
		}
	}
	sort.Strings(result)
	return result
}

// resolveCatalogModelIDs 确定账号可用的定价目录（多 group 取并集，无 group 用平台全局）。
func (r *AccountTestModelResolver) resolveCatalogModelIDs(ctx context.Context, account *Account) ([]string, error) {
	platform := account.Platform
	if len(account.GroupIDs) == 0 {
		return r.catalog.ListSelectablePricedModelIDs(ctx, PricedModelQuery{Platform: platform})
	}

	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, gid := range account.GroupIDs {
		g := gid
		models, err := r.catalog.ListSelectablePricedModelIDs(ctx, PricedModelQuery{Platform: platform, GroupID: &g})
		if err != nil {
			return nil, err
		}
		for _, model := range models {
			if _, ok := seen[model]; !ok {
				seen[model] = struct{}{}
				result = append(result, model)
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

// resolveTestableModels 计算账号能力与定价目录的交集，输出具体模型 ID。
func (r *AccountTestModelResolver) resolveTestableModels(account *Account, catalogModels []string) ([]string, error) {
	// 个人账号：号主严格白名单（exact identity），空则缺失，不接受通配符扩大权限。
	if account.OwnerUserID != nil {
		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			return nil, ErrAccountTestModelWhitelistMissing
		}
		if len(catalogModels) == 0 {
			return nil, ErrAccountTestModelCatalogEmpty
		}
		intersection := intersectCatalogAndMapping(catalogModels, mapping)
		if len(intersection) == 0 {
			return nil, ErrAccountTestModelNoPricedIntersection
		}
		return intersection, nil
	}

	// 平台账号：目录为空即无候选。
	if len(catalogModels) == 0 {
		return nil, ErrAccountTestModelCatalogEmpty
	}

	// 平台账号无显式 mapping 时，目录本身就是候选能力；
	// 有显式 mapping 时按能力过滤（支持通配符）。
	if !accountHasExplicitModelMapping(account) {
		return catalogModels, nil
	}

	intersection := make([]string, 0, len(catalogModels))
	for _, model := range catalogModels {
		if account.IsModelSupported(model) {
			intersection = append(intersection, model)
		}
	}
	if len(intersection) == 0 {
		return nil, ErrAccountTestModelNoPricedIntersection
	}
	return intersection, nil
}

// intersectCatalogAndMapping 目录 ID 与白名单键做精确集合交集。
func intersectCatalogAndMapping(catalogModels []string, mapping map[string]string) []string {
	catalogSet := make(map[string]struct{}, len(catalogModels))
	for _, model := range catalogModels {
		catalogSet[model] = struct{}{}
	}
	result := make([]string, 0)
	for model := range mapping {
		if _, ok := catalogSet[model]; ok {
			result = append(result, model)
		}
	}
	sort.Strings(result)
	return result
}

// accountHasExplicitModelMapping 判断账号是否配置了显式 model_mapping。
// 不能把 GetModelMapping() 自动注入的平台默认映射误认为管理员显式白名单。
func accountHasExplicitModelMapping(account *Account) bool {
	if account == nil || account.Credentials == nil {
		return false
	}
	rawMapping, _ := account.Credentials["model_mapping"].(map[string]any)
	return len(rawMapping) > 0
}

// isAccountTestablePlatform 判断平台是否支持「测试连接」流程。
func isAccountTestablePlatform(platform string) bool {
	switch platform {
	case PlatformOpenAI, PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformOpencode, PlatformAnthropic:
		return true
	default:
		return false
	}
}

// filterTextTestModels 排除图片/视频生成模型，只保留文本连接测试可用模型。
func filterTextTestModels(models []string) []string {
	result := make([]string, 0, len(models))
	for _, model := range models {
		if !isImageGenerationModelID(model) {
			result = append(result, model)
		}
	}
	return result
}

// isImageGenerationModelID 判断模型是否为图片/视频生成模型（与前端口径一致）。
func isImageGenerationModelID(modelID string) bool {
	normalized := strings.ToLower(modelID)
	return strings.HasPrefix(normalized, "gpt-image-") ||
		(strings.HasPrefix(normalized, "gemini-") && strings.Contains(normalized, "-image")) ||
		(strings.HasPrefix(normalized, "grok-") && (strings.Contains(normalized, "-image") || strings.Contains(normalized, "-video"))) ||
		normalized == "grok-imagine" ||
		strings.HasPrefix(normalized, "cogview")
}

// decorateTestModel 为已通过目录求交的模型 ID 补全展示元数据。
// 平台默认数组只用于装饰，不引入目录之外的模型；未知但已定价的 ID 生成通用展示名。
func decorateTestModel(platform, modelID string) claude.Model {
	switch platform {
	case PlatformOpenAI:
		for _, m := range openai.DefaultModels {
			if m.ID == modelID {
				return claude.Model{ID: m.ID, Type: m.Type, DisplayName: m.DisplayName, CreatedAt: ""}
			}
		}
	case PlatformGrok:
		for _, m := range xai.DefaultModels() {
			if m.ID == modelID {
				return claude.Model{ID: m.ID, Type: "model", DisplayName: m.DisplayName, CreatedAt: ""}
			}
		}
	case PlatformAnthropic:
		for _, m := range claude.DefaultModels {
			if m.ID == modelID {
				return m
			}
		}
	case PlatformGemini:
		for _, m := range geminicli.DefaultModels {
			if m.ID == modelID {
				return claude.Model{ID: m.ID, Type: m.Type, DisplayName: m.DisplayName, CreatedAt: m.CreatedAt}
			}
		}
	case PlatformAntigravity:
		for _, m := range antigravity.DefaultModels() {
			if m.ID == modelID {
				return claude.Model{ID: m.ID, Type: m.Type, DisplayName: m.DisplayName, CreatedAt: m.CreatedAt}
			}
		}
	}
	return claude.Model{ID: modelID, Type: "model", DisplayName: modelID, CreatedAt: ""}
}
