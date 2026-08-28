# 渠道定价模型目录统一实施方案

**生成日期**：2026-08-28  
**适用项目**：`sub2api-0.1.119`  
**实施类型**：跨后端服务、API、管理端/用户端前端、测试、发布的高复杂度改造  
**当前状态**：仅完成方案设计；本方案生成时未修改业务代码、未读取或修改数据库、未连接生产环境

## 1. 目标与已确认决策

### 1.1 总目标

将“渠道管理中已经配置定价的模型”确立为所有业务模型列表的唯一基础白名单，确保任何面向业务的模型候选集都不能超出已配置价格的模型集合。

目标约束为：

```text
任何业务可见/可选/可调度模型
  ⊆ 对应平台、对应分组范围内的启用渠道定价模型并集
```

在这个硬上限之内，再根据场景叠加账号白名单、账号实际能力、房间能力、分组可调度能力和测试协议能力等约束。

### 1.2 本次已确认的策略

以下策略已经由用户确认，实施时不得自行改成其他语义：

1. 渠道定价模型并集是唯一业务基础目录。
2. 有明确 `group` 上下文时，按该分组可访问的渠道过滤；没有分组上下文时，使用该平台所有启用渠道的定价并集。
3. 只统计启用渠道；禁用渠道的模型不再允许新增选择，但不自动删除历史账号 mapping。
4. 渠道主定价 `ModelPricing` 和账号统计定价规则 `AccountStatsPricingRules[].Pricing` 都纳入并集。
5. 账号测试采用“定价目录与账号有效能力求交”，不在个人账号空白名单时静默回退平台默认模型。
6. 个人账号白名单缺失或为空必须显式暴露为配置/数据完整性问题。
7. 批量测试使用所有目标账号的共同可测试模型，不再以第一条代表账号作为唯一依据。
8. 账号房间和网关模型列表也必须受定价目录硬上限约束，但不能把上游实时发现、Usage 历史模型或渠道定价编辑器粗暴替换成定价目录。
9. 历史空白名单账号本轮先做代码修复和只读审计；任何数据库回填必须另行说明影响并获得明确授权。
10. 本次实施计划包含完整发布和回滚步骤；实际生产发布仍需在执行阶段单独确认文档站同步选项。

### 1.3 非目标

- 不在代码阶段自动修改已有账号的 `credentials.model_mapping`。
- 不删除禁用渠道或历史定价记录。
- 不把上游 `/models` 实时发现改造成渠道定价查询。
- 不将 Gemini `/v1beta/models`、Codex manifest、Usage 历史模型筛选替换为业务目录。
- 不在本次改造中修改计费公式、价格数值、余额、用量记录或生产数据。
- 不引入新的外部依赖；优先复用现有 `ChannelService.ListPricedModelIDs`、缓存和响应基础设施。
- 不保留前端死代码、临时脚本、调试输出或重复的硬编码模型全集。

## 2. 当前问题基线

### 2.1 Grok 测试列表为空的现状

当前管理端真实调用链：

```text
frontend/src/views/admin/AccountsView.vue
  → frontend/src/components/account/AccountTestModal.vue
  → GET /api/v1/admin/accounts/:id/models
  → backend/internal/handler/admin/account_handler.go
  → service.AvailableTestModels(account)
  → Grok + OwnerUserID != nil 时只读 model_mapping
```

用户私有 Grok 账号如果 `model_mapping` 缺失或为空，后端返回空数组。前端又把请求异常也置为空，因此当前“无匹配选项”无法区分以下三种状态：

- 成功返回空数组；
- 账号私有白名单为空；
- 接口权限、网络或服务端错误。

### 2.2 已经统一和未统一的入口

| 入口 | 当前来源 | 当前状态 |
|---|---|---|
| 管理端新增账号 | 活跃渠道定价并集 | 已接入 |
| 管理端编辑账号 | 活跃渠道定价并集 | 已接入 |
| 管理端批量编辑 | 活跃渠道定价并集 | 已接入 |
| 用户新增/编辑/批量编辑 | 活跃渠道定价并集 | 已接入 |
| 用户账号导入 | 活跃渠道定价并集 | 已接入 |
| 单账号测试 | 账号 `model_mapping`/平台默认 | 未统一，直接导致本问题 |
| 批量测试 | 第一条代表账号的 `model_mapping` | 未统一 |
| 计划测试 | 复用单账号模型接口 | 未统一 |
| 账号房间创建 | 前后端静态默认数组 | 未统一，已有漂移 |
| 房间编辑 | 房间账号 mapping 交集 | 未与定价目录求交 |
| 标准 `/v1/models` | 分组内运行时账号能力 | 需要增加定价目录求交，不可替换为全局裸并集 |
| Gemini/Codex 上游发现 | 上游实时数据 | 保持独立 |
| Usage 历史模型筛选 | 实际用量记录 | 保持独立 |
| 渠道定价编辑器 | 用户正在编辑的定价数据 | 保持可自由录入 |

### 2.3 当前目录实现的限制

`ChannelService.ListPricedModelIDs` 已经完成了启用渠道、平台过滤、主定价和账号统计定价合并、去空、去重、排序，但目前没有根据具体 `groupID` 收窄，真实语义仍是平台范围内的全局并集。

实施时必须保留现有无分组调用的兼容语义，同时补充分组/渠道范围查询，不能在没有分组上下文时错误地把全局查询当成某个分组查询。

## 3. 目标架构与数据流

### 3.1 分层模型

```text
┌──────────────────────────────────────────────┐
│  Channel Pricing Catalog                      │
│  active channel + ModelPricing + stats rules  │
└──────────────────────┬───────────────────────┘
                       │ 基础硬上限
┌──────────────────────▼───────────────────────┐
│  Context Filter                               │
│  platform + optional groupID/channelID        │
└──────────────────────┬───────────────────────┘
                       │ pricedModels
        ┌──────────────┼───────────────┬──────────────┐
        ▼              ▼               ▼              ▼
  Account Config   Account Test    Room Models   Gateway Models
  pricedModels     priced∩ability  priced∩room   priced∩runtime
```

### 3.2 目录服务接口建议

优先扩展现有 `ChannelService`，不要再新增一套平行的渠道读取逻辑。建议引入内部查询对象，名称可在实现阶段按仓库命名规范微调：

```go
type PricedModelQuery struct {
    Platform  string
    GroupID   *int64
    ChannelID *int64
    AccountIDs []int64
}

type PricedModelCatalog interface {
    ListPricedModelIDs(ctx context.Context, platforms []string) ([]string, error)
    ListSelectablePricedModelIDs(ctx context.Context, query PricedModelQuery) ([]string, error)
    IsModelPriced(ctx context.Context, query PricedModelQuery, modelID string) (bool, error)
}
```

兼容规则：

- 现有 `ListPricedModelIDs(platforms)` 保留，作为“无具体分组上下文”的平台并集包装方法，避免一次性破坏大量测试桩和已有调用者。
- `ListSelectablePricedModelIDs` 负责列举可以安全展示在 UI 中的有限、具体模型 ID；`IsModelPriced` 负责对一个已经由账号/上游能力提供的具体模型 ID执行精确定价或通配符定价匹配。两者必须使用同一缓存快照和相同 scope 规则。
- 新的按范围方法负责 `Platform`、`GroupID`、`ChannelID` 的显式过滤。
- `AccountIDs` 仅用于判断账号统计定价规则是否适用于当前运行时账号集合；调用前必须去重和校验，不能用它绕过账号读取权限。
- `GroupID` 和 `ChannelID` 同时传入时必须验证一致性；不一致直接返回参数错误，不静默选择其中一个。
- 只接受有效、规范化的平台名；平台为空时仅允许现有“所有平台”管理用途，业务 resolver 不得在缺少平台时返回跨平台混合模型。

### 3.3 目录收集规则

对每个候选渠道执行：

1. 渠道状态必须为 `active`。
2. 渠道必须与查询的 `platform` 匹配（大小写不敏感、去首尾空白）。
3. 如果存在 `groupID`，渠道必须关联该分组；没有关联时不得回退到平台全局渠道。
4. 如果存在 `channelID`，只读取该渠道，并再次校验平台和启用状态。
5. 合并 `channel.ModelPricing`。
6. 合并 `channel.AccountStatsPricingRules[].Pricing`。无 group/account 上下文的管理目录保持当前“全部规则并集”兼容语义；存在范围上下文时必须复用 `backend/internal/service/account_stats_pricing.go` 已有的 `matchAccountStatsRule` OR 合同（`accountID ∈ AccountIDs OR groupID ∈ GroupIDs`，两个 scope 都为空则不匹配），不要重新实现一套 AND/OR 规则。需要多个运行时账号时，对任一 `AccountID` 命中即纳入该规则。
7. 模型名去首尾空白、丢弃空字符串、按精确字符串去重。
8. 保持稳定字典序输出，避免前端顺序抖动和测试快照不稳定。
9. 通配符定价必须拆分“列举”和“授权”语义：`claude-*` 之类模式不能直接返回给选择器，也不能靠字符串集合交集判断；目录列举只返回定价中的精确 ID，以及能从现有 `Channel.SupportedModels()`/mapping 安全展开且最终确认有定价的具体 ID。对于账号、房间、gateway 已经提供的具体能力模型，则逐个调用 pattern-aware `IsModelPriced`，复用 `GetChannelModelPricing`/现有 wildcard matcher。无法从通配符推导有限具体集合时保持不可列举，禁止用平台静态默认列表猜测展开。

### 3.4 统一 resolver 规则

目录和账号能力必须分层，不允许把静态平台默认清单继续作为候选来源。

```text
catalogModels = finite selectable priced catalog by context
accountModels = effective concrete account capability（支持显式 mapping、wildcard 和平台无 mapping 语义）

account-config models = catalogModels
owned-test models    = accountModels 中逐个通过 IsModelPriced 的模型
platform-test models = effective capability 中逐个通过 IsModelPriced 的模型
batch-test models    = 每个目标账号可测试模型的交集
room models          = 房间账号能力交集中过定价 matcher 的模型
gateway models       = 可调度账号能力并集中通过 group 定价 matcher 的模型
```

账号能力求交的实现细节：

- 对已有明确 mapping 的账号，不能只做“目录 ID 与 mapping 键的精确集合交集”，因为平台账号可能存在通配符 mapping；应遍历目录中的客户端请求模型 ID，使用现有 `account.IsModelSupported(modelID)`/等价纯函数判断能力。
- 反过来，当账号、房间或 gateway 已经有具体能力 ID 时，也不能与含 wildcard pattern 的原始定价键做字符串交集；应对每个具体 ID 调用 `IsModelPriced`。
- 对用户私有账号，继续遵守现有 exact identity whitelist 校验，不接受通配符扩大权限。
- 对平台账号，必须读取原始显式 mapping 状态：存在显式 mapping 时按能力过滤；确实没有显式 mapping 时才把目录视为候选能力。不能把 `GetModelMapping()` 自动注入的静态默认列表误认为管理员显式白名单。
- 目录和 mapping 比较使用客户端请求模型名，即 mapping 左侧；mapping 右侧上游目标模型不作为定价目录键。

平台默认模型数组如 `xai.DefaultModels()`、`openai.DefaultModels`、`claude.DefaultModels` 仅允许用于给“已经通过目录求交的 ID”补充 `display_name`、`type` 等响应元数据；它们不得引入目录之外的模型 ID。

### 3.5 账号状态和错误语义

resolver 必须区分：

- `ready`：目录非空，账号能力与目录存在交集；
- `catalog_empty`：当前平台/分组没有任何已定价模型；
- `account_whitelist_missing`：用户私有账号没有配置 `model_mapping`；
- `no_priced_intersection`：账号有白名单，但白名单中没有已定价模型；
- `unsupported_platform`：平台不支持该业务测试；
- `scope_mismatch`：账号平台、分组或渠道上下文不一致；
- `load_failed`：目录或账号读取失败。

本方案确定采用以下 API 兼容策略，实施时不再临场二选一：

- 保留现有单账号 `/models` 成功响应 `ClaudeModel[]`，避免无必要地破坏管理员端、用户端和潜在内部调用者。
- 正常成功响应必须至少含一个真正可测试模型；目录为空、私有白名单缺失、无已定价交集、测试协议无可用模型均使用项目标准错误响应返回明确业务错误码，不再返回 `200 + []`。
- 优先复用已有 `OWNED_ACCOUNT_MODEL_CATALOG_UNAVAILABLE`、`OWNED_ACCOUNT_MODEL_MAPPING_INVALID`、`OWNED_ACCOUNT_MODEL_NOT_SELECTABLE` 的语义；确实不能准确表达时再新增 `ACCOUNT_TEST_MODEL_CATALOG_EMPTY`、`ACCOUNT_TEST_MODEL_WHITELIST_MISSING`、`ACCOUNT_TEST_MODEL_NO_PRICED_INTERSECTION`、`ACCOUNT_TEST_PROTOCOL_NO_SUPPORTED_MODELS`、`ACCOUNT_MODEL_SCOPE_MISMATCH`。禁止用英文 message 字符串充当机器判断条件。
- 白名单缺失、无交集和目录为空属于当前账号/配置与操作冲突，使用项目 `infraerrors` 的 4xx 错误；Repository、缓存或服务依赖读取失败保持 5xx；平台参数或 scope 不合法为 400。
- 新增批量模型查询端点同样以 `ClaudeModel[]` 作为成功结果，以相同业务错误码表达非 ready 状态。
- 前端 API 客户端读取标准响应中的业务错误码并映射 UI；不得为了兼容旧后端而把任何错误 normalize 成空数组。由于本项目发布的是后端嵌入式前端，前后端必须作为同一个 release 切换。
- 错误响应不得包含凭证内容、完整 `model_mapping`、上游 token 或数据库细节。

## 4. 实施前置条件与工作区纪律

1. 先保存当前工作区状态快照：当前仓库已有大量 `ideas-plaza` 未提交修改，任何实施任务不得覆盖、回滚或格式化这些文件。
2. 本功能只复用现有渠道定价、渠道与分组关联及 `accounts.credentials.model_mapping`，不预期新增表、列、索引或 SQL migration；如果实现过程中发现必须持久化新状态，立即停止该分支，说明原因、影响、迁移文件和回滚方案，重新取得授权。
3. 代码、测试和临时调试脚本按仓库约束落位：临时测试仅放 `test/`，完成后清理；本方案文档放 `docs/`。
4. 所有文件读写使用 UTF-8；中文 i18n 和错误文案必须检查编码，不能产生 BOM 或乱码。
5. 不修改生产数据库、不执行回填、不执行删除历史 mapping。
6. 不修改 systemd、nginx、Redis 或数据库配置作为本功能的隐含副作用。
7. 所有任务完成后检查 `git diff`、未跟踪文件、测试产物和构建产物，确保没有临时文件进入提交。

## 5. 分阶段实施计划

每个 Sprint 都应形成可编译、可测试、可回滚的增量。除非某个任务明确声明依赖，标记为“可并行”的任务可以由独立代理执行；同一文件只能有一个写入者。

---

## Sprint 0：基线冻结、契约确认与保护性审计

**目标**：在改代码前锁定现状、接口消费者和回滚边界，避免把已有未提交功能混入本次改造。

### Task 0.1：记录工作区和构建基线

- **位置**：仓库根目录、`backend/cmd/server/VERSION`、`backend/migrations/`、`frontend/package.json`。
- **操作**：记录 `git rev-parse HEAD`、`git status --short`、版本号、最高 migration 编号、Node/pnpm/Go 版本；不修改任何文件。
- **依赖**：无。
- **当前已知基线**：`backend/cmd/server/VERSION` 为 `1.2.52`；本地目录当前最高迁移文件为未提交的 `279_ideas_plaza.sql`，且它属于用户正在开发的其他功能，不属于本模型目录改造。
- **验收**：基线记录可用于后续 diff；已确认 `ideas-plaza` 相关修改不会被覆盖或被本功能提交；发布构建不得在未确认时把 `279_ideas_plaza.sql` 作为无关迁移带入生产。
- **验证**：`git status --short` 前后内容一致；不得产生构建产物。

### Task 0.2：冻结模型来源清单

- **位置**：只读审计 `backend/internal/service/`、`backend/internal/handler/`、`frontend/src/api/`、`frontend/src/components/`、`frontend/src/views/`。
- **操作**：建立调用者表，标出“必须使用定价目录”“只能与定价目录求交”“必须保持上游/Usage 独立”的边界。
- **依赖**：Task 0.1。
- **验收**：单账号、批量、计划、房间、网关、新增/编辑/导入均有明确消费者和目标 resolver。
- **验证**：`rg` 搜索 `AvailableTestModels`、`ListPricedModelIDs`、`GetModelMapping`、`GetAvailableModels`、`supported_models`，结果无未分类入口。

### Task 0.3：落实 API 和错误状态契约

- **位置**：`backend/internal/handler/admin/account_handler.go`、`backend/internal/handler/user_account_handler.go`、`frontend/src/api/admin/accounts.ts`、`frontend/src/api/accounts.ts`。
- **操作**：按 3.5 已确定的策略保留 `/models` 成功数组，使用标准 typed error 表达空目录、白名单缺失、无交集和测试协议无可用模型；把错误码写入代码注释、前端映射和测试名称。
- **依赖**：Task 0.2。
- **验收**：管理员端和用户端使用同一套语义；不存在一个 Handler 返回 `200 []`、另一个返回 422 的漂移。
- **验证**：先以测试用例草案审查，不写业务代码。

---

## Sprint 1：渠道定价目录服务按范围收口

**目标**：把现有平台并集能力升级为可按平台、分组、渠道查询的唯一目录底座，同时保持旧调用兼容。

### Task 1.1：定义范围查询对象和目录接口

- **位置**：`backend/internal/service/channel_service.go`，必要时抽取到同目录的模型类型文件。
- **操作**：新增 `PricedModelQuery`/等价命名；保留现有 `ListPricedModelIDs`，增加 `ListSelectablePricedModelIDs` 和 pattern-aware `IsModelPriced`；所有列举方法返回排序稳定且不含 wildcard 的模型 ID。
- **依赖**：Sprint 0 完成。
- **验收**：旧调用者无需一次性修改；新调用者可以表达 platform-only、group-scoped、channel-scoped 三种范围。
- **验证**：编译服务包；新增接口的 nil service、空平台、空范围测试。

### Task 1.2：实现 active 渠道和分组过滤

- **位置**：`backend/internal/service/channel_service.go`；复用 `channelCache`、`channelByGroupID`、`groupPlatform` 或现有 Repository 查询。
- **操作**：实现分组到渠道的精确收窄；分组未绑定渠道时返回明确空目录/范围状态，不回退平台全局；渠道与查询平台不一致时返回范围错误。
- **依赖**：Task 1.1。
- **验收**：启用渠道纳入、禁用渠道排除；group A 不会读到 group B 专属渠道模型；channelID 与 groupID 不一致会失败。
- **验证**：构造多个渠道、多个分组、重叠/冲突绑定的单元测试。

### Task 1.3：统一定价来源、清洗和排序

- **位置**：`backend/internal/service/channel_service.go`、`backend/internal/service/channel_service_test.go`。
- **操作**：保留 `ModelPricing` 与 `AccountStatsPricingRules[].Pricing` 合并；范围查询复用/抽取已有 `matchAccountStatsRule`，支持 query 中的 group/account 集合；统一 trim、空值、重复模型、大小写平台处理；为 exact 和 wildcard 建独立索引。列举接口不返回 wildcard；matcher 对 concrete ID 正确应用 wildcard。复用 `Channel.SupportedModels()` 时只采纳最终能确认已定价的项，不能把 mapping-only 项当作已定价。
- **依赖**：Task 1.2。
- **验收**：输出与现有平台并集测试兼容，且所有新增范围测试可复现排序。
- **验证**：补充主定价+统计规则、active/disabled、重复/空值、exact/wildcard 列举与授权差异、mapping 展开边界测试。

### Task 1.4：处理缓存和定价变更失效

- **位置**：`backend/internal/service/channel_service.go` 的缓存加载/失效逻辑及渠道 CRUD。
- **操作**：让目录查询复用现有 `ChannelService.loadCache`/`channelCache`，必要时在快照中预计算 platform/group 模型集合；渠道新增、更新、删除、定价替换、分组绑定变更后必须使目录缓存失效；不要增加无法失效的独立长期缓存。尤其禁止让 Gateway `/v1/models` 热路径调用当前每次执行 `repo.ListAll(ctx)` 的旧实现。
- **依赖**：Task 1.1–1.3。
- **验收**：新增/删除最后一个模型定价后，下一次目录查询立即反映；多实例缓存机制遵循已有 `ClusterCacheCoordinator`，不另建广播协议。
- **验证**：缓存命中、失效、重载和错误短缓存测试；检查不会把 DB 错误永久缓存为合法空目录。

### Task 1.5：扩展目录服务测试桩和现有调用者

- **位置**：`backend/internal/service/*_test.go`、`backend/internal/handler/*_test.go`。
- **操作**：为测试桩增加新接口；现有使用 `pricedModelCatalog` 的测试只在需要范围上下文时升级，避免无意义地改动全部构造器。
- **依赖**：Task 1.1–1.4。
- **验收**：服务、Repository、Handler 快速测试均能编译通过，旧测试语义未被偷偷改写。
- **验证**：`go test -C backend ./internal/service/... ./internal/repository/...` 的相关子集。

**Sprint 1 Demo/验证清单**：

- 同一平台两个 active 渠道的模型输出为并集。
- disabled 渠道模型不进入输出。
- 指定 group 只得到其绑定渠道的模型。
- 删除某模型最后一份定价后，目录立即不再返回它。
- 原有 `ListPricedModelIDs` 平台调用结果保持兼容。

---

## Sprint 2：账号能力与测试模型 resolver

**目标**：把“渠道目录”“账号白名单”“测试协议过滤”拆开，统一生成单账号可测试模型，解决 Grok 私有账号空列表的错误表达。

### Task 2.1：定义账号模型能力判定

- **位置**：新增 `backend/internal/service/account_test_model_resolver.go` 及对应测试，把现有 `account_available_models.go` 中的元数据装饰逻辑迁入或收口。
- **操作**：实现独立的 `AccountTestModelResolver`，只依赖窄接口 `PricedModelCatalog`；定义单账号和批量解析方法，输入账号及可选范围上下文，输出模型 ID、内部状态和诊断原因；不得直接调用平台默认模型作为候选源。文本连接测试所需的图片/视频排除也在该 resolver 完成，前端不再重复维护业务过滤规则。
- **依赖**：Sprint 1 完成。
- **验收**：平台账号 mapping 为空时使用定价目录；私有账号 mapping 缺失/为空时返回白名单缺失状态；有 mapping 时执行目录与 mapping 求交。
- **验证**：OpenAI、Grok、Gemini、Antigravity、Anthropic、OpenCode 各平台至少一组测试。

### Task 2.2：保留元数据装饰，禁止静态 ID 泄漏

- **位置**：`backend/internal/service/account_available_models.go` 及平台模型类型转换代码。
- **操作**：将现有平台默认数组改为“仅对已通过 resolver 的 ID 补全描述”；未知但已定价的 ID 生成通用 `id/display_name`，不能因为不在静态默认数组中而丢弃。
- **依赖**：Task 2.1。
- **验收**：渠道定价新增任意模型后，测试列表可以显示它；静态默认数组中的未定价模型不会被重新带回。
- **验证**：新增模型元数据、未收录模型、图片/视频模型过滤测试。

### Task 2.2a：接入依赖注入且避免服务循环

- **位置**：`backend/internal/service/wire.go`、`backend/cmd/server/wire.go`、`backend/cmd/server/wire_gen.go`、`backend/internal/handler` 构造器。
- **操作**：提供 `AccountTestModelResolver(ChannelService)`；将其注入 `AccountTestService`，由后者向管理员/用户 Handler 暴露列表解析并在真正发起上游测试前做最终授权。不要把整个 `AccountService` 注入 `AccountTestService`，否则会与现有 `AccountShareModeService → AccountTestService`、`AccountService → AccountShareModeService` 依赖形成循环。
- **依赖**：Task 2.1、Task 2.2。
- **验收**：Wire 图无循环；所有调用 `TestAccountConnection`/`RunTestBackground` 的路径在同一个 resolver 处校验；现有测试桩只需实现最窄接口。
- **验证**：Wire 生成/编译测试；检查 `wire_gen.go` 差异只包含本功能所需依赖变更。当前这些文件已有 Ideas Plaza 未提交改动，禁止覆盖式重新生成后不审查 diff。

### Task 2.3：定义错误类型和诊断元数据

- **位置**：`backend/internal/service` 错误定义、`backend/internal/handler/response` 使用点。
- **操作**：新增或复用项目错误类型，至少覆盖 `ACCOUNT_MODEL_WHITELIST_MISSING`、`ACCOUNT_MODEL_CATALOG_EMPTY`、`ACCOUNT_MODEL_NO_PRICED_INTERSECTION`、`ACCOUNT_MODEL_SCOPE_MISMATCH`；错误 metadata 只包含平台、账号 ID、范围和计数，不包含凭证。
- **依赖**：Task 2.1。
- **验收**：Handler 能稳定把 resolver 状态映射为 HTTP/业务错误；前端无需解析英文异常字符串判断状态。
- **验证**：错误码和 metadata 的 Handler 测试；确认未知错误仍走 5xx，不被伪装为空列表。

### Task 2.4：改造管理员和用户单账号模型接口

- **位置**：`backend/internal/handler/admin/account_handler.go`、`backend/internal/handler/user_account_handler.go`、路由文件（若路径不变则不新增路由）。
- **操作**：两个 Handler 调用同一 resolver；保留管理员权限和用户所有权校验；成功响应继续返回相同数组类型，非 ready 状态返回相同 typed error，不能一端返回空数组、一端返回业务错误。
- **依赖**：Task 2.1–2.3。
- **验收**：截图场景中，私有 Grok 空 mapping 明确返回白名单缺失，而不是 `200 []`；正常私有账号只显示“已定价且在白名单中的模型”。
- **验证**：现有 `account_handler_available_models_test.go` 更新并补齐 Grok 私有账号测试。

### Task 2.5：修复管理员写入路径的空白名单生成

- **位置**：`backend/internal/service/admin_service.go` 创建、更新、归属变更相关函数；必要时复用 `AccountService` 的校验 helper。
- **操作**：当管理员把账号设置为 `OwnerUserID != nil` 时，执行与用户个人账号一致的定价目录/白名单完整性校验；平台账号转个人账号不得在没有 mapping 时静默成功。
- **依赖**：Task 1、Task 2.1。
- **验收**：新建、编辑、归属转换三条路径不能再制造“个人账号但 mapping 为空”的新状态；现有合法账号更新不受影响。
- **验证**：管理服务创建/更新单元测试；覆盖 owner 不变、从平台转个人、从个人转平台、显式空 mapping 和未定价 mapping。

### Task 2.6：统一前端请求错误和空态

- **位置**：`frontend/src/components/account/AccountTestModal.vue`、`frontend/src/utils/accountTestModels.ts`、相关 i18n 文件和测试。
- **操作**：去掉 catch 中“置空后当作正常结果”的逻辑；增加加载中、加载失败可重试、目录为空、白名单缺失、无定价交集、测试协议无可用模型等状态；`prepareAccountTestModels` 只保留展示排序和默认选择，不再作为图片/视频业务准入的第二权威来源。
- **依赖**：Task 2.4 的 API 契约确定后；可与 Task 2.5 并行。
- **验收**：Network 4xx/5xx 显示错误和重试；私有空 mapping 显示配置提示；合法空交集显示原因；正常列表可选择。
- **验证**：`AccountTestModal.spec.ts` 增加成功、失败、空目录、白名单缺失、重试、图片视频过滤测试；中英文文案 UTF-8 检查。

**Sprint 2 Demo/验证清单**：

- 管理员账号和用户端同一账号返回相同模型 ID 集合（权限范围不同除外）。
- Grok 私有空 mapping 不再显示“无匹配选项”作为唯一信息。
- 未定价的静态默认模型不会因元数据装饰重新出现。
- 渠道新定价模型可以在测试列表显示。

---

## Sprint 3：批量测试、计划测试和提交校验闭环

**目标**：让即时单测、批量测试、计划测试使用同一 resolver，避免代表账号偏差和“下拉能选但提交 400”。

### Task 3.1：实现批量账号共同模型 resolver

- **位置**：`backend/internal/service` 新增批量解析方法及测试；`backend/internal/handler/admin/account_handler.go` 的批量测试相关函数。
- **操作**：加载全部选中账号，先验证账号存在、平台一致、权限有效，再计算每个账号的可测试模型集合与目录的交集，最终取交集；记录被排除原因，不返回部分账号不能执行的模型。
- **依赖**：Sprint 2。
- **验收**：任何选中账号不支持的模型都不会出现在批量选择器；代表账号顺序变化不影响结果；空集合有明确原因。
- **验证**：三账号两平台、同平台不同 mapping、一个空 mapping、一个只有未定价模型、重复 ID、账号被删除等测试。

### Task 3.2：设计批量模型查询 API

- **位置**：`backend/internal/server/routes/admin.go`、`backend/internal/handler/admin/account_handler.go`、`frontend/src/api/admin/accounts.ts`。
- **操作**：新增 `POST /api/v1/admin/accounts/batch-test/model-options`，请求体为 `{account_ids: number[]}`，成功响应为 `ClaudeModel[]`；使用 POST 是为了避免大量 ID 进入 query string，该操作本身只读、不得创建任务。由后端统一计算交集，不能让前端只请求第一条代表账号。
- **依赖**：Task 3.1。
- **验收**：请求参数有数量上限、去重、平台一致性和权限校验；响应包含模型和诊断状态；不泄露其他管理员不可见账号信息。
- **验证**：Handler HTTP 测试、非法 ID、跨平台、超限、权限和空交集测试。

### Task 3.3：改造管理端批量测试弹窗和页面

- **位置**：`frontend/src/components/admin/account/AccountBatchTestModal.vue`、`frontend/src/views/admin/AccountsView.vue`。
- **操作**：弹窗加载选中账号共同模型；移除 representative-only 依赖；显示选中数量、平台、不可用原因；提交前再次确认 model ID 属于返回集合。
- **依赖**：Task 3.2。
- **验收**：批量测试选择的模型对所有目标账号合法；提交失败不会清空错误上下文；加载失败可重试。
- **验证**：批测组件测试、AccountsView 批量流程测试、浏览器手工验证。

### Task 3.4：统一计划测试的模型来源

- **位置**：`frontend/src/views/admin/AccountsView.vue`、`frontend/src/components/admin/account/ScheduledTestsPanel.vue`、`frontend/src/api/admin/scheduledTests.ts`、`backend/internal/handler/admin/scheduled_test_handler.go`、`backend/internal/service/scheduled_test_service.go`、`backend/internal/service/scheduled_test_runner_service.go`。
- **操作**：计划创建/更新时复用单账号 resolver；给 `ScheduledTestService`/runner 注入窄模型校验接口；服务端再次验证计划模型仍在账号可测试集合中；runner 每次执行前重新校验当前定价和账号能力。定价删除或账号白名单变更后，保存明确失败结果并推进下次计划，不发上游请求、不自动改写计划模型。
- **依赖**：Task 2.4、Task 3.1。
- **验收**：即时测试和计划测试使用相同模型边界；计划不能新建未定价模型；历史计划不会因目录变动被静默删除。
- **验证**：计划 CRUD、模型失效、账号 owner/白名单变化、定价删除后运行结果测试。

### Task 3.5：收紧管理员批测提交服务端校验

- **位置**：`backend/internal/handler/admin/account_handler.go`、`backend/internal/service/account_batch_task.go` 及执行器。
- **操作**：创建批量任务时重新解析目标账号集合，确保请求的 `model_id` 属于共同可测试集合；不能只信任前端下拉值；执行过程中若单账号状态改变，按任务项记录失败，不扩大白名单。
- **依赖**：Task 3.1、Task 3.2。
- **验收**：绕过前端提交未定价/不共同支持模型会被拒绝；任务结果区分参数错误和单账号上游失败。
- **验证**：Handler 和 batch worker 测试，覆盖并发状态变化。

**Sprint 3 Demo/验证清单**：

- 选中 3 个账号时，模型列表是三者共同交集。
- 代表账号换成任意一条，结果不变。
- 计划测试不能选择测试弹窗之外的模型。
- 直接构造 HTTP 请求绕过前端时，服务端仍拒绝非法模型。

---

## Sprint 4：账号新增、编辑、导入和静态模型清单清理

**目标**：清除剩余前后端硬编码业务模型全集，让账号配置场景和测试场景共同依赖目录服务。

### Task 4.1：核对账号 CRUD 的目录上下文

- **位置**：`frontend/src/components/account/CreateAccountModal.vue`、`EditAccountModal.vue`、`BulkEditAccountModal.vue`；`backend/internal/service/account_service.go`。
- **操作**：将已有平台定价查询升级为按 group 上下文可用时精确查询；无 group 时保持平台 active 渠道并集；提交时服务端继续做目录成员校验。
- **依赖**：Sprint 1。
- **验收**：UI 候选与后端写入校验使用同一范围；group 切换不会残留其他 group 的新增选项。
- **验证**：现有 model-options 测试扩展 group 场景；创建/编辑/批量编辑组件测试。

### Task 4.2：统一用户导入和 identity mapping

- **位置**：`frontend/src/components/user/ImportAccountsModal.vue`、`backend/internal/service/account_service.go` 的导入和白名单校验路径。
- **操作**：确认导入自动生成的 mapping 只来自当前目录；导入响应中对目录为空、模型全部未定价给出明确错误；不使用上游发现模型绕过定价。
- **依赖**：Task 4.1。
- **验收**：导入流程不会写入未定价模型；空目录不会生成空白但成功的个人账号。
- **验证**：已有 `ImportAccountsModal.agent-identity.spec.ts` 和后端导入测试扩展。

### Task 4.3：移除前端静态业务模型全集

- **位置**：`frontend/src/composables/useModelWhitelist.ts`、`frontend/src/components/account/ModelWhitelistSelector.vue`、`frontend/src/views/user/AccountShareView.vue`。
- **操作**：静态数组仅可保留为非业务元数据或测试 fixture；账号配置、房间创建、mapping preset 不得在缺少 `allowedOptions` 时回退硬编码全集；缺少目录时显示明确错误，不偷偷恢复默认列表。
- **依赖**：Task 4.1、Task 4.2、Sprint 5 房间 API 设计。
- **验收**：`rg` 不再发现业务路径直接依赖平台硬编码模型数组；组件没有“错误时回退默认模型”分支。
- **验证**：Vitest 组件测试、TypeScript 类型检查、手工验证 OpenAI/Anthropic/Grok 创建页面。

### Task 4.4：删除重复测试弹窗副本

- **位置**：当前使用的 `frontend/src/components/account/AccountTestModal.vue` 与未引用的 `frontend/src/components/admin/account/AccountTestModal.vue` 及其孤立测试。
- **操作**：先用全仓引用搜索确认旧副本没有运行时引用；将仍有价值的正式回归用例迁移到共享组件测试后，删除死副本和仅验证死实现的重复测试文件。正式行为测试必须保留，不能把“清理临时测试”的要求误用为删除永久回归门。不得删除仍被其他页面使用的导出。
- **依赖**：Sprint 2 完成；Task 4.3 可并行但需同一负责人审查。
- **验收**：管理端和用户端只保留共享实现；构建不出现缺失导入。
- **验证**：`rg` 引用检查、Vitest 全相关组件、`pnpm --dir frontend run build`。

**Sprint 4 Demo/验证清单**：

- 渠道新增模型后，账号配置和测试都能显示；无需改前端常量。
- 删除最后一份定价后，新增选择被禁止，历史 mapping 不被自动删除。
- 静态数组不再作为错误 fallback。

---

## Sprint 5：账号房间模型统一

**目标**：房间创建和编辑都使用分组定价目录与房间账号能力交集，修复个人账号身份在 Repository 临时对象中丢失的问题。

### Task 5.1：房间创建改用动态分组目录

- **位置**：`backend/internal/service/account_share_mode.go`、`frontend/src/views/user/AccountShareView.vue`、相关 Handler/路由。
- **操作**：房间创建必须取得明确平台和 group 上下文，调用分组目录；没有定价目录时阻止创建并给出原因；删除前后端静态 OpenAI/Anthropic/OpenCode 默认列表；同时收紧 `frontend/src/api/accountShare.ts` 中过宽的 `platform: 'openai' | 'anthropic' | string`，改为与实际 UI/后端一致的显式平台联合类型（至少包含 `opencode`），避免 `string` 掩盖平台遗漏。
- **依赖**：Sprint 1、Sprint 4。
- **验收**：房间创建选择器只显示当前 group 已定价模型；前后端不再出现 `claude-sonnet-5` 等列表漂移。
- **验证**：房间创建 API、前端选择器、空目录和 group 未绑定测试。

### Task 5.2：房间编辑执行“目录 ∩ 账号交集”

- **位置**：`backend/internal/service/account_share_mode.go` 计算 `supported_models` 的路径、`frontend/src/views/user/AccountShareView.vue` 编辑逻辑。
- **操作**：先求房间内账号能力交集，再与该房间 group/channel 定价目录求交；保留历史未定价模型的诊断信息，但禁止新增和新请求使用。
- **依赖**：Task 5.1。
- **验收**：房间不会开放未定价模型；账号能力变化后房间可配置集合正确收窄；不自动删除数据库中的历史 mapping。
- **验证**：两账号不同 mapping、一个未定价、一个图片模型、group 切换的服务测试。

### Task 5.3：补齐房间 Repository 的 OwnerUserID

- **位置**：`backend/internal/repository/account_share_mode_repo.go` 的 `ListRoomAccountModelInfos` 查询和临时 `Account` 构造。
- **操作**：查询并传递 `owner_user_id`（或仓库当前等价字段）到服务层临时 Account；确认私有账号空 mapping 不会被误判为平台账号并回退默认映射。
- **依赖**：数据库表结构已有 owner 字段的只读代码确认；不新增 migration。
- **验收**：个人账号严格白名单语义在房间和单账号测试中一致；平台账号默认行为不被破坏。
- **验证**：Repository 集成测试或可替代的 SQL mock 测试；私有/平台双分支回归。

### Task 5.4：房间运行时请求增加最终校验

- **位置**：`backend/internal/service/account_share_mode.go`、`account_share_lifecycle.go`、房间 Repository 中涉及 `IsModelSupported` 的路径。
- **操作**：创建/编辑时的目录约束不能只停留在 UI；dispatch 前再次确认请求模型属于房间快照/分组目录，并保留现有账号能力校验。
- **依赖**：Task 5.2、Task 5.3。
- **验收**：绕过 UI 请求未定价模型会被拒绝；定价删除后的已有房间不会新发起未定价请求。
- **验证**：房间请求流程、并发定价变更、缓存失效测试。

**Sprint 5 Demo/验证清单**：

- 新建房间只显示当前 group 的已定价模型。
- 编辑房间模型集合等于分组目录与所有房间账号能力交集。
- 私有账号空 mapping 不再因 Repository 临时对象缺少 owner 而回退默认模型。

---

## Sprint 6：网关模型列表和运行时能力边界

**目标**：让标准 `/v1/models` 遵守定价硬上限，同时保留“当前分组真实可调度能力”的运行时语义。

### Task 6.1：审查 GatewayService 现有可用模型计算

- **位置**：`backend/internal/service/gateway_service.go` 的 `GetAvailableModels`、`gateway_model_availability.go`、相关 OpenAI-compatible Handler。
- **操作**：确认 group 内账号能力并集的现有计算、缓存和空结果语义；定义 `runtimeSupportedModels ∩ groupPricedModels` 的最小改造点。
- **依赖**：Sprint 1 完成。
- **验收**：不会把全局平台定价模型直接暴露给没有可调度账号的 group。
- **验证**：现有 gateway hotpath/cache 测试作为基线。

### Task 6.2：实现运行时能力与定价目录求交

- **位置**：`backend/internal/service/gateway_service.go`、必要时 `gateway_model_availability.go`。
- **操作**：保留当前 group 过滤、schedulable、mapping 和 transient 状态语义；在最终输出阶段与 group/channel priced catalog 求交；没有 group 的兼容路径不得误用跨平台目录。
- **依赖**：Task 6.1。
- **验收**：未定价但账号支持的模型不出现在 `/v1/models`；已定价但无账号支持的模型也不出现在 `/v1/models`。
- **验证**：跨 group、定价删除、账号禁用、mapping 别名、缓存失效和空池测试。

### Task 6.3：保持上游发现端点独立

- **位置**：`backend/internal/handler/gemini_v1beta_handler.go`、`openai_codex_models_handler.go`、`upstream_models.go`。
- **操作**：只在业务可选择/可调度出口应用目录硬上限；上游实时发现仍可返回完整上游结果，必要时增加“已定价/未定价”标记，但不能改变协议发现语义。
- **依赖**：Task 6.2。
- **验收**：Gemini/Codex/上游同步不因目录为空而伪造静态结果或被截断；业务请求仍由目录和能力校验拦截。
- **验证**：上游发现成功、失败、静态兼容 fallback 和未定价差异测试。

**Sprint 6 Demo/验证清单**：

- `/v1/models` 返回集合同时满足“分组可调度”和“已配置定价”。
- Gemini `/v1beta/models`、Codex manifest 和上游同步没有被误改。

---

## Sprint 7：前端类型、交互、i18n 和重复逻辑收口

**目标**：让所有消费者正确表达目录状态，删除重复硬编码，确保 TypeScript/Vue 构建和中文显示稳定。

### Task 7.1：统一 API 类型

- **位置**：`frontend/src/api/admin/accounts.ts`、`frontend/src/api/accounts.ts`、`frontend/src/api/admin/channels.ts`、`frontend/src/types/index.ts` 或专用类型文件。
- **操作**：为模型目录、单测模型、批测模型和 typed error code 定义共享类型；成功模型列表保持现有数组契约，避免管理员和用户端分别声明相近但不兼容的类型。
- **依赖**：Sprint 2、Sprint 3 API 契约冻结。
- **验收**：所有调用者编译时能识别业务错误码和 `retryable`；不再通过 `any` 或英文 message 读取错误响应。
- **验证**：`vue-tsc -b`、API mock 类型检查。

### Task 7.2：统一单测/批测/计划测试 UI 状态

- **位置**：`AccountTestModal.vue`、`AccountBatchTestModal.vue`、`ScheduledTestsPanel.vue`、`AccountsView.vue`。
- **操作**：统一 loading、error+retry、catalog-empty、whitelist-missing、no-intersection、ready 状态；关闭弹窗时取消请求并清理 abort controller；重试不使用旧列表。
- **依赖**：Task 7.1。
- **验收**：任何请求失败都不会显示旧模型或“无匹配选项”；成功新数据覆盖旧状态；提交按钮仅在合法模型选中时可用。
- **验证**：组件测试、AbortController 测试、连续打开/关闭弹窗测试。

### Task 7.3：i18n 文案和 UTF-8 检查

- **位置**：`frontend/src/i18n/locales/zh.ts`、`en.ts` 及必要的其他 locale 文件。
- **操作**：新增“未配置白名单”“当前分组未配置定价”“没有已定价交集”“加载失败，点击重试”“历史模型未定价”等文案；中文文件以 UTF-8 无 BOM 保存。
- **依赖**：Task 7.2。
- **验收**：中文和英文键完整；插值参数有类型/命名一致性；无乱码。
- **验证**：locale key 对比脚本、Vitest 文案快照、文件 BOM 检查。

### Task 7.4：清理静态数组和死导出

- **位置**：`useModelWhitelist.ts`、`ModelWhitelistSelector.vue`、旧 `AccountTestModal.vue` 副本和相关 index/test。
- **操作**：删除仅用于业务候选的静态模型全集；保留确实用于标签/元数据的最小常量；删除死副本后运行全仓引用检查。
- **依赖**：Sprint 4、Task 7.1–7.3。
- **验收**：模型新增只需渠道定价配置；不需要同时改前后端多个常量。
- **验证**：`rg`、TypeScript 构建、相关 Vitest。

**Sprint 7 Demo/验证清单**：

- 管理端和用户端 UI 状态文案一致。
- 失败、空目录、白名单缺失三种场景视觉上可区分。
- 中英文构建和中文显示无乱码。

---

## Sprint 8：回归测试、性能和安全收口

**目标**：用分层测试证明目录硬上限、能力求交和错误可见性没有回归，并确认不会把数据库/凭证数据暴露给客户端。

### Task 8.1：后端单元测试矩阵

- **位置**：`backend/internal/service/*_test.go`、`backend/internal/handler/admin/account_handler_available_models_test.go`、用户 Handler 测试、gateway/room 测试。
- **测试矩阵**：
  - active/disabled 渠道；
  - platform-only/group/channel 查询；
  - 主定价与统计定价合并；
  - Grok/OpenAI/Gemini/Anthropic/Antigravity/OpenCode 平台；
  - 平台账号空 mapping；
  - 私有账号正常、空、缺失 mapping；
  - 白名单只有图片/视频；
  - mapping 含未定价和别名模型；
  - 定价删除后的历史 mapping；
  - 批测共同交集；
  - 房间能力交集；
  - gateway runtime capability 与定价求交；
  - API 错误不得转为 `200 []`。
- **验收**：每个已确认业务规则至少有一个可读测试名；测试失败时能定位目录、能力或 UI 状态层。

### Task 8.2：前端 Vitest 回归矩阵

- **位置**：`frontend/src/components/account/__tests__`、`frontend/src/components/admin/account/__tests__`、`frontend/src/views/admin/__tests__`、`frontend/src/views/user/__tests__`。
- **测试矩阵**：单测成功/失败/重试、批测全量交集、计划模型同步、账号编辑动态目录、房间动态目录、i18n、图片视频过滤、旧静态数组不回退。
- **验收**：不依赖真实网络；mock 响应覆盖 2xx 模型数组、4xx typed error、5xx/network error。

### Task 8.3：API/权限和敏感信息审查

- **位置**：所有新增/修改 Handler、API 类型和日志。
- **操作**：确认管理端只能读取有权限的账号；用户端只能读取自己的账号；错误 metadata 不含凭证；批量查询不允许借 account ID 枚举其他账号详细信息；日志不打印 `model_mapping` 全量和 token。
- **验收**：权限测试通过；响应结构不包含 credentials、proxy secret 或上游响应原文。

### Task 8.4：性能与缓存验证

- **位置**：目录服务、gateway 模型缓存、前端请求层。
- **操作**：确认一次页面打开不会对每个模型执行 N+1 查询；批测使用批量账号读取或有上限的并行查询；定价变更后缓存失效；错误短缓存不会把临时数据库故障变成长期空目录。
- **验收**：常见账号/批测规模下延迟可接受；没有未经限制的 `Promise.all` 扩散或每账号独立全表查询。
- **验证**：Go benchmark/日志计数（仅本地）、前端请求计数测试、缓存命中测试。

### Task 8.5：清理测试和构建产物

- **位置**：`test/`、`backend/internal/web/dist`、本地 release 目录。
- **操作**：一次性脚本、截图、日志、coverage、临时二进制和压缩包只放 `test/` 或 `test/release/`；测试结束清理这些临时文件。正式 Go/Vitest 回归测试必须留在现有测试目录，不能删除；构建产物不作为源码提交，除非仓库明确要求。
- **验收**：`git status --short` 只包含有意修改；没有临时 SQL、dump、日志或调试脚本。

**Sprint 8 Demo/验证清单**：

- 后端相关 Go 测试通过。
- 前端相关 Vitest 和类型检查通过。
- 目录硬上限、分组隔离、私有白名单错误和批测交集均有回归证据。

---

## Sprint 9：历史账号只读审计与可选数据修复（独立审批）

**目标**：在代码规则稳定后识别历史空白名单账号；数据修复不是本次代码发布的默认步骤，必须单独取得授权。

### Task 9.1：编写只读审计查询方案

- **位置**：仅在 `test/` 放临时只读查询脚本；完成后清理；不得提交生产凭证或查询结果。
- **操作**：统计 `OwnerUserID != nil` 且 Grok/其他受影响平台 `model_mapping` 缺失或为空的账号数量、平台分布、用户归属、当前可回填定价模型数量；只读连接，禁止 `UPDATE/DELETE/INSERT/TRUNCATE`。
- **依赖**：Sprint 8 通过；用户明确授权只读数据库审计后才能执行。
- **验收**：输出不包含完整 credentials/token；只给出计数、账号 ID（如确需）和预估影响。
- **阻断条件**：用户未授权、生产连接信息不明确、查询可能扫描超大日志表时停止。

### Task 9.2：提出回填方案和回滚方案

- **位置**：不直接执行；以实施变更单/用户确认消息形式提交。
- **操作**：说明预计账号数、平台和分组范围、每账号回填模型集合、事务边界、备份方式、回滚字段和并发影响；优先生成 dry-run 报告。
- **依赖**：Task 9.1。
- **验收**：用户明确确认后才可实施；若未确认，保持代码 Fail-Fast 和人工修复流程。

### Task 9.3：经授权执行最小范围回填（可选）

- **位置**：单独的数据迁移/运维任务，不混入普通代码提交。
- **操作**：只按已确认账号集合和当前定价目录回填；先备份涉及行/字段；事务执行；写审计记录；回填后重新查询数量和白名单状态。
- **依赖**：Task 9.2 用户明确授权。
- **验收**：回填只影响批准范围；可按备份恢复；无账号凭证字段之外的意外变更。
- **禁止**：运行时静默 fallback、全表无条件覆盖、删除历史模型、直接手写不可回滚 DDL。

---

## Sprint 10：发布、预发布验证与生产回滚

**目标**：在本地和预发布门全部通过后，按 Pixel 当前 binary+systemd+nginx 发布流程上线；生产发布前必须重新确认是否同步文档站。

### Task 10.1：发布前文档站选择门

- **操作**：发布前明确询问“是否同步部署 `docs/site` 到 `pixel-docs.service:8082`”；只有当前对话明确回复“同步”才执行文档站阶段。
- **依赖**：所有代码和测试 Sprint 完成。
- **验收**：记录 `DOCS_SYNC=yes/no`；未确认时只发布主项目。

### Task 10.2：本地版本、BOM、静态检查和测试门

- **操作**：
  - 检查工作区、版本和完整 migration 文件目录；迁移名存在 `006b`、`108a` 等字母后缀，禁止使用 `[int](filename.Split('_')[0])` 这类会报错或漏迁移的解析方法，应按 `migrationFilesThrough` 相同的完整文件名字典序和 checksum 语义比较；
  - 扫描 `backend/migrations/*.sql` UTF-8 BOM，仅在发现时去除 BOM 字节；
  - `go vet -C backend ./...`；
  - 运行受影响 Go 测试；
  - `pnpm --dir frontend run build`，确认 `backend/internal/web/dist` 新文件时间晚于构建开始；
  - 用 `-tags embed`、`GOOS=linux GOARCH=amd64 CGO_ENABLED=0` 编译二进制；
  - 检查 ELF 头 `7f 45 4c 46`、64 位 little-endian；
  - 交叉编译后恢复 `GOOS/GOARCH/CGO_ENABLED` 环境变量。
- **依赖**：Sprint 8 完成。
- **验收**：新增/修改目标测试、touched package、lint、typecheck、构建必须 100% 通过；全仓如果存在已知且可复现的无关预存失败，必须记录命令、测试名、错误和无关证据并交由用户决定是否放行，不能自行忽略。工作区 dirty 时 BuildType 必须为 `dev`，不得谎报 release；若要求可复现 release，必须在用户指定的干净提交/工作树上重建。
- **发布内容门**：当前工作区包含 Ideas Plaza 等其他未提交功能，直接构建会把全部源码和全部 embedded migration 一起带入二进制。生产发布前必须让用户明确选择“本次一起发布这些改动”或“从指定提交/独立干净工作树仅构建模型目录变更”；默认推荐后者。未确认发布内容集合时不得构建生产包。

### Task 10.3：生产远程只读预检

- **操作**：按 `pixeldeploy` 技能执行一轮 SSH 只读预检：
  - SSH 身份、sudo、systemd unit、WorkingDirectory、ExecStart；
  - 当前 release、资源目录、服务状态；
  - PostgreSQL `schema_migrations` 的 `filename/checksum` 集合与本地嵌入 migration 目录对比；当前代码表结构是 `filename` 主键，不存在 `version` 列，因此禁止执行 `max(version)`；
  - `/health`、`/health/ready`、nginx 80/443 基线；
  - 磁盘空间、监听端口、迁移 drop-in；
  - 记录精确集合判定：`PENDING = LOCAL_EMBEDDED_FILES - REMOTE_APPLIED_FILES`；`REMOTE_ONLY = REMOTE_APPLIED_FILES - LOCAL_EMBEDDED_FILES`；并核对交集中每个文件的 checksum。`PENDING` 非空表示待迁移，`REMOTE_ONLY` 非空表示当前 checkout 落后，checksum 不一致表示已应用 migration 被修改，后两者都必须阻断。
- **依赖**：Task 10.2。
- **验收**：旧版本健康、sudo 可用、磁盘足够、数据库没有本地缺失的迁移文件、已应用 migration checksum 与本地一致；若已有故障、DB 包含本地不存在的迁移，或 checksum 漂移，立即停止。
- **数据库边界**：`schema_migrations` 查询属于数据库读取。正式执行前必须向用户展示只读 SQL、用途和零写入影响并取得授权；不得执行数据修改。
- **灰度门校验**：`DATABASE_MIGRATION_THROUGH` 必须是实际嵌入的完整 `.sql` 文件名或空值；如果生产配置不是这种格式、`MIGRATION_MODE` 不是 `validate`，立即阻断并报告，不能按数字简称猜测。

### Task 10.4：压缩上传、校验和、分阶段 smoke test

- **操作**：
  - 将 Linux 二进制 gzip 压缩上传到 `/home/pixel/sub2api_release_uploads`；
  - 远端解压后比较 SHA256 和 ELF 头；
  - 在 `/opt/sub2api/releases/<timestamp>` 建目录，复制二进制和当前 `resources/`；
  - 以 `sub2api` 用户执行 staged binary `--version`；
  - 确认旧 `current` 仍服务 `200`。
- **依赖**：Task 10.3。
- **验收**：哈希一致、版本/commit/BuildType 正确、旧 release 未被切换。

### Task 10.5：数据库迁移判定和安全门

- **操作**：本功能预期不新增 SQL migration。以“本地嵌入文件集合减去远端已应用 filename 集合”计算待执行文件，并逐个核对 checksum；若待执行项来自其他未提交功能（例如当前 `279_ideas_plaza.sql`），立即阻断本次发布，不得因本功能发布顺带应用。只有用户另行授权这些具体 migration 后，才按 `pixeldeploy` 规则执行选择性 schema/相关表备份，使用 staged binary 的 `--migrate-only` 并显式清空 `DATABASE_MIGRATION_THROUGH`。
- **依赖**：Task 10.3、Task 10.4。
- **验收**：迁移文件名、checksum、备份路径、执行前后已应用集合和 gray-release ceiling 全部记录；失败时不切换 symlink。任何 `--migrate-only` 都属于数据库修改，必须在展示具体待执行文件和备份范围后再次获得明确授权。
- **禁止**：full `pg_dump` 599GB 数据库、手写 DDL、回填 `model_mapping`、修改 migration drop-in。

### Task 10.6：切换、重启和应用验证

- **操作**：记录 `PREVIOUS`、`RELEASE`、`STAMP`、可直接执行的 `ROLLBACK_CMD`；切换 `/opt/sub2api/current`；只重启 `pixel.service`；轮询 `/health/ready` 至 200；检查 systemd、SHA256、版本、依赖服务、nginx 基线、监听端口和日志。
- **依赖**：Task 10.5。
- **验收**：服务 running、ready 200、nginx 结果不低于部署前基线、无 panic/反复 error/缺失列或表日志；旧 release 保留用于回滚。

### Task 10.6a：生产功能冒烟授权边界

- **默认允许范围**：只读调用渠道 `model-options`、账号 `/models`、网关 `/v1/models` 并核对 UI，不改变渠道状态、价格、mapping、房间、计划或任务。
- **额外授权范围**：真实“测试连接”会访问上游，可能产生费用并写 usage/log；批量测试和计划测试还会创建任务/计划数据。执行前必须列明测试账号、模型、请求数、预计费用和写入影响并取得明确授权。
- **禁止在生产构造的场景**：禁用/启用渠道、增删价格、制造空 mapping、创建临时房间等写场景只在本地隔离测试栈验证，不为冒烟临时修改生产配置或数据。
- **验收**：默认发布验证不产生业务写入或上游计费；任何额外冒烟都有当前会话授权记录。

### Task 10.7：可选文档站独立发布

- **前提**：Task 10.1 明确 `DOCS_SYNC=yes` 且主项目验证通过。
- **操作**：先只读断言 `docs/site/next.config.ts` 含 `output: 'standalone'`，`docs/site/pnpm-workspace.yaml` 允许 `esbuild`、`sharp`、`unrs-resolver` 构建脚本；缺失即停止并单独修复。随后按 `pixeldeploy`：只打包源码到临时目录，服务器端 `pnpm install --frozen-lockfile` 和 standalone build；复制 `.next/static`、`public`；用 `cp -a` 保留 pnpm symlink；切换 `/opt/pixel-docs/current`，只重启 `pixel-docs.service`；验证 8082、页面内容、CSS/JS 和日志。
- **验收**：文档站失败不回滚主项目；成功后清理临时源码包和构建目录，保留 release 目录作为回滚路径。

### Task 10.8：发布失败回滚

- **快速回滚**：将 `/opt/sub2api/current` 指回 `PREVIOUS`，重启 `pixel.service`，轮询 `/health/ready`，检查日志和 nginx。
- **数据库边界**：symlink 回滚不撤销已应用 migration；只有旧二进制确实无法兼容新 schema 时，才在用户明确确认后考虑选择性 schema/表恢复；恢复表 dump 会覆盖备份后写入的数据，必须先说明数据损失范围。
- **文档站回滚**：只切换 `/opt/pixel-docs/current` 并重启 `pixel-docs.service`，不影响主项目。
- **验收**：回滚命令、路径、版本和健康结果均有记录；不删除旧 release、不清理日志、不进行未经批准的数据恢复。

### Task 10.9：发布验收记录模板

实际发布完成后必须提交以下记录，字段不得省略；没有发生的事项要明确写“未发生/已跳过”，不能留空：

```text
部署结果：成功 / 失败 / 部分成功

【版本】
- VERSION / Commit / BuildType / BuildDate：
- 工作区来源：指定提交 / 已确认 dirty 内容：
- 二进制 SHA256（本地 → 服务器，是否一致）：

【发布路径】
- PREVIOUS=
- RELEASE=
- STAMP=
- ROLLBACK_CMD=

【数据库迁移】
- LOCAL_EMBEDDED_FILES 数量 / REMOTE_APPLIED_FILES 数量：
- PENDING / REMOTE_ONLY / checksum 差异：
- 迁移判定：无漂移已跳过 / 已授权并应用的具体文件：
- 备份路径：本次无迁移 / 实际路径：
- DATABASE_MIGRATION_MODE / DATABASE_MIGRATION_THROUGH：

【验证】
- systemctl is-active pixel / postgresql-18 / redis / nginx：
- /health /health/live /health/ready：
- nginx 80 / 443：部署前基线 → 部署后：
- 公网访问：
- 当前 symlink / 服务器 binary hash / version：
- 端口监听：
- NRestarts：
- 日志异常：无 / 摘要：
- 功能冒烟：只读项 / 额外授权项：

【文档站】
- DOCS_SYNC=yes/no（用户当前会话明确决定）：
- 若同步：DOCS_PREVIOUS / DOCS_RELEASE / DOCS_ROLLBACK_CMD / 8082 验证：

【风险与遗留】
-

【临时文件清理】
- 本地 test/release、coverage、压缩包：
- 远端上传/tmp 临时文件：
- 保留的主项目/文档站 release 回滚资产：

【提交信息建议】
-
```

## 6. 测试策略与验收矩阵

### 6.1 目录层验收

| 场景 | 期望 |
|---|---|
| active 渠道含 A/B，disabled 渠道含 C | 输出 A/B，不含 C |
| 两个 active 渠道重复 A | 输出一个 A |
| 主定价 A，统计规则 B | 输出 A/B |
| group-1 绑定 channel-1，group-2 绑定 channel-2 | group-1 不见 channel-2 模型 |
| 无 group 上下文 | 平台 active 渠道并集 |
| 平台没有定价 | 明确 catalog-empty，不返回静态默认模型 |
| 删除最后一份定价 | 新目录不含该模型，历史 mapping 不自动删除 |

### 6.2 账号/测试层验收

| 账号状态 | 期望 |
|---|---|
| 平台账号，mapping 空 | 使用对应定价目录 |
| 私有账号，mapping 正常且全已定价 | 返回 mapping 与目录交集 |
| 私有账号，mapping 空/缺失 | 明确 whitelist-missing |
| 私有账号，mapping 全未定价 | 明确 no-priced-intersection |
| mapping 含图片/视频 | 目录层保留，普通文本连接测试按协议过滤并明确原因 |
| 账号平台与 group 不一致 | scope-mismatch |
| 请求接口 4xx/5xx | UI 显示加载失败和重试，不显示正常空态 |

### 6.3 批量/房间/网关验收

| 场景 | 期望 |
|---|---|
| 批量账号能力 A/B、A/C | 共同可测模型为 A |
| 代表账号顺序改变 | 结果不变 |
| 房间账号能力交集 A/B 与 group 定价 A/C | 房间模型为 A |
| gateway group 定价 A/C、运行时能力 A/B | `/v1/models` 为 A |
| 已定价但无运行时账号 | 不出现在 `/v1/models` |
| 上游发现返回未定价 X | 上游发现可保留 X，但业务选择/调度不可使用 X |

### 6.4 验收命令建议

本地命令需根据最终受影响包微调，但最低覆盖：

```powershell
go test -C backend ./internal/service/... ./internal/repository/...
go test -C backend ./internal/handler/admin/... ./internal/handler/...
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend exec vitest run src/components/account/__tests__/AccountTestModal.spec.ts
pnpm --dir frontend exec vitest run src/components/admin/account
pnpm --dir frontend run test:run
pnpm --dir frontend run build
```

不使用 `go test -race`，因为当前 Windows 环境没有 gcc；不得把这一限制伪装成 race 测试已通过。

## 7. 修复前后状态与影响对比

| 项目 | 修复前状况及影响 | 修复后目标状况及影响 |
|---|---|---|
| 基础目录 | 新增/编辑使用渠道定价，测试使用 mapping/平台默认，来源分叉 | 所有业务候选模型先经过唯一渠道定价目录 |
| Grok 私有测试 | 空 mapping 返回 `[]`，前端显示“无匹配选项” | 明确白名单缺失或无定价交集，不再静默回退 |
| 平台账号测试 | 可能显示未定价平台默认模型 | 只显示已定价目录中的模型 |
| 批量测试 | 只取第一条代表账号，可能误选其他账号不支持的模型 | 使用全部目标账号可测试模型交集 |
| 计划测试 | 与即时测试来源可能漂移 | 复用相同 resolver 和服务端校验 |
| 分组隔离 | 现有并集主要按平台，可能看到其他 group 模型 | 有 group 上下文时严格按绑定渠道求并集 |
| 房间创建/编辑 | 前后端静态列表和账号交集分叉，已有模型漂移 | 动态 group 定价目录与房间账号能力求交 |
| 网关 `/v1/models` | 运行时能力没有统一受定价上限约束 | `group priced ∩ runtime schedulable capability` |
| 上游发现 | 与业务目录容易混淆 | 保持发现独立，未定价模型不进入业务选择/调度 |
| 错误展示 | 4xx/5xx、空目录、空 mapping 都像“无选项” | 加载失败、空目录、白名单缺失、无交集分别呈现 |
| 管理员写入 | 可制造 owner 存在但 mapping 为空的状态 | 归属变更和创建路径执行完整性校验 |
| 历史数据 | 空白名单静默遗留，未有迁移方案 | 先只读审计；回填需单独授权、备份和回滚 |
| 重复代码 | 多份测试弹窗和静态模型数组易漂移 | 单一目录服务、单一 resolver、删除死副本 |

## 8. 关键风险、注意点与应对

### 8.1 “已定价”不等于“账号可执行”

定价目录是硬上限，不是账号能力证明。直接返回完整平台并集会导致用户选择后被 `IsModelSupported` 拒绝，尤其是用户私有账号。必须坚持求交。

### 8.2 私有空 mapping 不能用隐藏 fallback 修复

现有调度和请求校验把私有空 mapping 解释为拒绝全部。测试页面偷偷回退定价并集会制造“列表能选、提交失败”的矛盾。正确做法是显式错误和后续授权数据修复。

### 8.3 禁用/删除定价不能自动破坏历史配置

模型从目录退出后，禁止新增和新请求，但不自动从账号 mapping、房间历史配置或计划记录中删除，避免未经授权的数据破坏；编辑界面应标记“未定价/已失效”。

### 8.4 分组绑定和平台不一致

当前渠道关联关系存在缓存和冲突校验。查询 group 时必须使用现有绑定事实，不可根据平台名猜测渠道；未绑定时不得回退全局。

### 8.5 通配符和别名模型

计费通配符、账号 mapping 左侧请求模型、mapping 右侧上游模型的语义不同。目录匹配使用客户端请求模型键；通配符不得直接作为 UI 选项；别名右侧上游模型不能替代定价键。

### 8.6 上游发现与业务目录边界

Gemini、Codex、上游同步、Usage 查询并不是同一种“可选择模型”语义。只在业务配置、测试和调度出口使用目录硬上限，避免破坏协议兼容和诊断能力。

### 8.7 API 兼容

本方案保留旧单账号成功数组，使用标准业务错误码表达非 ready 状态，因此无需让客户端猜测数组/对象两种成功类型。新增批量查询使用独立 endpoint。发布时仍需让后端和嵌入式前端同一版本切换，避免旧前端不认识新增错误码。

### 8.8 缓存一致性

渠道定价和分组绑定变更必须失效目录缓存；多实例环境遵循现有集群缓存协调机制。不得引入没有失效路径的永久缓存，也不得把 DB 临时错误缓存成合法空目录。

### 8.9 批量规模和 N+1

批量测试不能对每个账号、每个模型重复查询渠道表。优先一次读取账号集合和一次目录，再在内存中求交；对账号数量设置合理上限并在 API 层拒绝超限。

### 8.10 现有未提交工作区

当前工作区已有 `ideas-plaza` 相关修改和 migration。实施时不得运行 `git reset --hard`、`git checkout --`、全目录格式化或覆盖式生成；Wire 文件若需更新，应只改生成相关差异并先审查现有 dirty diff。

### 8.11 数据库安全

本功能代码阶段不需要 schema migration。任何只读审计或回填都必须先说明查询范围和影响；回填前给出账号数量、平台/分组分布、备份和回滚方式，得到明确授权后再做。

## 9. 回滚策略

### 9.1 代码层回滚

- 每个 Sprint 以小提交交付，目录服务、resolver、消费者和清理任务尽量分离提交。
- 若 Sprint 2 之后发现 typed error 兼容问题，先回滚前端消费者或恢复旧错误展示，不回退目录服务硬上限逻辑。
- 保留旧静态代码的删除前引用审计结果；不通过恢复硬编码模型来掩盖问题。

### 9.2 发布层快速回滚

- 发布前记录 `PREVIOUS`、`RELEASE`、`STAMP`、二进制 SHA256 和可执行 `ROLLBACK_CMD`。
- 发现新版本 crash-loop、ready 非 200、网关模型异常或关键日志错误时，只切回旧 `current` 并重启 `pixel.service`。
- 旧 release 不删除；验证回滚后的 ready、nginx 和日志。

### 9.3 数据层回滚

- 本次正常代码发布不应写账号 mapping，原则上无需数据回滚。
- 如果另行授权执行历史回填，必须在独立变更中保存最小范围备份/旧值；回滚只恢复批准字段和批准账号。
- 不使用全库 dump；不恢复 599GB 日志表；任何表恢复会覆盖备份后写入的数据，必须由用户再次确认。

## 10. 完成定义（Definition of Done）

只有以下条件全部满足，才能认为实施完成：

1. 渠道定价目录是唯一业务基础白名单，且支持 platform/group/channel 范围。
2. active、主定价、统计定价、去重、排序和缓存失效均有测试。
3. 单账号、批量、计划测试共享 resolver；Grok 私有空 mapping 有明确错误态。
4. 用户端下拉结果不会产生服务端白名单 400；管理员绕过前端也会被服务端校验。
5. 账号新增/编辑/导入、房间创建/编辑、网关 `/v1/models` 均遵守目录硬上限。
6. Gemini/Codex/上游发现、Usage、定价编辑器保持各自正确语义。
7. 前后端静态业务模型全集和死测试弹窗已清理，`rg` 无未使用运行时引用。
8. Go、Vitest、TypeScript、前端构建和本地专属冒烟全部通过。
9. 没有未经授权的数据库写入、schema migration、生产配置修改或临时文件遗留。
10. 发布报告包含版本、commit、BuildType、SHA256、迁移判定、健康检查、nginx 基线、日志、回滚命令和文档站同步结果。

## 11. 建议提交粒度

建议按以下提交边界组织，方便审查和独立回滚；实际提交前仍需检查工作区 dirty 文件，不能把用户已有修改一起提交：

1. `feat(model-catalog): add scoped priced model catalog`
2. `feat(account-test): resolve test models from priced catalog and account capability`
3. `fix(account): prevent owned accounts with missing model whitelist`
4. `feat(account-batch-test): use common priced model intersection`
5. `feat(account-share): scope room models by group pricing and account capability`
6. `fix(gateway): intersect runtime models with group pricing`
7. `refactor(frontend): remove static business model lists and duplicate test modal`
8. `test(model-catalog): cover pricing scope and consumer consistency`

如果本次没有 schema migration，提交说明可以写“未涉及数据库结构变更”；如果执行阶段发现并应用了其他待发布 migration，必须如实列出，不能写成数据库未变化。

## 12. 执行顺序摘要

```text
Sprint 0 基线/契约
        ↓
Sprint 1 目录服务 ─────────────┐
        ↓                       │
Sprint 2 账号测试 resolver      │
        ↓                       │
Sprint 3 批测/计划测试          │
        ↓                       │
Sprint 4 账号配置/静态清理      │
        ├───────────────┐       │
        ▼               ▼       │
Sprint 5 房间       Sprint 6 网关
        └───────┬───────┘       │
                ▼               │
Sprint 7 前端/i18n 收口         │
                ↓               │
Sprint 8 测试/性能/安全         │
                ↓               │
Sprint 9 历史数据只读审计（需授权，独立）
                ↓
Sprint 10 本地构建 → 远程预检 → 分阶段发布 → 验证/回滚
```

推荐实际开发时先完成 Sprint 1–3，尽快恢复 Grok 测试和测试链路的一致性；Sprint 4–6 负责全项目模型来源收口；Sprint 7–8 负责质量门；Sprint 9 永远与普通代码发布解耦，Sprint 10 在用户明确决定是否同步文档站后执行。
