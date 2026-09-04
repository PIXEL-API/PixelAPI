# Pixel 核心能力增量重建设计与实施蓝图

> 状态：请求计费与席位小时费双计费、使用记录中的余额流水、奖励、提现、邀请消费返利与前端保真范围已纳入；专项逻辑复核完成，可进入 Phase 0/1，核心业务编码须先通过兼容门禁
>
> 编写日期：2026-09-04；最终逻辑审查：2026-09-05
>
> 上游基线：Sub2API `main`，提交 `b1748c4ea99ce2120401a269142aa071e18a84da`，`backend/cmd/server/VERSION=0.2.0`
>
> 远端核验：2026-09-05 通过 `git ls-remote upstream refs/heads/main` 再次确认远端 `main` 与上述提交一致
>
> 数据策略：继续使用现有生产数据库；以上游表结构为主，按需加字段、加表、加索引，不新建空库搬运核心数据，不默认全量回填
>
> 实施边界：本文只确定设计、兼容方式和实施顺序；任何生产 DDL、数据修正、迁移记账、删表、部署、重启、提交或推送均需另行执行和授权

## 1. 最终结论

新版不再重建一套独立的 Pixel 技术内核，而是把精确的上游 `0.2.0` 代码作为唯一主干，在上游已有 Handler、Service、Repository、调度器、定价器和事务内做最小增量。

核心原则只有一句话：

> 选定上游基线后不重排其调用链、不替换其核心解析器、不改变既有跨步骤业务事务边界；Pixel 只在明确扩展点增加必要字段、过滤条件、业务表和同事务附加写入。不启用上游 `payment_fulfillment -> affiliate` 充值返利副作用，是删除未采用的业务副作用，不改变其余支付步骤。唯一跨步骤、状态编排层面的事务边界例外，是为发票与退款互斥增加退款状态 claim/CAS 短事务，且不得包住权益扣减或 Provider I/O。为保证现金余额与审计流水同成同败，允许在每个原有单次 `users.balance` mutation 的原调用点，把该一次数据库变更收口为 `balance + user_balance_ledger` 的单条原子 SQL；仅当现有 Repository 无法用单条语句表达时，才使用只覆盖这一次 mutation 的局部短事务。该局部原子性不跨业务步骤、不包住外部 I/O、不与 finalize 合并，也不改变原调用顺序。

当前旧项目承担三种角色：

- 产品规则参考：保留已经证明合理的用户自有账号、三种投放模式、账号广场、定价目录、多分组、积分与奖励、现有邀请关系与消费抽成、提现和商业运营逻辑；
- 生产数据合同：已有 ID、关系、余额、积分、API Key、账号、邀请关系、分润、提现、订单、支付、发票、卡密和结算历史必须继续可识别；
- 回归案例来源：保留旧项目中能证明业务语义的测试场景，但不复制大型 Service、重复 Handler、旧状态机和完整历史迁移链。

新版产品定位为：

> 以用户为主的 AI 账号托管与共享平台。用户管理自己的账号并选择私用、公共号池或账号房间投放；消费者通过 API Key 使用平台或他人共享的账号；平台在上游网关能力之上统一完成模型授权、账号调度、动态计价、使用记录与余额流水、奖励、邀请消费返利、提现和商业运营。

## 2. 不可违反的实施约束

| 编号 | 约束 | 实施含义 |
|---|---|---|
| P01 | 上游代码是唯一主干 | 从 `b1748c4e...` 创建干净分支或工作树，不从当前 fork 反向整理 |
| P02 | 保持上游调用顺序 | 逐 Handler、逐协议保留各自已有阶段顺序；不把不同入口强行整理成一条统一流水线。唯一明确删除的业务副作用是未采用的上游充值返利调用，其余支付步骤顺序不变 |
| P03 | 保持上游事务边界 | 新增写入优先附加到已有事务；不得再包一层总事务或在普通请求转发前新增资金预授权事务。发票互斥所需的退款状态 claim/CAS 是唯一跨步骤事务例外。原来是 autocommit 的单次余额 mutation 可在原调用点收口为 `balance + ledger` 的单条原子 SQL，必要时仅使用覆盖该 mutation 的局部短事务；两者都不得包含 Provider I/O、相邻业务步骤或 finalize，也不得改变 Prepare、权益扣减、Provider I/O 和 finalize 的先后关系 |
| P04 | 复用上游解析和调度 | Composite、模型映射、定价、账号调度、代理切换和 Provider 转发均先复用上游实现 |
| P05 | 一个规则只实现一次 | Pixel 只增加窄 Helper/Repository 条件；HTTP、WebSocket 和不同协议在原位置调用同一 Helper |
| P06 | 生产数据原地兼容 | 保留现有主键和业务关系；默认不搬表、不重编号、不重放支付、不重建财务历史 |
| P07 | 迁移只做必要增量 | 优先 `ADD COLUMN`、`CREATE TABLE`、新索引；非纯加法变更必须有只读前检、单独审批和回滚办法 |
| P08 | 失败关闭 | 目录、权限、房间、代理或资金状态无法确定时明确拒绝，不使用静态模型、默认币种或服务器直连兜底 |
| P09 | 不预留未使用架构 | 不创建四领域内核、总 Planner、RuleGraph、Port 大体系、第二迁移器或空状态机 |
| P10 | 首次切换不删历史 | 退出范围的业务表/列先停止新写并保留查询；物理删除放到后续独立退役批次。仅四个与新版房间语义冲突的唯一索引可在精确核定义并单独获批后删除 |
| P11 | 保留现有前端风格 | 保留 Vue/Tailwind 技术栈、应用壳、明暗主题、语义 token、route skin、核心路由和页面骨架；只重接业务数据与状态，不借重建全站换肤 |
| P12 | 资产口径明确 | 积分是不可提现的平台权益；提现继续针对 `users.balance`；邀请返利仅指共享账号的请求费用或已确认席位小时费产生的一级邀请人抽成，与账号主分润一同由服务端在各自结算事务中按不可变快照计算；小时豁免按原结算快照逆向，前端不得自行推导；支付充值不产生邀请返利 |

## 3. 核心范围

### 3.1 本次必须实现

| 功能簇 | 首版范围 |
|---|---|
| 用户自有账号 | 普通用户新增、导入、刷新、测试、编辑和停用自己的账号；复用上游账号校验、OAuth、凭证和健康逻辑 |
| 三种账号投放 | `PRIVATE / PUBLIC_POOL / ROOM`，一个账号只有一种有效对外投放模式 |
| 多分组路由 | 一个 API Key 可配置多个 Group 路由；保留优先级、权重、启用状态和冷却语义 |
| 模型与渠道治理 | 启用渠道中已定价模型的并集作为根目录，再与 Group、账号、房间、网关和 Provider 能力求交 |
| 等级号池 | 继续使用账号 `account_level`、Group `required_account_level` 和上游账号调度条件 |
| 账号广场 | 多账号房间、Key 加入/离开、动态倍率请求计费、席位小时费的一分钟滚动预付、低消豁免，以及号主/有效一级邀请人/平台三方分账 |
| 使用记录与余额流水 | 保留 `/usage` 的请求记录/余额流水双页签；用户可查看自己的现金余额入账、扣费、变动后余额、业务原因和引用，管理端保留同源全局查询 |
| OpenCode | 产品上是独立平台；内部作为薄 Provider Adapter 复用 OpenAI 协议和流式能力 |
| 峰谷定价 | 保留生产已有 Channel 时间段价格和 Group 时间段倍率，在上游现有定价函数内做兼容适配 |
| 独立代理归属 | 平台共享代理和用户专属代理；到期后只切换兼容备用，不默认直连 |
| 商业运营 | 单商品订单、文本卡密、余额支付、现有外部支付、确定性福利活动、兑换中心 |
| 积分与奖励 | 积分余额/流水、积分优先抵扣与商城支付；福利和兑换支持积分、永久并发与有期限的临时并发奖励 |
| 邀请关系与消费返利 | 以当前 Pixel 模块为准：保留邀请码、直接邀请关系和有效期；PUBLIC_POOL/ROOM 请求费用及 ROOM 已确认小时费按账号主、有效一级邀请人、平台三方拆分，邀请人抽成实时进入余额；小时豁免按原快照返冲 |
| 提现 | 沿用余额提现、收款码、用户申请/取消和后台结算/驳回；积分不可提现 |
| 发票 | 发票资料、来源选择、申请/取消、后台导出、开具/驳回和事件审计 |
| 订单与财务后台 | 订单、支付、退款、履约、余额/积分流水、请求费与小时费、邀请消费抽成、共享分润、提现和异常对账查询 |
| 前端兼容 | 保留现有应用壳、路由、主题、页面视觉骨架和响应式行为；旧业务组件只作外观参考 |

### 3.2 明确不进入首版

- Ideas Plaza；
- 账号广场排队、预约、排空、退出中、异步生命周期 operation；
- 房间评分、评论、推荐算法和消费画像；
- 固定时长包、长周期预付套餐和条款版本合同；一分钟滚动席位预付及小时费不在此排除项中；
- 多商品购物车、优惠券叠加、随机抽奖、排行榜和保底；
- 新支付渠道、外部开票 Provider 和发票文件交付状态机；
- 积分兑换现金、积分提现、积分过期/冻结和第二套积分资产；
- 上游充值订单返利、返利冻结、成熟、额度转余额和相应管理体系；
- 二级、三级或无限级代理树、团队业绩和代际分佣；首版“复杂分销”只指现有一级邀请关系参与 PUBLIC_POOL/ROOM 的账号主、邀请人、平台三方消费分账；
- 自动打款 Provider、多币种提现和新的可提现子账户；
- 集群控制面、工单、独立文档站和本次未列出的旧模块；
- 新双分录钱包、历史 opening、正常网关请求预授权；
- 微服务拆分或为了“边界漂亮”搬迁上游目录。

这些模块的已有生产历史不能在首次切换时删除。新代码停止写入后，再根据数据、外键、报表、审计和保留期单独决定归档或删除。

## 4. 上游主链与 Pixel 最小扩展点

### 4.1 必须逐入口保护的请求顺序

上游没有一条适用于所有协议的统一流水线。API Key/Group 上下文、模型解析、渠道映射、安全审核、用户并发、计费资格、账号调度、代理、Forward、RecordUsage 和 UsageBilling 是需要保护的阶段集合，不代表它们在每个 Handler 中都存在，也不代表固定为同一先后顺序。

固定基线中的反例已经确认：

- `GatewayHandler.WebSearch` 先检查计费资格，再做安全审核，而且没有用户并发槽；
- `OpenAIGatewayHandler.ChatCompletions` 先做安全审核，再解析渠道级模型映射；
- 其他 HTTP、SSE、WebSocket、Gemini 和图片入口也必须以各自上游实现为准。

因此 Phase 0/1 必须先生成“入口阶段矩阵”，至少记录：Handler/方法、请求解析、模型/渠道解析、安全审核、用户并发、计费资格、账号调度、账号并发、代理、Forward、Usage 以及 Pixel 插入点。后续只允许在矩阵标明的位置调用窄 Helper；某入口原本没有的阶段，不因重建而顺手补齐或搬动。

上游实现已经提供：

- `CompositeRouteResolver`：显式路由、账号模型归属、内置探测器和确定性优先级；
- `ModelPricingResolver`：Group、Channel、LiteLLM、fallback 的既有解析顺序；
- 各平台账号调度、粘性会话、continuation 和 Provider Forward；
- `UsageBillingRepository.Apply`：幂等认领和单事务的订阅、余额、Key 配额、限流与账号配额更新；
- `ResolveProxyFallbackTarget` 和现有代理到期扫描事务；
- 单一 migration runner、advisory lock、`schema_migrations`、Atlas baseline 元数据和逐文件事务。

这些能力不再复制或替换。

### 4.2 只允许增加的十一个窄扩展点

| 扩展点 | 最小改动 | 明确不做 |
|---|---|---|
| API Key 鉴权投影 | 一次读取 `api_key_group_routes`，生成有序候选；`api_keys.group_id` 保留为主 Group 兼容投影 | 不创建 Route Scope、Supply Pool 或新路由图 |
| Group 外层选择 | 在进入现有单 Group 流程前选择候选 Group；容量/资格允许在响应输出前换下一个候选 | 不改写单 Group 内部账号选择和 failover 顺序 |
| 模型授权 | 增加一个共享的 priced catalog Helper，供模型列表、账号写入、房间发布和网关终检调用 | 不替换 Composite，不再建 RuleGraph |
| 积分计费资格 | 在上游每个既有 `CheckBillingEligibility` 阶段内部，把非订阅分支的 balance-only 谓词窄改为“现有余额资格或 `prefer_points_billing && points_balance>0`”；其他订阅、平台额度、Key 限流和 RPM 顺序不变 | 不新增或搬动 preflight 阶段，不因积分跳过订阅/额度/限流检查 |
| 房间绑定与费率 | Key 有 active membership 时，在现有账号候选查询上增加 `listing_id/platform/account_level/model` 条件，并在现有费率解析点提供房间有效倍率 | 不持久化请求级房间账号绑定，不旁路上游调度，不在计费结果上二次乘倍率 |
| 代理资格 | 在现有备用代理选择中增加 owner、平台、等级和状态判断 | 不建 ProxyFallbackPlanner，不改变每源代理事务 |
| 共享请求结算 | 给现有 `UsageBillingCommand` 增加 Apply 前稳定的来源/房间/账号/号主/倍率/政策/消费者请求费用快照并纳入指纹；邀请人资格和三方金额只在原 `Apply` 事务内解析并写 request settlement | 不在网关预查邀请人，不新增前置 WalletReservation，不改变原效果顺序，不把席位小时费揉入 UsageBilling 或 API Key 用量 |
| 席位小时结算 | 复用 Pixel 现有独立短事务语义：加入时一分钟预付，worker、加入前和请求前 catch-up 共用同一 Repository 结算函数，离开时结清并退未发生预付，低消迟到时按原快照补偿 | 不原样装配同时启动五类任务的旧 `StartSeatBillingWorker`，不让计费 worker 激活 membership，也不恢复 queue/drain/消费者暂停恢复或通用 operation |
| 有效用户并发 | 在 API Key/JWT 认证投影形成前读取当前有效临时 grant，并与 `users.concurrency` 求和后写入既有 `AuthSubject.Concurrency` | 不修改各 Handler 的并发阶段，不用 worker 到期回写永久并发 |
| 事务内奖励发放 | 增加一个接收现有事务的窄奖励分派和积分变更 Helper，供福利、兑换、商城与 UsageBilling 在原事务中调用 | 不建通用钱包/事件总线，不在 Service 中再次开启事务，不复制积分 SQL |
| 邀请消费抽成与提现附加写入 | 请求抽成在 `UsageBillingRepository.Apply` 原事务尾部随 request settlement 落账；小时抽成及豁免返冲在独立 seat settlement 事务内落账；提现沿用现有锁序，只在原事务补余额流水 | 不接入上游 `payment_fulfillment -> affiliate` 充值返利，不跨事务补写分润，不写冻结额度或二次转余额，不自动打款，不回填伪造历史流水 |

### 4.3 多分组的边界

`api_key_group_routes` 是 Pixel 必要增量，不是上游原生表。它只负责选择本次请求使用哪个商业 Group；选中后，Group 的价格、订阅、额度、限流、Usage 和 continuation 归属必须一致。

实现规则：

1. 鉴权时一次读取 enabled routes，并按现有 `priority/weight/cooldown_seconds` 语义生成候选；
2. `api_keys.group_id` 始终保留，并对应一个 enabled route，作为上游兼容投影；运行时 Route 顺序以 `api_key_group_routes` 为准，不把该投影当作权威；
3. Group 候选只在消费者尚未收到任何响应字节时切换；
4. 容量或资格不足使用“跳过候选”语义，不把 Group/账号标记成上游故障；
5. 已确认的上游失败才进入现有故障切换和 cooldown；
6. `previous_response_id`、WebSocket continuation 和已有粘性会话继续由原拥有者处理，不重新选 Group；
7. 首字节发出后禁止跨 Group；
8. HTTP、SSE、WebSocket、Gemini 等入口在各自现有位置调用同一 route Helper，不把所有协议搬入新总编排器。

新建或编辑完整 Route 集合时要求 Group 平台一致。生产现有 157 个 enabled Route 集合跨多个平台，为满足“不改生产数据”的要求，首切继续保留原行；请求时仍按请求平台、模型和端点能力过滤候选，不自动改组或禁用 Key。Phase 3 必须为这 157 个存量集合建立逐入口特征样本，证明新 Route Helper 与旧行为一致后才能切流；这些 Key 后续被编辑时才要求一次性收敛为同平台。

## 5. 核心功能规则

### 5.1 用户自有账号

上游 `accounts` 继续是唯一账号表和账号健康、配额、凭证、等级、所有者、代理等运行态事实源。账号的对外投放模式则以 `account_external_placements` 为唯一权威；`accounts.share_mode/share_status/share_policy_id` 只作为 PUBLIC_POOL 旧结算路径的兼容投影，不创建第二张用户账号表。

权限规则：

- 普通用户只能读取和修改 `owner_user_id=当前用户` 的账号；
- 管理员系统账号仍按上游管理员流程维护，不暴露给普通用户；
- 用户新增、OAuth、PAT/凭证导入、刷新、测试和健康检查复用同一套上游校验函数；
- 用户输入的模型列表必须来自统一 priced catalog，目录不可用时拒绝扩大权限；
- 凭证继续使用上游既有不透明表示和加密/解密入口，不在新模块自行解析或复制；
- 账号禁用、过期、限流、配额、健康、粘性和 continuation 仍由上游字段与调度器判断。

### 5.2 三种账号投放模式

物理表示优先兼容现有生产结构：

| 逻辑模式 | 物理表示 | 调度含义 |
|---|---|---|
| `PRIVATE` | `account_external_placements` 无该账号记录 | 只进入账号所有者的私有 Group/个人使用路径 |
| `PUBLIC_POOL` | 一条 `placement_type='public_pool'`，带 `public_group_id` | 进入指定公共 Group 的上游账号候选 |
| `ROOM` | 一条 `placement_type='room'` 作为房间投放资格；实际房间关系在 `account_share_room_accounts` | 只在已加入的房间内成为候选；未挂房间时不对消费者供给 |

约束：

- `account_external_placements.account_id` 主键保证一个账号最多一种对外投放模式；
- ROOM 的实际归属以 `account_share_room_accounts` 为准，一个账号最多加入一个房间；
- placement 与 room-account 必须保持 owner、platform、account_level 一致；
- `accounts.share_mode/share_status/share_policy_id` 不参与三态判定；切到 PUBLIC_POOL 时同步为 `public/approved/对应 policy`，切到 PRIVATE 或 ROOM 时同步为 `private/approved`；既有 `share_policy_id` 可保留作以后再次投放的配置，但非 `public/approved` 时不得触发分账；
- `account_groups` 只是上游 scheduler 所需投影，不能反推投放模式；
- 不涉及 ROOM 的 PRIVATE/PUBLIC_POOL 切换继续先锁账号，并在取得账号锁后重验当前模式仍非 ROOM；若已并发切入 ROOM，则中止并从 listing-first 路径重新开始，禁止在持有账号锁时补锁 listing。当前模式或目标模式涉及 ROOM 时，先用只读查询解析当前/目标 listing ID，按 ID 顺序锁涉及的 listing，再锁 room-account/accounts 与 placement，并在锁后重验当前投放关系；随后校验目标并更新 placement/room-account、`accounts` 公共分账投影和 `account_groups` 后提交。不得出现账号锁后再等待房间锁的反向路径；
- 普通读取不得自动修复投影；三者不一致时 fail-fast 并进入 Phase 0 差异清单，避免双向触发器或读时写入形成第二事实源；
- 不在模式切换中重置账号健康、配额、session、限流或代理状态；
- 离开 PUBLIC_POOL/ROOM 后删除外部 placement 即回到 PRIVATE，不再创建 private placement 行。

生产中 2,771 条 ROOM placement 只有 138 条已加入具体房间，这说明 ROOM placement 当前也承担“房间模式资格”而不是具体房间归属。新版沿用该语义，不为命名整洁搬数据。

### 5.3 模型与渠道治理

每个候选 Group 的模型集合按下面的顺序求值：

```text
Root(group)
  = 启用 Channel 中存在有效定价的具体模型并集

Allowed(group, key, room, protocol)
  = Root(group)
  ∩ Group 模型规则
  ∩ 当前可调度账号能力并集
  ∩ 房间 allowed_models（Key 已加入房间时）
  ∩ 网关端点/协议能力
  ∩ Provider 实际能力

Models(key)
  = 对 Key 的可用 Group 求 Allowed 的并集并去重
```

实现要求：

- `ListPricedModelIDs` 或等价窄 Helper 是目录根集合的唯一入口；
- 目录只包含具体 model ID，不把 `*` 当作可展示模型；
- account-stats 或账号级价格只能细化目录内模型，不能扩大根集合；
- `/v1/models`、用户账号模型选项、房间模型选项、保存校验和网关终检共享该 Helper；
- 每个模型在内部保留可用 Group 来源，真正请求时再按 Route 顺序选择有效 Group；
- 模型映射和平台判定继续调用上游 Composite；Pixel 不实现第二套 exact/prefix/endpoint 优先级；
- 计费继续调用上游 `ModelPricingResolver` 和 `CalculateCostUnified`；priced catalog 是授权边界，不替换计费器；
- Channel/Group/账号/房间任何必需来源读取失败时返回明确错误，不回退静态模型表。

三个模型身份必须分开，不能因为“统一目录”而强制同名：

- `PublicModel`：客户端看到和请求的名称；普通 Group 通常等于具体模型，Composite 可继续使用上游公开别名；
- `UpstreamModel`：上游 Composite 在既有阶段为具体账号解析出的实际转发模型；
- `BillingModel`：上游现有请求模型、渠道映射和响应模型规则最终选定的计费身份。

网关先验证 `PublicModel` 至少属于一个候选 Group；Composite 解析后，再确认 `UpstreamModel` 仍落在同一 Group 的 priced root、账号、房间和端点交集内。`BillingModel` 继续由上游计费链决定，并必须能被上游定价器确定性核价；Pixel 不自行把三者互换。Composite 别名只有在其实际目标全部通过上述检查时才能展示，wildcard 只作为匹配规则，永远不作为目录项返回。

### 5.4 等级号池

首版继续使用现有字段和关系：

- `accounts.account_level` 表示账号等级；
- `groups.required_account_level` 表示 Group 需要的等级；
- `account_groups` 表示账号属于哪些上游调度 Group；
- 上游 scheduler 继续完成健康、容量、配额、限流、优先级和粘性判断；
- Pixel 只在账号候选查询中附加 owner、placement、room 和 level 条件。

房间必须同平台、同账号等级。生产 103 个已挂账号的房间中混合等级数量为 0，现有外键也按 owner/platform/account_level 约束；因此不为“未来可能混级”拆约束或增加复杂定价。

### 5.5 账号广场

账号广场的对象保持简单：

- `Room`：房间展示、请求倍率、席位小时价、低消豁免门槛和最低余额配置；
- `RoomAccount`：号主放入房间的可调度账号；
- `Membership`：一个 API Key 是否加入一个房间，以及加入时冻结的小时价/豁免门槛、空闲退出分钟数、`paid_until` 和 `billed_until`；
- `SettlementEntry`：请求费用 `usage_request`，或席位费用 `seat_charge / seat_refund / seat_waiver_refund` 的消费者、号主、有效邀请人和平台金额快照。

不新增 RoomTermsRevision、OpenRoomBinding、排队、排空或通用 operation 状态机。加入确认只使用房间行版本和短期 intent 防止确认后价格静默变化，最终仍以加入事务锁住的房间配置生成 membership 快照，不创建独立条款版本。已有复杂生命周期历史表保留只读，但新代码不再写入。

房间运营配额保留现有最小能力：原地复用 `account_share_quota_policies` 的 `max_live_rooms / max_room_creates_24_hours / max_accounts_per_room / max_room_accounts_per_owner`、全局默认、房主覆盖和审计；它只约束创建/编辑，不触发 drain、排空或 membership operation。配额表和生产值在 Phase 0 只读核对后收养，不新增第二套限额体系。

#### 房间状态

业务只暴露三种状态：

| 业务状态 | 现有生产物理映射 | 行为 |
|---|---|---|
| `ACTIVE` | `deleted_at IS NULL AND status='active'` | 可以加入并发起新请求 |
| `PAUSED` | `deleted_at IS NULL AND status='paused'` | 房主/管理员的房间可用性状态；保留 membership 和占席，拒绝新房间请求，并停止暂停期间的小时计费；恢复时从恢复点重新建立一分钟预付后立即可用 |
| `CLOSED` | `deleted_at IS NOT NULL` | 终态，不可恢复；关闭点结清 active membership 的小时费并退还未发生预付，历史和结算继续可查 |

新代码只写 active、paused 和 deleted_at，不再产生 validating、draining、suspended 或 disabled。PAUSED 不属于消费者 membership 生命周期：消费者只有“加入/离开”，没有暂停/恢复席位操作。读取兼容层可把 paused/suspended/validating/draining 等旧的非 active 状态统一展示为“不可用”，但新写和可恢复操作只认物理 `status='paused'`；其他旧状态不能因展示映射直接恢复为 active，必须先在管理事务中确认没有未完成的旧 operation，再规范化为 paused。无需为此预先改写生产存量。

#### 加入、使用、离开

1. 加入、暂停/恢复、离开和关闭都使用固定的 Pixel 短事务，不包住 Provider I/O；加入和 RoomAccount 管理遵守 `listing -> room_accounts/accounts -> API Key -> membership` 的对象锁序。单 membership 的 seat 结算先锁 membership；房间批量暂停、恢复或关闭时先锁 listing，再按 `membership.id` 升序锁其 active membership，汇总本批消费者、账号主和有效邀请人后统一按 `user_id` 升序锁 users 行，随后才改变任何余额。某操作不涉及的对象直接跳过，不另建外层总事务；
2. 加入前先通过同一个 seat billing Repository 对该消费者、Key 和房间相关的到期席位做 catch-up；它只收口旧计费，不创建或激活 membership；
3. 加入事务先锁目标房间，校验 ACTIVE、席位、平台、等级、模型交集、房间行版本和短期 intent；
4. 从可用 RoomAccount 中按 `priority, account_id` 选择第一条作为兼容锚点，并在同一事务持有共享行锁，避免它在 membership 提交前被移除；
5. 再锁定并校验 API Key 所有者和状态，重新检查该 Key 是否已有 active membership；
6. 锁消费者余额，校验 `min_balance_required`。非房主自用且 `hourly_rate>0` 时，同一事务扣除一分钟预付，写余额流水，并创建 `status=active`、`paid_until=joined_at+1 minute`、`billed_until=joined_at` 的 membership；小时价、低消门槛、最低余额和用户选择的 `idle_timeout_minutes` 均写入 membership 快照；
7. 房主自用继续使用全局自用请求倍率，不收小时费、不校验最低余额，也不占消费者席位；积分只参与请求用量扣费，小时预付和最低余额资格只看 `users.balance`；
8. 一个 `api_key_id` 全局最多一条 active membership；同一用户可用不同 Key 加入不同房间，但同一用户在同一房间最多占一个 active 席位；
9. 加入事务提交后 membership 立即生效，下一次请求直接使用该房间；worker 不负责激活，不等待绑定任务，也不存在 queued/pending；
10. 离开事务按固定顺序锁 listing 和 membership，以同一个 `ended_at` 作为小时费截止点，强制结算截止点前的已发生小时费，退还 `ended_at` 之后未发生的一分钟预付，并直接 `active -> ended`。事务提交是对新请求生效的线性化点；任一技术错误或数据库写入失败则整体回滚并保持 active，而“余额不足”属于可预期业务结果，按规则结清后原子结束，不伪装成技术失败；历史 binding 由兼容读取处理，新主链不创建异步 binding；
11. 请求开始被定义为服务端在离开事务提交前成功取得并冻结 membership/房间/实际 RoomAccount 快照。提交前已取得快照的在途请求允许在 membership ended 后完成 `usage_request` 结算；提交后不得再取得新房间快照。迟到请求不得把小时费延长到 `ended_at` 之后；若其用量使截止窗口低消达标，只能通过幂等豁免补偿退款和返冲，不重新打开 membership；
12. 房主/管理员暂停房间时，以事务内唯一 `paused_at` 为截点：按 membership ID 升序结清 active 收费席位并退还未发生预付，仅对 `hourly_rate_snapshot>0` 的 membership 把 `paid_until`、`billed_until` 和 `waiver_window_started_at` 同时置为 `paused_at`，清零低消展示投影；免费或房主自用 membership 的这些计费字段继续为空，所有 membership 仍为 active 并占席。恢复事务必须在 listing 改为 active 前，以同一个 `resumed_at` 为仍有效的收费 membership 扣除新一分钟预付，并置 `billed_until=resumed_at`、`paid_until=resumed_at+1 minute`、`waiver_window_started_at=resumed_at`，新开低消窗口且不追收暂停期间；余额不足者在同一事务结清并直接 ended，其余合格 membership 正常恢复，免费或房主自用席位无需预付。迟到 usage 只能补偿暂停前的旧截止窗口。关闭则按同一截点规则结束全部 active membership 并软删除房间；消费者没有暂停/恢复操作；
13. `idle_timeout_minutes>0` 时，以 `COALESCE(last_request_at,joined_at)+idle_timeout` 为候选空闲 cutoff；idle worker 必须先确认该 membership 没有 active concurrency lease，有在途长流/WebSocket 时跳过并在后续轮次重评，lease 查询失败时 fail-closed、本轮不结束。只有确认无在途请求时，才以该 cutoff 走与主动离开相同的短事务结清、退款并直接 `active -> ended`，不经过 queue/ending operation；值为 0 时不自动退出；
14. 席位满直接返回 `ROOM_FULL`，不创建 queued 记录；Key 已在其他房间时返回 `API_KEY_ALREADY_IN_ROOM`。

为让“立即离开/暂停”在并发下也真实成立，新版只增加一个基于现有行锁的窄 request-admission fence，不增加 ending 状态、operation 或新表：

1. scheduler 选出 membership 和实际账号、并按上游原顺序取得现有 Redis membership/account lease 后，在 Provider Forward 前开启一个不含外部 Provider I/O 的短事务；按 `listing -> account_share_room_accounts(account_id) -> membership` 顺序 `FOR SHARE`，重新确认 listing ACTIVE、membership active、API Key/消费者归属以及既有 `(listing_id,account_id)` RoomAccount 仍可用于本次请求，在锁内冻结不可变请求快照后立即提交；校验失败时释放已取得 lease 并拒绝请求；
2. 离开和 idle 按同一顺序对 listing 取共享锁、对 membership 取排他锁；暂停/关闭对 listing 及相关 membership 取排他锁。它们持有排他 fence 后再确定 cutoff 和复查状态；idle 还须在该短事务内用严格超时复查 Redis active membership lease，查询失败即回滚、稍后重试；
3. 已在 admission fence 内冻结快照的请求允许继续；只取得 Redis lease、但尚未通过 fence 的请求会被状态事务阻塞，并在其提交后重读新状态、释放 lease、拒绝 Forward。因此不需要异步 ending，也不存在“检查为空后又放进一条新请求”的 TOCTOU。

生产只读查重已确认同一用户及同一房间的 active/ending 冲突组均为 0。为落实第 7 条且彻底退出 queue，在单独批准的 migration 中精确删除四个旧唯一索引：`uq_account_share_memberships_live_consumer` 会禁止同一用户用不同 Key 加入不同房间；`uq_account_share_memberships_active_or_queued_listing_consumer` 和 `uq_account_share_memberships_live_listing_consumer` 会让同房间的 queued 历史阻止新 active 加入；`uq_account_share_memberships_queue_rank` 主要保留 `(api_key_id,queue_rank)` 的旧队列排名唯一语义，也随 queue 一并退役。现有 `uq_account_share_memberships_active_listing_consumer` 已提供所需的房间内 active 唯一性，无需新建替代索引。继续保留 `uq_account_share_memberships_active_api_key` 和 `uq_account_share_memberships_live_api_key`，保证整个 Key 只有一个 active/ending 房间。migration 必须核对实际定义后按精确名称执行，定义不符时 fail-fast。

生产现有 8 条 queued membership 不作为新版开放关系，也不占新版席位；它们原地保留为历史。生产 active membership 中没有一个 Key 同时加入多个房间。

#### 房间内账号选择

- membership 只绑定 Key 和房间，不承担长期物理账号粘性；
- 请求时把 `listing_id` 作为附加条件交给上游 scheduler，只从 `account_share_room_accounts.state='active'` 且上游账号本身健康的候选中选择；validating/draining/failed 等旧状态均视为不可接收新请求；
- continuation 和粘性仍遵守上游已有账号拥有关系；
- 现有 `membership.account_id` 因生产非空 CHECK 和 room-account 校验 trigger 保留为兼容锚点；加入时必须按上节规则写入，但运行时不把它当成唯一候选；
- scheduler 成功选择账号后，请求快照必须携带既有 `(listing_id,account_id)` 作为 RoomAccount 身份；现表以 `account_id` 为主键，不新增 `room_account_id` 同义列（若代码局部变量沿用 `roomAccountID`，其值就是 `account_id`）。UsageBilling 锁 membership 后只校验 membership、listing、consumer、API Key 和 owner 等基础归属未被篡改，不再要求本次实际 `account_id` 等于 `membership.account_id` 兼容锚点；请求开始时已冻结的 RoomAccount 事实足以支撑迟到结算，不因 Apply 时 listing 已暂停、RoomAccount 已移除/转 draining、account 后续不可用或 membership 已 ended 而失败；
- 移除被兼容锚点引用的 RoomAccount 时，如果还有其他账号，则在同一房间事务内更新锚点；最后一个账号不能在 active membership 存在时移除，单纯暂停不会结束 membership，必须等待消费者离开或关闭房间并在同一事务结束 membership；
- 房间没有可用账号时明确返回容量错误，不旁路到 PUBLIC_POOL 或 PRIVATE。

#### 请求计费、小时计费与分账

房间倍率不做“老会员永久价”。每次请求开始时读取当前 `room.rate_multiplier`，把它作为上游现有计费函数的本次有效基础倍率，并写入 Usage/Settlement 快照。Key 有 active room 时，它覆盖普通用户/Group 默认倍率和 Pixel Group schedule；Key 不在房间时，才按普通用户倍率与 `EffectiveBaseRateAt` 的既有优先级解析。房间倍率不是在已经得到的 `ActualCost` 上再乘一次；上游既有 Channel 时间价、模型价和原生 peak 仍在原位置计算。

```text
请求消费者费用 = QuantizeUsageBillingAmount(CostBreakdown.ActualCost)
已发生小时费   = hourly_rate_snapshot × billable_duration_ms / 3,600,000
号主收入       = 本类已确认费用 × owner_share_ratio
邀请人收入     = 本类已确认费用 × invite_share_ratio（邀请关系有效时，否则为 0）
平台收入       = 本类已确认费用 - 号主收入 - 邀请人收入
```

请求费用与小时费用是两笔独立事实，不能混成一次 `ActualCost`。同一份“请求消费者费用”必须同时写入 `usage_logs.actual_cost`、余额或订阅用量、API Key 配额/费率统计（仅在上游原谓词要求时）和 request settlement；账号上游配额继续使用上游原有 `TotalCost × AccountRateMultiplier`，不改成消费者价格。小时预付、`seat_charge` 和退款不得写入 `usage_logs.actual_cost`、请求 fingerprint 或 API Key 用量/配额。

请求开始时可稳定取得的 source、membership、listing、account、owner、rate、policy ID/version、配置比例和 request consumer charge 在首次 Normalize 前附到 `UsageBillingCommand` 并进入 request fingerprint；实际 inviter、绑定/到期时间、effective invite ratio 和三方金额只在 `Apply` 事务内解析并写入首次 request settlement 不可变快照，不在网关预查，也不进入请求指纹。修改房间请求倍率只影响修改提交后的新请求；在途请求和重放使用已取得的请求快照及首次结算事实。

小时费采用当前 Pixel 合同：非房主自用 membership 在加入时冻结 `hourly_rate_snapshot` 和 `hourly_fee_waiver_minimum_snapshot`，以一分钟为滚动预付单位；`paid_until` 表示已预付可占席上界，`billed_until` 表示已确认到的截止点、同时也是下一待结算区间的起点，必须满足 `billed_until <= paid_until`，两者不得互代。15 秒 seat worker 负责推进，加入前和请求前 catch-up 是 worker 延迟或故障时的同步兜底，三个入口只能调用同一结算函数。余额不足以续下一分钟时，先收口已预付区间，再直接结束 membership；不能先延长席位再扣款。

一分钟预付只是消费者资金暂扣，不等于账号主/邀请人收入；只有实际经过的时间生成 `seat_charge` 后才按当次不可变政策/邀请快照确认三方收入。离开产生的 `seat_refund` 只退尚未发生的预付，因为这部分从未分给账号主或邀请人，所以不做分润返冲。

低消豁免继续保留当前语义：最多按一小时窗口计算，所需用量为 `hourly_fee_waiver_minimum_snapshot × window_duration / 1 hour`；权威用量是同一 membership 在窗口内重叠的 `usage_request` 消费额，跨窗口请求按重叠时长分摊，membership 上的进度字段只作展示投影。完整窗口结束后保留 15 分钟结算宽限；即时达标时直接退消费者并写 `seat_waiver_refund`，不确认 `seat_charge`；迟到 usage 使既有 `seat_charge` 达标时，由 10 分钟补偿 worker 引用原 charge 快照退款并扣回原账号主/邀请人份额，邀请审计写 `share_reverse`，不得按退款时的新政策或新邀请关系重算。

暂停恢复后，seat 区间拆分、低消权威聚合和展示投影均以当前 `waiver_window_started_at/billed_until` 作为本轮 billing epoch，不再按原 `joined_at` 对齐；`joined_at` 只保留历史加入时间。

PUBLIC_POOL 继续按上游 Group/用户倍率得到同一份 `ActualCost`，不允许号主自定义倍率；PUBLIC_POOL 与 ROOM 共用 `account_share_policies` 的 owner/invite 比例、邀请资格解析和一次三方拆分函数。为避免静默扩大财务口径，请求邀请资格保留当前 Pixel 差异：PUBLIC_POOL 只有 `BalanceCost > 0` 的非订阅计费用量才给邀请人抽成；积分只是该余额计费分支内部的资产抵扣方式，即使本次完全用积分扣除，也不改变已捕获的正 `BalanceCost` 和邀请抽成资格；订阅计费分支的邀请份额归平台。ROOM 请求仍以正的 request `TotalCharge` 判断；ROOM `seat_charge` 以正的已发生小时费判断。该差异必须作为共享 eligibility Helper 的明确输入，不允许各场景复制 SQL 或拆分公式。只有直接邀请关系、邀请人状态和奖励有效期同时有效时才产生邀请人收入；不满足时对应份额归平台。placement 与 room membership 互斥，所以同一请求最多进入一条请求共享结算路径。请求新写和小时新写按 §5.12 的两类 active 事实保存，并通过一个 Repository 统一读取，不搬迁、不重算历史。

### 5.6 OpenCode 原生平台

OpenCode 在产品、账号、Channel、Group、模型目录和房间筛选中使用独立 platform 值，但内部实现保持薄：

- 复用上游 OpenAI 请求、SSE/WebSocket、usage 和错误处理；
- 只增加 OpenCode 模型 ID 规范化、账号凭证适配、endpoint/capability 声明和 Provider Forward；
- 模型列表仍来自 active Channel priced catalog；
- 房间仍遵守同平台、同等级和一个 Key 一个房间；
- 不复制一套 OpenCode Gateway、调度器、计费器或模型映射器。

### 5.7 代理归属和到期回退

首版支持：

- 平台共享代理；
- 用户专属代理；
- 代理到期后沿既有 fallback 链选择兼容备用代理；
- 无兼容备用时账号退出调度；
- 原代理续费后不自动回切。

兼容条件至少包括：代理 active、未过期、归属范围匹配、平台匹配和账号等级兼容。判断作为一个纯函数接入上游 `ResolveProxyFallbackTarget`/代理 Repository 事务，不创建独立 Planner。

上游每源代理的处理顺序和事务保持不变：锁定到期代理及受影响账号、标记过期、为账号选择兼容目标、更新账号代理、提交；提交后再执行已有缓存/通知动作。配置禁止直连时，即使 fallback 链声明 direct 也必须失败关闭。

### 5.8 峰谷定价

上游已经有 Channel `time_pricing` 和 Group `PeakMultiplierAt`，但生产数据实际存放在 Pixel 的规范化表：

- `channel_pricing_time_ranges`：6 条，影响 3 条 Channel pricing，无重叠；
- `group_rate_schedules`：11 条，影响 5 个 Group，无重叠；
- `group_rate_schedule_states`：5 条，对应全部 5 个有 schedule 的 Group；旧 worker 保存“进入时间段前基础倍率”的兼容状态；
- 生产上游 `channel_model_pricing.time_pricing` 有效配置为 0；
- 生产上游 Group peak 字段启用数量为 0。

两类规则不能用同一种接法：Channel 时间段是价格对象的一部分；`group_rate_schedules.rate_multiplier` 按现有表注释和旧执行逻辑表示“切换到的绝对 Group 倍率”，不是额外 peak 因子。把它接入 `PeakMultiplierAt` 会变成 `base × schedule`，既会重复计价，也会漏掉标准型 Group。

因此首版不搬迁这 17 条规则，也不继续运行每分钟改写 `groups.rate_multiplier` 的 worker。采用两个窄适配：

1. Channel Repository 读取 pricing 时同时读取其 time-range 行，并转换成上游 `ChannelTimePricing` 值对象；
2. 后续价格选择仍调用上游现有 `PricingAt`/定价函数；
3. Group Repository 只为存在 schedule 或 legacy state 的 Group 加载小型列表和基础倍率，并随现有 Group/API Key auth cache 一起缓存；
4. 增加一个纯值 Helper `EffectiveBaseRateAt(now)`：命中区间时返回 schedule 的绝对倍率；未命中时返回 `group_rate_schedule_states.base_rate_multiplier`（存在 legacy state 时）或 `groups.rate_multiplier`；
5. 非房间请求把该结果作为上游现有用户/Group 倍率解析函数的 `groupDefault` 入参，继续保持“用户专属倍率优先于 Group 默认”的上游语义；active room 请求直接以房间倍率覆盖这两个普通来源，不再应用 Pixel Group schedule；
6. `Group.PeakMultiplierAt`、`computePeakAwareMultipliers` 及其订阅型 Group 限制完全不改。存在 Pixel schedule 的 Group 禁止同时启用上游原生 peak，管理端直接拒绝冲突配置；
7. 新管理写入只写规范化 schedule，不写 JSONB/peak，也不启动 worker。若该 Group 存在 legacy state，第一次修改基础倍率或 schedule 时在同一管理事务把正确基础值写回 `groups.rate_multiplier` 后删除 state；未编辑的旧数据继续由第 4 条只读兼容；
8. schedule 更新沿用现有 Group/API Key/用户倍率缓存失效入口；所有协议继续在原费率解析位置取值，不因 schedule 新增请求级数据库查询；
9. Group schedule 时间语义固定为系统配置时区内 `[start_minute,end_minute)`，沿用现有 CHECK，不支持跨午夜；Channel time-range 各自沿用其上游值对象语义。两者都增加起止边界测试，不创建新规则引擎。

### 5.9 商城、卡密、福利与兑换中心

商城复用现有 `shop_categories`、`shop_products`、`shop_orders`、`shop_card_keys` 和上游 `payment_orders`：

- 首版一张订单只含一个商品和数量，不做购物车；
- 只新增文本卡密，不新增文件卡密；已有文件历史若存在仍可查询和履约；
- 支付方式为余额和上游已有外部支付，不新增 Provider；
- 商城订单、库存和履约继续使用现有事务与幂等键；支付退款除 §5.10 的跨步骤状态 claim/CAS，以及在原扣款调用点把单次 `balance + ledger` 收口为局部原子操作外，保持上游既有调用顺序和事务边界；
- 已展示卡密不自动退款，进入人工异常处理；
- 兑换中心是统一入口，不创建第二套资产系统：余额、订阅、积分、永久并发和临时并发分别调用现有权益写入或 §5.11 的窄 Helper。

生产现有 1,956 条文本卡密，其中 707 条 available、1,249 条 sold。`shop_card_keys.content` 目前是必填文本列。为兼容现有数据且避免用内容前缀猜格式：

- 给原表增加 nullable `content_encoding`；历史 NULL/空值明确表示 plaintext，新写只允许 `aes-gcm-v1`；
- 新导入行复用上游 `SecretEncryptor` 和已持久化的手工配置密钥，在同一 `content` 列保存密文；密钥未稳定配置时拒绝导入；
- 履约在既有库存锁事务内先锁定卡密，再按 `content_encoding` 解密并校验，之后才标记 sold、写交付快照并提交；解密失败时整个事务回滚；
- 提交后只返回事务内已成功解密的内存结果，不再次查询或猜测格式；
- 管理列表默认不返回明文内容；所有用户交付和管理员查看读取入口必须共用同一 codec Helper；
- 不在首次切换时批量重写旧卡密；后续轮换作为单独安全任务。

福利活动首版只做确定性领取：固定开始/结束时间、资格条件、每用户一次，以及固定余额、订阅、积分、永久并发或临时并发奖励。若旧 `activity_*` 结构只能表达抽奖，则保留其历史只读，新增最小 `benefit_campaigns` 和 `benefit_claims` 两表；领取事务通过唯一 `(campaign_id,user_id)` 防重，在同一事务调用现有权益写入或 §5.11 的奖励 Helper。临时并发必须显式保存数量和有效天数，不能把有效期藏在描述文本或直接改写永久并发。

### 5.10 发票

保留 `invoice_management_enabled` 开关并原地复用：

- `invoice_profiles`；
- `invoice_requests`；
- `invoice_request_items`；
- `invoice_events`。

首版流程保持简单：维护资料 -> 选择可开票来源 -> 提交/取消 -> 后台 BIFF8 批量导出 -> 开具或驳回 -> 写事件。继续使用 `pending / issued / rejected / cancelled` 四状态，不新增 processing/exported/PDF/OSS 状态。

申请保存抬头和来源快照，profile 只作为填写模板；pending/issued 的 item 保持 active，取消或驳回时在同一事务把 item 置为 inactive 并释放来源。用户接口不得返回 `admin_note` 等仅管理员可见字段；历史事件中 `operator_user_id IS NULL` 是允许状态。

币种和来源规则：

- 新申请只允许 `CNY`；
- 新申请首版只开放 `payment_order` 来源；
- 给 `payment_orders` 增加 nullable `currency`，只为新订单在创建时写真实币种；
- 历史订单仅在不可变 provider/order snapshot 能明确证明 CNY 时可用，不能按当前支付配置或默认值猜测；
- 发票金额使用该订单真实支付金额 `pay_amount`，不使用包含赠送权益的 `amount`；`pay_amount<=0` 不可开票；
- `redeem_codes` 当前没有币种字段，因此不再作为新发票来源；
- 生产已有 1 张 CNY 已开具发票，来源为 redeem code，必须原样保留，不重新解释或作废。

新发票只接受 `status='COMPLETED'`、尚无成功/部分/处理中退款事实、且未被其他 active invoice 占用的订单。`PARTIALLY_REFUNDED` 即使仍有剩余实付额也不再允许新开票；这是首版有意采用的严格规则，不实现拆分开票、剩余额开票或红字发票。

上游退款不是一个覆盖“准备、扣减、网关 I/O、完成”的长数据库事务，因此不能假定存在可直接追加的 `FOR UPDATE`。互斥采用最小短事务/CAS：

- 创建发票时在现有发票事务内锁 `payment_orders` 行，重新检查币种、`COMPLETED`、退款事实和 active source 后再插入 request/item；
- 用户 `RequestRefund` 的条件状态更新改为一个短事务：锁同一订单、确认没有 active invoice、按上游原条件写 `REFUND_REQUESTED` 后立即提交；
- 管理员 `ExecuteRefund` 用同样的短事务/CAS 把允许状态 claim 为 `REFUNDING` 并再次检查 active invoice；提交后才继续上游既有余额/订阅扣减和网关调用顺序；
- pending refund finalize 在其现有短事务内增加同一 active invoice 检查；
- 外部支付网关 I/O 永远不放进数据库事务；claim 失败直接返回冲突，不创建新的跨域状态机或消息队列；
- 生产当前没有 active payment invoice，也没有“已开票来源正在退款”的冲突。

这是本文对“不改变上游事务边界”的唯一跨步骤、状态编排层窄例外：只改变状态 claim/CAS 的原子校验边界，不改变退款准备、余额/订阅扣减、Provider 调用、pending finalize 的既有先后关系，也不把它们合并成一笔事务。各单次余额 mutation 在原调用点增加 `user_balance_ledger` 的局部原子收口遵守 P03，只保证这一次余额变化与流水同成同败，不属于跨步骤事务例外。若后续要求绝对禁止 claim/CAS 短事务，则必须同时取消“并发下可靠阻止同一订单既开票又退款”的能力，不能用非原子先查后写伪装满足。

### 5.11 积分、统一奖励与临时并发

积分沿用现有轻量资产模型，不变成现金或第二套钱包：

- `users.points_balance` 是积分余额，`points_ledger` 是不可变审计流水；积分永久有效、不可提现、不可兑换现金；
- `users.prefer_points_billing` 保留“请求计费优先扣积分，不足部分再扣余额”的现有语义；拆分仍发生在 UsageBilling 原余额扣费位置，不移动请求阶段；
- 上游 0.2.0 的请求前计费资格本来只认识订阅/余额，因此必须在每个 Handler 已有的 `CheckBillingEligibility` 调用内部共享同一个 `CheckBalanceOrEnabledPoints` 谓词：订阅模式判定和原检查顺序保持不变，只有落入原 balance 分支时，`prefer_points_billing=true && points_balance>0` 才可替代余额资格。否则“余额为 0、积分可用”的请求会在实际扣积分前被提前拒绝；
- API Key/JWT 的用户认证投影必须携带 `points_balance/prefer_points_billing`；积分增减或偏好修改提交后沿用现有用户级 auth/billing cache 失效入口，避免已耗尽积分仍被旧快照长期判定为有资格。缓存失效只能在提交后执行，不挪进资金事务；
- 商城积分支付、兑换、福利领取和后台调整继续可用，但所有积分增减只调用一个接收当前 `tx` 的 `ApplyPointsDelta` 窄 Helper；
- Helper 负责锁定用户、禁止负余额、写 before/after、direction、reason、`ref_type/ref_id`，并用稳定业务引用保证幂等；调用方负责自己的资格和事务，不在 Helper 内再次开启事务；
- 历史流水和余额不回填、不重算；新写必须在发起业务的同一事务中同时改变余额与写流水。

福利与兑换只增加一个小型奖励分派函数，类型固定为 `balance / subscription / points / concurrency / temporary_concurrency`。它只负责把已校验的奖励送到既有写入函数，不建立通用奖励引擎、规则图或消息总线。永久 `concurrency` 继续更新 `users.concurrency`；临时并发不得更新该字段。

临时并发使用一张最小增量表 `user_concurrency_grants`：

| 字段 | 语义 |
|---|---|
| `id` | `BIGSERIAL` 主键 |
| `user_id` | 获得奖励的用户 |
| `bonus_concurrency` | 正整数临时增量 |
| `starts_at / expires_at` | 半开区间 `[starts_at, expires_at)`，结束必须晚于开始 |
| `source_type / source_id` | `benefit_claim`、`redeem_code` 等稳定来源；与 user 组成唯一键防止重复发放 |
| `created_at` | 审计时间；不增加可漂移的 status 字段 |

除时间与主键外的业务字段均为 NOT NULL；`bonus_concurrency > 0`、`expires_at > starts_at`，并以 `UNIQUE(user_id, source_type, source_id)` 保证奖励幂等。相同来源重试且 payload 一致时返回既有 grant；数量或时间不同则 fail-fast，不能静默覆盖已发权益。增加面向认证查询的 `(user_id, starts_at, expires_at)` 索引即可，不为到期扫描创建索引或 worker。

运行时只使用：

```text
effective_concurrency
  = users.concurrency
  + SUM(当前时刻有效 grant 的 bonus_concurrency)
```

该值在 API Key/JWT 认证投影形成、写入既有 `AuthSubject.Concurrency` 之前计算一次；API Key auth cache 只在 miss/重建时查询有效 grant，把 effective value 和下一次时间边界写入快照，命中期间不让各 Handler 自行查库。各 Handler 继续在自己的原阶段读取同一个字段，不修改网关调用顺序。创建 grant 后只做提交后用户级认证缓存失效；缓存有效期不得跨越下一次 `starts_at` 或 `expires_at` 边界。生效和过期都由时间条件自然完成，不需要 worker、到期任务、回写永久并发或批量改状态。多个有效 grant 直接相加；数据库求和使用足够宽的整数类型，转换越界时 fail-fast。

### 5.12 邀请关系与消费抽成返利

本节以当前 Pixel 已运行的共享消费抽成链路为产品基准，不采用上游 `0.2.0` 的充值返利模块，也不整体搬运旧 `AffiliateService`。首版只有一种邀请收益来源：被邀请用户使用 PUBLIC_POOL 或 ROOM 共享账号产生请求费用，或在 ROOM 产生已确认席位小时费后，其有效直接邀请人按政策比例获得消费抽成。充值订单、支付履约和订阅购买都不产生邀请返利。

现有邀请关系能力继续保留，但在上游主干上按单一职责重接，不复制旧大型 Service：

- `user_affiliates` 保存邀请码、自定义邀请码、每周生成额度/周期、邀请码到期与自动轮换、直接 `inviter_id`、绑定时间/来源和 `invite_reward_expires_at`；
- `affiliate_enabled` 继续作为邀请绑定与消费抽成总开关；关闭时不接受新邀请码绑定，也不产生新的邀请人抽成，但既有关系和历史仍可只读查询；
- 所有邮箱/OAuth 等用户注册提交的邀请码都调用同一个 `ConsumeAndBindInvitation` 事务方法，统一校验开关、格式、周期、到期、周额度和既有绑定，再写入一个直接邀请人；邀请码是否必填只决定“空值能否注册”，只要提交了邀请码就执行同一套额度规则；禁止注册旁路、自邀请和覆盖既有有效绑定；
- 管理员关系维护保留独立的 `AdminBindInviterByUserID` 短事务，按用户 ID 补绑或改绑并可按当前权限选择重置收益有效期，不消费邀请码或周额度；它与用户注册是不同用例，只共享自邀请、用户存在性和审计等底层不变量；首版不新增登录用户自助补绑 API；
- `aff_code_expires_at` 只决定邀请码还能否用于新绑定，`invite_reward_expires_at` 决定已绑定关系还能否参与后续消费抽成，两者不得混用；
- 邀请人状态、绑定时间和奖励有效期在请求或小时费用首次确认时通过同一个 resolver 判定；业务页面、活动入口、网关请求结算和 seat 结算不得各写一套资格规则；
- 首版比例只来自 `account_share_policies.invite_share_ratio`，不启用上游按用户配置的充值返利比例、金额上限或冻结期限。

接口层保留当前用户详情/摘要、注册前邀请码校验，以及后台关系查询、补绑和延长消费收益有效期；不新增用户自助补绑接口，不注册 `POST /user/aff/transfer`，不把个人充值返利比例、冻结期和额度批量管理接口纳入新版 API。数据库中的旧字段可以为了原库兼容暂时存在，但不得出现在新 DTO 的当前消费抽成字段中；展示层统一使用 `effective_commission_rate_percent / period_commission / total_commission` 等明确语义，避免旧列名继续泄漏到页面。

PUBLIC_POOL/ROOM 请求费用在 UsageBilling 原事务尾部拆成账号主、有效直接邀请人和平台三份；ROOM `seat_charge` 在独立小时结算事务内用同一个 resolver/split 拆成三份。邀请资格、政策版本和三方金额均写入本次不可变结算快照。“抽成”的基数是消费者本次共享账号请求费用或已发生小时费，不是充值金额，也不是账号的上游成本。请求资格保持当前口径：PUBLIC_POOL 必须处于正 `BalanceCost` 的非订阅计费分支，积分抵扣不取消该资格；ROOM 必须有正 request `TotalCharge`；小时结算必须有正 `seat_charge`。不借重建自动扩大到 PUBLIC_POOL 的订阅计费分支。

消费分润公式固定为：

```text
owner_credit    = consumer_charge × owner_share_ratio
invite_credit   = consumer_charge × invite_share_ratio（直接邀请关系有效时，否则为 0）
platform_credit（业务名） = consumer_charge - owner_credit - invite_credit
```

`owner_share_ratio + invite_share_ratio <= 1`；邀请人不存在、被禁用、尚未绑定、绑定发生在本次费用确认之后或已超过 `invite_reward_expires_at` 时，`invite_credit=0`，未分配部分归平台。账号主和邀请人收入直接进入现有 `users.balance`，并在产生该收入的 request 或 seat 事务内写 `user_balance_ledger`；邀请人的消费抽成同时写 `user_affiliate_ledger action='share_accrue'` 作为兼容审计投影。

消费抽成不得先进入 `aff_quota` 或 `aff_frozen_quota`，不得等待成熟，也不得再执行一次“转余额”。`user_affiliate_ledger` 不参与余额计算或收入二次汇总；只有 request settlement 首次成功插入，或 `seat_charge` 首次按新计费区间插入时，才能在同一事务增加邀请人余额并各写一次 `user_balance_ledger` 和 `share_accrue`。相同请求重放、相同席位区间 catch-up 或 worker 重试只能返回/推进既有事实，不得重复入账。

ROOM 席位小时费继续纳入分账。即时低消豁免不产生可分配 `seat_charge`；迟到 usage 导致已确认小时费转为豁免时，`seat_waiver_refund` 必须引用原 `seat_charge`：settlement 按原快照保存被冲回的 10 位 owner/inviter/platform economic credit。为避免把 10 位经济值误当成 8 位钱包事实，只在现有 `account_share_mode_settlement_entries` 增加 nullable signed `owner_wallet_delta NUMERIC(20,8)` 与 `invite_wallet_delta NUMERIC(20,8)`：新 `seat_charge` 保存本次各角色实际正向 delta，迟到 `seat_waiver_refund` 保存各角色实际负向返冲 delta，普通 `seat_refund` 和没有原正向分润的即时 `seat_waiver_refund` 均保存 `0/0`；缺席或未发生收入的角色也保存 0。只有切换前历史行保留 `NULL/NULL`，NULL 表示没有钱包快照而不是零。迟到返冲优先精确反向原 charge 的非空钱包快照；legacy NULL 行只走 §5.14 的窄兼容解析器，不回填历史、不等待旧窗口排空，也不从旧流水 amount 猜金额。邀请人审计仍写 `share_reverse`。统一读取适配器按 settlement type 将正额 refund 经济快照解释为负数；不能按退款时的政策或邀请关系重算，也不能把 `share_reverse` 误当作上游充值返利。`seat_refund` 只退消费者尚未发生的预付，不参与分润逆向。

公式中的 `platform_credit` 是业务名称；request `account_share_settlement_entries` 继续写现有物理列 `platform_fee`，不新增同义 `platform_credit`。seat `account_share_mode_settlement_entries` 继续使用其现有 `platform_credit` 物理列；统一读取适配器将它别名为 `platform_fee` 后映射到同一个领域字段，避免在 API/页面再出现两个同义概念。

新写只保留一套政策、邀请资格和拆分实现，但尊重请求与小时费的两个既有事务边界：

- `account_share_policies` 是新写政策事实源，PUBLIC_POOL/ROOM 共用一个 policy resolver 和拆分函数；旧 `account_share_mode_policies` 只读；
- PUBLIC_POOL 请求、ROOM 请求和 ROOM 小时费共用同一个 `resolve policy -> resolve inviter eligibility -> split credits` 纯逻辑；request writer 与 seat writer 只负责各自物理字段和原子余额/流水落账，禁止各自复制比例、舍入和资格 SQL；
- `account_share_settlement_entries` 作为 PUBLIC_POOL/ROOM 新请求结算事实；复用现有 `share_mode_snapshot` 区分来源，并为旧 public 值建立只读映射；平台份额继续写现有 `platform_fee`。为 ROOM 新写按需增加 nullable 的 `listing_id`、`membership_id`、`duration_ms`、`period_started_at`、`period_ended_at`、房间倍率和缺失的政策/邀请快照字段；旧行允许为空并按现有 `usage_log_id` 回退，但新的 ROOM request 行这些房间/时间字段不得为空，`consumer_charge` 是低消用量金额。不得新建 `platform_credit` 或第三张结算表；
- `account_share_mode_settlement_entries` 继续作为 ROOM 小时结算事实，新写 `seat_charge / seat_refund / seat_waiver_refund`；只增加 nullable signed `owner_wallet_delta NUMERIC(20,8)`、`invite_wallet_delta NUMERIC(20,8)`。新 charge 写各角色实际正向值，迟到 waiver 写实际负向值，普通 refund、即时 waiver 和缺席角色写 0；仅切换前历史行保持 NULL 且不回填。其既有 `usage_request` 历史原地保留但不再新写。小时豁免用量 Repository 同时读取 `account_share_settlement_entries` 中的 ROOM request 新写和旧 mode 表的 `usage_request`，在一个位置按业务引用去重；
- 查询只在一个 Repository 适配器中组合 `account_share_settlement_entries`（旧 PUBLIC_POOL 历史及新 PUBLIC_POOL/ROOM request）与 `account_share_mode_settlement_entries`（旧 ROOM request 及新旧 seat）。适配器必须保留 `(source_table,id,settlement_type)`，只对同一请求业务引用检测跨表重复；`seat_charge` 和对应的 refund/reversal 是不同业务事实，不能被当作重复删除；不搬迁、不合并 ID、不重放分润；
- `/affiliate` 的期间/累计消费与抽成统计统一查询上述适配器的有符号 `invite_credit`：request/seat charge 为正，waiver reversal 为负，`seat_refund` 为零；`share_accrue/share_reverse` 只用于审计核对，不得继续用 `aff_history_quota` 作为累计抽成金额。

生产现有邀请关系只有一个 `inviter_id`，没有代际、团队或上级链快照，因此首版所谓“复杂分销”只表示同一次共享消费按账号主、一级邀请人和平台三方拆分，不实现二级、三级或无限级分佣。若以后明确需要多级代理，那是独立产品与财务项目，必须另行定义层级、资格、比例、退款逆向和历史快照，不能借本次重建猜测实现。

### 5.13 提现

提现复用现有 `user_withdrawal_requests`、`user_receipt_codes`、前端入口和后台处理流程。首版冻结当前资产口径：只从 `users.balance` 提现，不允许积分提现，也不新增“可提现余额/充值余额/收益余额”子账户。也就是说，现有总余额可申请提现的行为继续保留；若以后要限制只有账号主或邀请人的消费抽成收益可提现，必须先建立资金来源口径并作为单独迁移项目，不能在本轮按历史流水猜余额来源。

状态只保留 `PENDING / SETTLED / CANCELLED / REJECTED`：

- 提交沿用 `锁 users -> 校验一条 PENDING/频次/收款码/余额 -> 扣 total_deducted -> 插入申请 -> 插入余额 debit 流水 -> commit`；
- `total_deducted = amount + fee_amount`，金额、费率和收款信息写申请快照；每个用户最多一条 PENDING；
- 用户取消或管理员驳回沿用 `锁申请 -> 返还 total_deducted -> 更新终态 -> 插入余额 release 流水 -> commit`，同一申请只能释放一次；
- 管理员确认到账只把 PENDING 改为 SETTLED，不再扣余额；人工转账或未来外部 Provider I/O 必须在数据库事务外，首版不接自动打款 Provider；
- 新余额流水统一使用 `ref_type='withdrawal_request'`、`ref_id=申请 ID`；提交与释放分别使用固定 `withdrawal_submit/withdrawal_release` action slot 和不同 reason，复用申请状态迁移作为业务 claim，不能只靠方向或 reason 防重复；
- 生产历史提现没有对应流水时保持原样，不伪造回填；新版本切换后的每一笔扣除和释放必须可对账。

### 5.14 余额、结算和财务后台

本轮不再创建 double-entry Ledger，也不做 opening transaction。继续沿用：

- `users.balance` 是运行时现金余额事实，`users.points_balance` 是独立的非现金积分事实；
- `UsageBillingRepository.Apply` 是请求扣费与请求分润事务入口；seat billing Repository 是小时预付、收入确认、退款和豁免返冲的独立短事务入口；
- 现有 `user_balance_ledger`、`points_ledger`、`user_affiliate_ledger`、商城流水、提现申请和 share settlement 是审计/业务记录，不是第二份可写余额；
- 支付、退款、订阅、提现和人工调整继续走各自现有 Service/Repository；支付履约明确不挂接上游 affiliate 充值返利；
- 财务后台基于现有事实表构建只读查询，不建立第二套资金事实。

使用记录中的余额流水按当前 Pixel 能力整体保留：

1. 用户端继续提供认证且受查询限流保护的 `GET /usage/balance-ledger` 和 `GET /usage/balance-ledger/stats`；服务端强制从认证主体注入 `user_id`，忽略任何跨用户意图，不能因复用管理 DTO 让用户查询或依赖他人的用户摘要。管理端继续提供 `/admin/usage/balance-ledger` 及 `/stats`，复用同一个查询实现但保持独立管理员权限；
2. 列表 DTO 保留 `id/user_id/user?/direction/amount/reason/ref_type/ref_id/balance_after/metadata/created_at`；因兼容现有共用 DTO，用户和管理接口都可附带 `id/email/username/status` 的 optional user 摘要，用户页不依赖该字段且仍只能得到自己的行，管理页可展示。`amount` 和 `balance_after` 继续序列化为十进制定点字符串，前端不得先转 JavaScript 浮点再格式化；为兼容历史仍接受并展示 `amount>=0`，显示正负只由 `direction=credit/debit` 决定，但重建后的新 writer 不产生零金额行；
3. 列表与统计共用完全相同的 `user_id/direction/reason/ref_type/ref_id/start_date/end_date/start_time/end_time/timezone` 过滤器；用户页默认浏览器本地今天及前 6 个自然日，管理页保留当前最近 24 小时默认值。reason、ref_type 和 ref_id 精确匹配，精确 start/end 成对出现时优先于 date；时间统一为 `[start_time,end_time)`。自然日结束值使用“次日 00:00:00”作为排他上界，不再使用会遗漏最后一秒及小数秒的 `23:59:59`；精确时间输入也明确 end-exclusive；
4. 用户列表保留精确总数分页、持久化 page size 以及 `(created_at,id)` 稳定升降序；管理端允许沿用现有 `exact_total=false` 快速分页。统计返回总笔数、credit/debit 笔数、入账合计、扣费合计和 `credit-debit` 净变动，页面现有四张卡继续显示流水笔数、入账、扣费和净变动；
5. reason 展示必须同时保留本地化名称和原始码；用户端与管理端共用一份无业务写入能力的 `(reason,direction)` presentation 配置，覆盖请求扣费、小时预付/普通退款/低消退款、账号主收入、邀请收入及邀请豁免返冲、兑换、提现、管理员调整等首版现金原因。历史 `account_share_mode_seat_waiver_refund + credit` 显示为消费者低消退款，`account_share_mode_seat_waiver_refund + debit` 显示为账号主低消返冲，不依赖 metadata 中一定存在角色字段；重建后的 owner 新写使用专用 reason。其他历史或未知 reason 永远回退显示原码及 `ref_type/ref_id`，不得因 Ideas/随机活动等历史 reason 重新恢复已删除模块；
6. metadata 保留当前 object 响应合同，但页面不直接 dump JSON，只按 `(reason,direction)` 的展示白名单读取 request/API Key/account/group、小时价、时长、计费区间、membership、低消门槛/用量、提现申请、已消费兑换记录和管理调整说明等现有可读字段；所有未来 writer 严禁向 metadata 写入 API Key 明文、账号凭证、未消费兑换码、商城卡密正文、银行资料或发票敏感内容；列名统一为“流水说明”，不再误称只适用于 debit 的“扣费备注”；
7. `users.balance` 始终是当前现金余额事实，`user_balance_ledger` 是不可变审计流水。列表和统计只查询该表，不用流水重算或覆盖余额，也不从 `points_ledger`、share settlement、`user_affiliate_ledger`、`shop_balance_ledger` 或 withdrawal request 在前端拼账；
8. Phase 0 必须列出首版所有 `users.balance` 变更调用点。重建后每条保留的现金变更路径都在原余额事务内追加一条带稳定 `reason/ref_type/ref_id` 和 `balance_after` 的 `user_balance_ledger`；原来只有一条 autocommit 余额语句的路径，按 P03 在原调用点收口为一次局部原子 mutation。任一步失败都不得留下“余额已变、流水未写”或相反的半事实，幂等重放也不得重复写流水。现有商城或提现等历史缺口不跨表伪造回填，旧记录按实际已有流水展示；
9. 余额流水页首版不新增 CSV 导出，不新增第二套查询 SQL，也不把管理端用户筛选能力暴露给普通用户。

本节是余额流水的规范性正文，后续 Phase、验收和风险表只引用本节，不另造口径。现金金额与流水事件必须满足以下合同：

- `user_balance_ledger.amount` 永远等于本事务中对应一次 `users.balance` 实际变化量的绝对值，不等于页面展示的请求总费用，也不等于 settlement 的经济费用；`balance_after` 是该次变化完成后的余额。请求被积分部分抵扣时，只为实际 `balance_deducted>0` 的部分写现金 debit，积分部分只写 `points_ledger`；全积分或订阅且现金变化为 0 时不写消费者现金流水；
- 请求的 `consumer_charge` 仍是 Usage、配额和共享结算使用的唯一量化经济费用。账号主、邀请人的 settlement 保存 10 位经济 credit，余额流水只写它实际形成的 8 位 wallet delta；因此“请求经济费用、现金扣款、积分扣款、订阅消耗、共享收入”是不同指标，不能因业务引用相同而相互替代；
- 保留生产现状：`users.balance` 为 `DECIMAL(20,8)`，`user_balance_ledger.amount/balance_after` 为 `DECIMAL(20,10)`，不为统一外观修改余额列精度。定价、Usage 和 settlement 经济值继续由统一 decimal 函数量化到 10 位；owner/inviter economic credit 各自按 10 位计算，platform 仍取“10 位 consumer charge - 10 位 owner - 10 位 inviter”的精确余数。每个未来现金 writer 必须在原子余额语句之前或之内形成唯一的有符号 `cash_delta_8`：加减型操作显式使用与 PostgreSQL `NUMERIC(20,8)` 一致的 decimal 8 位量化值，set/clamp 型操作则从同一语句锁定的 before/after 或 `RETURNING` 结果取得差额。余额更新和流水必须共同使用该 `cash_delta_8`；流水 `amount=abs(cash_delta_8)`，`balance_after` 取数据库结果，并以 10 位定点字符串承载。任何未来 writer 得到 `cash_delta_8=0` 时都不插入 `user_balance_ledger`，业务 claim/no-op 结果由订单、settlement、提现申请等原领域事实保存；历史零金额行仍原样兼容。只有重建后的非零新写保证末两位为 0，历史 10 位流水原样兼容。普通 `seat_refund` 从未使用预付时长计算退款后形成 `cash_delta_8`，即时低消豁免从本次 waiver refund amount 形成，支付退款从实际 `BalanceToDeduct` 形成。财务对账分别验证 10 位 settlement 恒等式，以及每条未来现金流水的 `cash_delta_8 = balance_after - balance_before`；精度差额不改写成第二条用户流水。SQL 直接在 numeric 上求和，列表和 stats 的全部金额均返回十进制定点字符串，只有笔数返回整数；
- ROOM seat 新写还必须在同一 settlement 事务保存该角色实际使用的 `owner_wallet_delta`、`invite_wallet_delta`。类型矩阵固定为：`seat_charge=实际正向值/缺席角色 0`，迟到 `seat_waiver_refund=实际负向值/未发生角色 0`，即时 `seat_waiver_refund=0/0`，普通 `seat_refund=0/0`；即使零 delta 不产生余额流水，settlement 快照仍写 0，不能用 NULL 代替，旧 `usage_request` 不再新写。保持现有“先插 settlement，再按 owner、inviter 顺序改余额和写审计”的调用顺序；每个计费窗口都在该 settlement 的 owner、inviter 写入完成后，以本次 `settlement_id` 做一次窄更新补齐两个实际 delta，任一步失败仍整体回滚，提交后的每个新 seat 行两列都不得为 NULL。实际 delta 必须在数据库 numeric 中取得并以 decimal/定点字符串承载，不得用 float64 前后相减。原 charge 快照非空时直接返冲，不读取 ledger amount。对快照为 NULL 的 legacy 正向 charge，兼容解析器只读取原 settlement 中不可变的 10 位正向 credit，并逐 settlement、逐角色量化，绝不能先 SUM 再 CAST：令 `q=0.00000001`，非精确半舍入点使用 `CAST(credit AS NUMERIC(20,8))`；当 `MOD(ABS(credit) * 10000000000, 100) = 50` 时，必须关联该角色唯一的原正向余额流水，仅用其 `balance_after` 符号恢复当时 PostgreSQL 赋值结果——`balance_after >= 0` 仍取 CAST 值，`balance_after < 0` 取 CAST 值减 `q`。Phase 0 建立并首轮运行覆盖全部 legacy 候选的只读门禁，核实原 writer 来源、生产余额列 typmod、无仓库外余额 trigger 及半舍入点正向流水唯一性；发布时还必须按 Phase 8 在旧实例停写后刷新该门禁。任一证据不成立，就阻断 `ROOM seat billing + waiver compensation` 整体切换，不允许只启动新 charge writer，也不另增毒性行隔离或改变 worker 的遇错终止语义。不得猜测、不得退化为 10 位 ledger amount，也不得批量回填历史 charge；门禁通过后，解析所得负向实际 delta 只写入本次新 reversal settlement 与余额流水；
- 每个首版新写现金事件都必须有非空 canonical `ref_type/ref_id`，逻辑幂等身份固定为 `(ref_type,ref_id,action_slot,role_slot)`。`action_slot` 由固定 writer 定义，用于区分同一对象上的合法相反动作，例如 `withdrawal_submit/withdrawal_release`、`shop_pay/shop_refund`；多角色事件再以 `consumer/owner/inviter` role slot 区分，单角色事件使用固定 role。action slot 必须由现有领域状态 claim、唯一业务行或 canonical ref 构造持久化表达，不能只从 direction/reason 推导，也不要求为此给 ledger 新增通用列。现有部分唯一键 `(user_id,direction,reason,ref_type,ref_id)` 只作为重复插入防线，不是可由 payload 改写的业务身份。`direction/reason/amount` 和关键快照均是该 canonical action 的不可变 payload：同一 action 同 payload 重放返回既有事实，不再改余额；任一 payload 改变都必须 fail-fast，不能靠换 direction 或 reason 绕过幂等。业务 settlement/order/claim 应在余额变化前取得；若只能同语句完成，则流水唯一冲突必须使该次余额变化一起回滚。禁止先更新余额再用 `ON CONFLICT DO NOTHING` 静默吞掉流水冲突；
- 同一自然人兼任 consumer、owner 或 inviter 时不按用户净额合并业务事件。先按各原事务既有规则去重并锁定用户行，再按 Phase 0 冻结的原调用顺序逐 `role_slot` 改变余额并立即写对应 reason 流水；每一行 `balance_after` 承接上一笔实际变化。新写的 consumer 低消退款、owner 迟到返冲和 inviter 迟到返冲分别使用独立 reason，且仍以同一 settlement ID 加固定 role slot 标识各自 canonical action；不能因用户 ID 相同丢失其中一笔，也不能为了统一而改动上游已验证的调用顺序；
- `created_at` 是数据库为该流水记录的事务时间，不承诺等于 wall-clock commit 时刻；请求发生时间、小时计费区间、退款原事实时间等通过安全业务引用或允许的 metadata 字段表达，不能把迟到补偿的入账时间冒充原消费时间；
- 业务代码对 `user_balance_ledger` 只有 INSERT 权限语义，不提供更新或删除旧行的 Service/Repository API。历史行原样保留，不能为了统一 reason、metadata 或展示文案改写旧记录。

Phase 0 必须先输出一份“现金流水事件矩阵”，Phase 2 将其固化为后端常量和前端只读展示配置，Phase 4/6 只能消费该矩阵。矩阵逐事件冻结 `action_slot、role_slot、direction、实际 cash delta 来源、reason、ref_type、ref_id 构造、允许的 metadata 字段、业务 claim/唯一键、零 delta 结果、重放结果、原事务与原写入顺序`。已存在或需要最小纠偏的新写基线至少包括：`usage_charge`、`private_group_commission`、`account_share_income`、`account_share_mode_income`、`invite_share_income`、`account_share_mode_seat_prepay`、`account_share_mode_seat_refund`、`account_share_mode_seat_waiver_refund`、`account_share_mode_owner_waiver_refund`、`account_share_mode_invite_waiver_refund`、`redeem_code` 和 `admin_adjustment`；其中新增的 owner 专用 reason 只用于重建后的迟到返冲，生产历史中 owner 与 consumer 共用的旧 reason 原样保留并按 direction 兼容展示。确定性福利、支付/退款、商城余额支付/退款、提现提交/释放等缺口只在核对实际调用点和生产已有 reason 后命名，不得在实现中临时复用含义不同的旧码。Ideas 打赏、随机抽奖等退役模块的 reason 只作为未知历史值兼容展示，不进入新写矩阵。

下表中的 reason/ref/角色和原调用顺序以当前代码事实为核对起点；“实际 wallet delta”是重建 writer 必须达到的目标，不表示当前 writer 已经全部正确记录了数据库 8 位差额。它仍须由 Phase 0 的生产 reason/ref 聚合与调用点审查确认：

| reason | direction 与实际现金 amount | 当前 ref / 幂等事实 |
|---|---|---|
| `usage_charge` | debit；非订阅请求中实际 `balance_deducted`，全积分时不写 | `usage_log / usage_log_id`；请求真正幂等来自既有 `request_id + api_key_id + fingerprint` 先行 claim，新写不得再走 ref_id 为空的 legacy 路径 |
| `private_group_commission` | debit；订阅私有分组实际佣金现金扣款，不是订阅消费额 | `usage_log / usage_log_id`，保持原订阅更新后扣佣金的顺序 |
| `account_share_income` | credit；PUBLIC_POOL request 的账号主实际 wallet delta | `usage_log / usage_log_id`，request settlement 首次成功后写 |
| `account_share_mode_income` | credit；ROOM request 或 seat charge 的账号主实际 wallet delta | request 使用 `usage_log`，seat 使用 `account_share_mode_settlement / settlement_id` |
| `invite_share_income` | credit；request 或 seat 的邀请人实际 wallet delta | request 使用 `usage_log`，seat 使用 settlement ID；同事务另写 `share_accrue` 审计但不重复计收入 |
| `account_share_mode_seat_prepay` | debit；加入或续费的实际预付 wallet delta | renew 优先引用 settlement ID；join 暂沿用 membership 与 paid_until 派生引用，Phase 0 必须核对碰撞/重放后再固化 |
| `account_share_mode_seat_refund` | credit；未发生预付的实际返还 | `account_share_mode_settlement / refund settlement_id` |
| `account_share_mode_seat_waiver_refund` | consumer credit；即时或迟到补偿实际形成的退款 `cash_delta_8` | `account_share_mode_settlement / waiver refund settlement_id + consumer role_slot`；保留当前 consumer reason，也兼容历史上 owner debit 共用此 reason 的旧行 |
| `account_share_mode_owner_waiver_refund` | owner debit；精确反向原 charge 的 `owner_wallet_delta`，legacy NULL 时使用上文窄解析器得到实际 delta | `account_share_mode_settlement / waiver refund settlement_id + owner role_slot`；这是消除当前 consumer/owner reason 混用的最小新 reason，legacy 证据门禁不通过时阻断 ROOM seat billing 与 waiver compensation 整体切换 |
| `account_share_mode_invite_waiver_refund` | inviter debit；精确反向原 charge 的 `invite_wallet_delta`，legacy NULL 时使用上文窄解析器得到实际 delta | `account_share_mode_settlement / waiver refund settlement_id + inviter role_slot`，owner 返冲后再写；legacy 证据门禁不通过时阻断 ROOM seat billing 与 waiver compensation 整体切换 |
| `redeem_code` | credit 或兼容历史 debit；以 clamp 后实际 wallet delta 为准 | `redeem_code / code_id`；兑换业务先完成既有使用 claim，保留当前已消费 code 与引用展示合同，Phase 0 核对其数据安全性 |
| `admin_adjustment` | credit 或 debit；set/add/subtract 后实际 wallet delta | `redeem_code / 管理调整记录 ID`；保留当前 operation/notes/code 展示合同，Phase 0 抽样自由文本，不能把新的内部敏感备注写给用户 |

支付充值余额当前通过 balance redeem code 履约，已经产生 `redeem_code` 流水，禁止再补一条 `payment_recharge`。支付退款则保留“Provider I/O 在事务外”和当前调用顺序，只把 Provider 成功后的原 `DeductBalance` 位置替换为一个窄原子 Repository 操作：按 `payment_order_id + payment_refund action_slot + consumer role_slot` 判定幂等，用实际 `BalanceToDeduct` 形成唯一 `cash_delta_8`，改变余额并插入实际 debit 流水，再继续原 `markRefundOk`；单条 PostgreSQL 原子 SQL 优先，现有 Repository 无法表达时才使用只覆盖这一次扣款与流水的局部短事务。重试看到同一 payload 的既有事实直接复用，payload 改变则报冲突，不得二次扣款。该局部操作不包住 Provider I/O、不与 `markRefundOk` 合并、不移动退款准备或终态化阶段，也不扩成新状态机或新的跨步骤事务。

提现提交、取消/驳回，以及商城余额支付/退款或余额奖励，均只在各自已经存在的业务事务内追加通用余额流水；提现申请和 `shop_balance_ledger` 继续是领域事实，财务不得与通用流水重复相加。同一提现申请分别使用 `withdrawal_submit/withdrawal_release` action slot，同一商城订单分别使用 `shop_pay/shop_refund` action slot；各 action 复用原领域状态 claim，不新增通用状态表。没有现成事务的单次余额写优先收口成“幂等判定 + 余额变化 + 流水”的单条原子 SQL；现有 Repository 确实无法表达时，允许按 P03 使用只覆盖该单次 mutation 的局部短事务。两种方式都不能扩成跨步骤事务、包住外部 I/O 或重排上游业务步骤。

对外查询继续沿用现有 HTTP 形状，最小合同如下：

| 接口 | 参数与默认值 | 响应和权限 |
|---|---|---|
| `GET /usage/balance-ledger` | `page=1`、`page_size=20` 且上限 1000，并兼容 `limit`；只要请求中提供非空 `page_size` 就优先采用它，否则才读取 `limit`，所选值不是正整数或超过 1000 时沿用当前行为回退 20，不 clamp、也不改成 400；非法或非正 `page` 回退 1。`direction/reason/ref_type/ref_id` 精确过滤；只有 `sort_order`，asc/desc 之外回退 desc；日期或精确时间按下述时区合同传入 | 原始 HTTP 为 `code/message/data` envelope，`data` 内保持 `items/total/page/page_size/pages` 且无 `has_more/exact_total` 新字段；前端 client 沿用现有自动解包；服务端强制当前认证用户和精确 total |
| `GET /usage/balance-ledger/stats` | 与用户列表完全相同的业务过滤器，不接收分页和排序 | 保持现有整数 `total_entries/credit_entries/debit_entries`，以及字符串 `credit_amount/debit_amount/net_amount`，字段不改名；仅统计当前认证用户 |
| `GET /admin/usage/balance-ledger` | 在用户列表参数上增加 `user_id` 与 `exact_total`；默认 `exact_total=false`，确需精确总数才显式开启 | 独立管理员授权；可返回 `id/email/username/status` 白名单用户摘要；快速分页时 `total/pages` 只是用于判断下一页的探测值，不冒充全局精确总数 |
| `GET /admin/usage/balance-ledger/stats` | 与管理列表的业务过滤器完全相同，不接收分页、排序和 `exact_total` | 独立管理员授权；金额仍为十进制定点字符串 |

`start_date/end_date` 解释为前端所传 IANA 时区内的自然日；用户页面默认范围含今天及前 6 日，管理页面默认最近 24 小时，原始 API 未传时间过滤时仍保持当前“不自动补页面默认范围”的行为。服务端转换为 UTC 查询的 `[start,end)`，结束自然日使用次日零点；日期参数继续允许只传一端，形成开区间，页面正常操作仍始终成对传入。`start_time/end_time` 必须成对出现并为 end-exclusive，少一端返回 400；精确时间成对存在时优先于 date，不叠加第二套日期边界。`timezone` 缺省或不是有效 IANA 名称时，沿用当前兼容行为使用服务端已配置时区，不新增另一套猜测；RFC3339 精确时间自带的 offset 仍优先。缺省页面按 `(created_at DESC,id DESC)` 排序，页大小继续使用全站 `table-page-size` 偏好；`/usage` 默认打开请求记录，现有实现没有余额页签深链，本轮不新增路由状态协议。list 与 stats 是两次只读请求，首版接受并发入账造成的瞬时差异，但切换页签、应用筛选和手动刷新时必须使用同一过滤快照一起重取。

数据库中的历史 metadata 不改写，API 继续按现有合同返回 object；用户端和管理端页面共用 `(reason,direction)` presentation 配置，只读取明确允许的可读字段。旧 `account_share_mode_seat_waiver_refund` 的 credit/debit 分别按消费者退款/账号主返冲展示，不依赖历史 metadata 一定含角色字段；未知 reason 仅展示顶层 `reason/ref_type/ref_id`，不把任意 JSON 渲染到页面。Phase 0 对历史 metadata key 和样本值做索引友好的只读安全抽样；如确认已有敏感值，必须先提出保持客户端兼容的服务端脱敏方案并单独确认，不能在本轮擅自改变响应合同。页面统计文案明确为“所选期间已记录流水”，并提示“历史记录可能不完整，当前余额以账户余额为准”。

PUBLIC_POOL/ROOM 的请求分润只在现有 UsageBilling 事务尾部追加：

1. 保持上游 subscription、balance、API Key quota、rate limit、account quota 的现有顺序；points 只在原 balance 资格/扣款分支内部适配；
2. 请求计费函数只产生一份量化后的 request consumer charge；积分/余额拆分、订阅消耗、Key 配额/费率、Usage 与 request settlement 都以这份值为共同输入，账号配额仍保留上游口径；现金流水只记录拆分后实际发生的 `balance_deducted`，不得把完整 consumer charge 再记一遍；
3. 使用同一 `request_id + api_key_id` 幂等键；request fingerprint 只包含首次 `Apply` 前稳定的请求、路由、共享来源、consumer charge 和政策配置快照，且这些字段必须在首次 Normalize 前附加；事务内才解析的 inviter、有效期、effective ratio 和三方金额只进入首次 settlement 快照；
4. 在原效果全部成功后，依次追加 request settlement、号主/邀请人余额、相应余额流水和邀请消费抽成审计；
5. 任一步失败则原事务整体回滚；
6. 消费者余额先锁、事务尾部再锁号主/邀请人余额可能与反向消费形成 PostgreSQL `40P01`；不重排上游锁序，只在 `UsageBillingRepository.Apply` 外层对明确的 `40P01` 最多重试 3 次，每次新开完整事务，其他错误不重试；
7. 不把 Usage Forward 前移成余额预授权；
8. commit 后统一失效消费者、号主和邀请人的余额缓存；幂等重放必须从已落 request settlement 在 `owner_credit > 0` 时恢复 `owner_user_id`、在 `invite_credit > 0` 时恢复 `inviter_user_id`，去重后放入 `BalanceCreditUserIDs`，再执行安全的缓存失效，不依赖重放请求重新解析 owner/inviter；只有通知保持首次 `Applied=true` 才发送；
9. 不重新生成历史 settlement、消费抽成或提现流水。

席位小时费不并入上述 UsageBilling 事务，继续使用每个 membership 一笔独立短事务：

1. `hourly_rate_snapshot>0` 才进入小时链；加入事务先扣一分钟预付并推进 `paid_until`，小时费只使用现金余额，不消耗积分或订阅额度；
2. worker、加入前 catch-up 和请求前 catch-up 都先 `FOR UPDATE` 同一 membership，依据单调 `paid_until/billed_until` 只结算尚未结算的区间；确定本次消费者、账号主和有效邀请人后，统一按 `user_id` 升序锁 users 行，再改变任何余额。批量暂停、恢复和关闭额外按 `membership.id` 升序锁 membership；worker 只是推进器，不是唯一正确性入口；
3. 到期续费先确认旧预付区间的 `seat_charge`，再扣下一分钟预付和推进游标；余额不足则结清旧区间并直接结束 membership；离开/暂停/关闭以各自唯一 cutoff 强制结清并退未发生预付；
4. `seat_charge`、账号主/邀请人余额、balance ledger 和 `share_accrue` 同事务提交；即时豁免的 consumer refund 与 `seat_waiver_refund` 同事务提交；迟到豁免的 consumer refund、原账号主/邀请人 debit、balance ledger、`share_reverse` 和带 `reversal_of_settlement_id` 的 settlement 同事务提交；
5. seat charge/refund 的防重以 membership 行锁及 `paid_until/billed_until` 单调推进为首要合同；一分钟预付流水使用 `membership_id + paid_until` 稳定业务引用，waiver refund 使用 membership+period 唯一键。现状没有 seat charge/refund 的 period 唯一约束，不在未做生产查重和单独 DDL 审批时擅自增加；UsageBilling 保留仅对明确 `40P01` 最多三次、每次新开完整事务的现状，seat 事务首版依靠固定锁序防死锁，不新增缺少稳定 operation identity 的自动重放；
6. 任一步技术失败整体回滚；余额不足按第 3 条作为业务结果原子结束。正常提交后失效消费者、账号主、邀请人的余额缓存，以及已经结束或切换房间的 Key 鉴权缓存；每次首次应用的 seat 事务直接返回本次受影响用户 ID。seat no-op 重放目前没有 request ID 等稳定调用身份，不能承诺从上一笔 settlement 重建缓存失效名单；Phase 0 必须验证现有余额缓存 TTL 的有界收敛时间，若不满足财务可见性要求则阻断实现并另行审批 outbox，而不是在首版暗加一套机制；
7. seat 事务不得反向调用会自行开启事务的 UsageBilling Service，也不得把一整小时计费窗口持锁等待。

财务后台首版提供：订单/支付/退款/履约时间线、余额与积分流水、请求费用、小时预付/已确认小时费/普通退款/低消退款、房间/公共池三方分润及邀请消费抽成、提现处理、已支付未履约异常、支付/余额/积分异常和发票状态。所有金额与层级由服务端返回；前端只展示，不自行计算分润或可提现额。

报表必须区分“业务结算事实”和“资产变动审计”，不能把同一金额重复相加：消费者小时现金净支出从 `user_balance_ledger` 计算“预付 debit - seat_refund credit - waiver refund credit”；平台、号主和邀请人的小时确认收入从 settlement 计算“`seat_charge` - 引用原 charge 的迟到 waiver reversal”，普通 `seat_refund` 与即时 waiver 对确认收入的贡献均为 0。统一适配器按指标返回不同投影，禁止用一个通用 signed sum 同时服务 My Spend 与收入。其他余额变动仍以带业务引用的 `user_balance_ledger` 对账；`user_affiliate_ledger action IN ('share_accrue','share_reverse')` 只作为按用户/期间/金额的聚合审计，不承诺逐行一一关联，也不作为第二份收入事实；提现状态以 withdrawal request 为事实。双表适配器只对相同 request 业务引用做跨表查重，若重复则报异常而不是相加；三类 seat 明细均原样保留以供审计。

### 5.15 前端页面风格保留

前端是兼容重建，不做视觉重构。继续使用 Vue 3、TypeScript、Vite、Pinia、Vue Router、Vue I18n、Tailwind 和现有 Vitest 测试栈；不更换框架、组件库或另建 Design System。

必须保留：

- 当前统一应用壳：桌面侧栏、移动抽屉、64px 顶栏、内容区间距、余额/用户/邀请/主题入口，以及 simple mode/feature flag 的导航显隐；
- 当前语义 token：`canvas / surface / content / line / brand / positive / warning / danger`、轻边框、低阴影、控件与面板圆角及对应暗色主题；
- 当前 `route.meta.uiSkin` 的 legacy/v2 边界；本轮不把所有页面强制改成 v2，也不从单页复制硬编码颜色；
- 现有核心路径：`/store`、`/accounts`、`/account-share`、`/usage`、`/purchase?tab=redeem`、`/activities`、`/affiliate`、`/orders`、`/invoices`、`/profile`、`/admin/usage`、`/admin/redeem`、`/admin/activities`、`/admin/store/*`、`/admin/orders`、`/admin/withdrawals`、`/admin/invoices`、`/admin/revenue`；
- `AppLayout`、`TablePageLayout`、`DataTable`、`BaseDialog`、`ConfirmDialog`、`EmptyState`、`Pagination`、筛选/选择/日期/状态等公共组件和现有支付图标资产。

保留的是视觉骨架和用户习惯，不是旧页面内部的业务实现：

| 模块 | 保留的视觉/交互骨架 | 业务重接边界 |
|---|---|---|
| 我的账号 | `TablePageLayout + DataTable`、筛选、批量区、状态徽标、创建/导入弹窗 | 重接统一账号 Service，不复制旧分享投影和批处理逻辑 |
| 账号广场 | 最大宽度内容区、Hero、平台页签、摘要、筛选、房间卡片、加入确认、低消进度、请求费与小时费消费明细、历史快照和移动端卡片 | 保留倍率、小时价、豁免门槛、最低余额、`paid_until/billed_until`；不移植 queued/ending/draining、消费者 membership 暂停/恢复、排空、选号助手或“下一次 API 请求才激活” |
| 使用记录/余额流水 | 保留 `/usage` 的 v2 skin、`AppLayout + TablePageLayout`、可键盘切换且首次进入才加载的请求记录/余额流水双页签、四张流水统计卡、时间/方向/原因/引用筛选、DataTable、分页和空态；金额按 credit 绿“+”、debit 红“-”并显示 `balance_after`，reason 同时显示本地化名称与原码，metadata 只按 `(reason,direction)` 展示白名单转成可读说明 | 用户端只读自己的 `user_balance_ledger`，管理端复用同一查询；列表和统计使用同一过滤合同，不从 points、settlement、affiliate 或 shop 表在前端拼账；余额流水首版不新增 CSV 导出 |
| 房间配额管理 | 现有管理入口、全局默认/房主覆盖、四项配额和审计表格 | 只重接创建/编辑限额；删除组件中的 drain/排空文案和任何生命周期副作用 |
| 商城 | 简洁 Hero、资产摘要、响应式商品卡、结算弹窗和交付结果 | 价格、支付资格和交付完全以服务端结果为准 |
| 福利/兑换 | 现有页签、统计卡、活动卡、领取弹窗、兑换结果和历史列表 | 共用奖励类型与展示组件，不在前端直接改资产 |
| 邀请关系与消费返利 | 当前指标卡、邀请码/链接复制、周额度说明、时间筛选、邀请记录和顶部快捷入口 | 抽取无布局的共享邀请内容组件，`/affiliate` 页面壳和 Activities 页签分别嵌入；`HeaderInviteLink` 只复用 `/user/aff/share` 摘要接口；请求抽成、小时抽成及豁免返冲后的期间/累计净额均由服务端返回，累计抽成不再读取 `aff_history_quota` |
| 提现 | Profile 的资产/申请/收款码/记录卡片，以及后台表格和处理弹窗 | 只展示服务端可提现额、费用和状态；不开放积分提现 |
| 发票 | 资料、可开票来源和申请记录的两列卡片布局 | 资格、币种、金额和退款互斥全部由服务端判定 |
| 财务后台 | 顶部页签、指标卡、趋势、明细表和状态弹窗 | 纳入余额、积分、请求/小时结算、共享分润/邀请消费抽成、提现和发票；结算 DTO 增加 `settlement_type`、允许 request ID 为空，前端只展示服务端有符号金额 |

账号广场创建/编辑和加入确认继续展示并提交 `rate_multiplier / hourly_rate / hourly_fee_waiver_minimum / min_balance_required`；确认文案分两行说明“请求按实际用量和房间有效倍率结算”及“小时费按 active 占席时长一分钟滚动预付、低消达标后退款”，不得由前端估算绝对请求价。当前 membership 展示加入时间、下次预付、已结算到、低消窗口/进度；历史展示当时快照，`snapshot_quality != exact` 时必须标注，不能用当前房间价格冒充旧账。页面消费汇总只展示服务端返回值：

```text
hourly_net_cost = max(0, hourly_charge - hourly_refund - hourly_waiver_refund)
total_cost      = request_cost + hourly_net_cost
```

为兼容现有 API，My Spend 的 `hourly_charge` 实际表示从 `user_balance_ledger` 汇总的小时预付 debit，不是 settlement 的 `seat_charge` 收入确认；`hourly_refund` 和 `hourly_waiver_refund` 是对应的消费者 credit。上述公式及 `max(0,...)` 原样保留当前消费者页面口径。财务和邀请收益则按 settlement 的 `seat_charge - late waiver reversal` 有符号口径汇总，不能减从未确认收入的 `seat_refund`，也不能 clamp 掉返冲。后端 DTO 文档必须把这两类同名近义字段标明数据源，禁止跨口径相加。

加入接口收口为最小 DTO，删除 queue 和独立条款版本字段：

```text
CreateJoinIntentRequest = { api_key_id, idle_timeout_minutes }
JoinIntentResponse      = {
  listing_id, api_key_id, token, expires_at, expected_version,
  terms: {
    row_version, rate_multiplier, hourly_rate,
    hourly_fee_waiver_minimum, min_balance_required,
    seat_limit, per_user_concurrency, allowed_models,
    platform_protection
  }
}
JoinRequest             = {
  api_key_id, idle_timeout_minutes, intent_token, expected_version
}
```

新 DTO 不再出现 `expected_revision_id / listing_revision_id / accept_queue / queue_may_be_required`。房间状态只向新 UI 暴露 `ACTIVE/PAUSED/CLOSED`，membership 只暴露 `active/ended`；`validating/draining/suspended/queued/ending`、review、blocker 和 operation 字段只在服务端历史兼容层解释，不进入新页面类型。管理端 settlement DTO 至少返回全局唯一的 `settlement_ref`（由 `source_table:id:settlement_type` 组成）、`settlement_type`、nullable `request_id/listing_id/membership_id/reversal_of_settlement_ref`；列表、详情和导出都使用该引用，不能只用可能跨表重号的数字 ID。

Profile 还要显示积分明细入口和“永久并发 + 当前临时并发 + 最近到期时间”，但临时并发不新增独立页面。邀请相关页面只保留当前 Pixel 的邀请码、直接邀请关系、有效期和消费抽成视图；不恢复上游充值返利的额度、冻结、成熟、转余额或四类管理页面。`/admin/settings` 邀请卡只保留总开关、消费收益有效期、关系补绑和延长；`/admin/revenue` 的统一分成策略是 `invite_share_ratio` 的唯一配置入口；旧 rate/freeze/cap/batch-rate 字段不显示、不提交。所有保留页面继续使用本地现有应用壳/token。

实现阶段建立 360px、768px、1440px 三档截图/交互基线，同时验证浅色/深色、legacy/v2、移动端表格卡片、键盘操作、Teleport 皮肤继承、44px 触控目标和 reduced-motion。旧 `AccountShareView` 只作为视觉参考，禁止整文件复制。

## 6. 数据库物理复用方案

### 6.1 核心表处理

| 数据域 | 物理对象 | 处理方式 |
|---|---|---|
| 用户与认证 | `users`、现有 auth identity/session 表 | 以上游结构为主；保留 ID、密码哈希、角色、状态和认证关系，并原地兼容 points/prefer-points 等现有增量字段 |
| API Key | `api_keys` | 完整复用上游；保留 Key 的既有存储与校验语义 |
| 多分组 | `api_key_group_routes` | 作为 Pixel 增量表原地复用；不压回单 `group_id` |
| 账号与等级 | `accounts`、`account_groups`、`groups` | 以上游结构为主；accounts 保存账号事实，account_groups 只作 scheduler 投影 |
| Channel/定价 | `channels`、`channel_groups`、`channel_model_pricing` | 复用上游；用兼容查询接入现有时间段表 |
| 峰谷扩展 | `channel_pricing_time_ranges`、`group_rate_schedules`、`group_rate_schedule_states` | 原地复用现有数据；state 仅承接旧 worker 基础倍率，不把绝对倍率接成 peak 因子 |
| 投放 | `account_external_placements` | 原地复用并作为三态权威；无行表示 PRIVATE，accounts.share_* 只作公共分账投影 |
| 房间 | `account_share_listings`、`account_share_room_accounts`、`account_share_memberships` | 原地复用 ID 和历史；listing 的倍率/小时价/豁免/最低余额及 membership 的费率/空闲退出快照、`paid_until/billed_until` 继续 active 新写，对外只暴露简单加入/离开生命周期 |
| 房间运营配额 | `account_share_quota_policies` 及现有审计对象 | 原地复用四项创建/账号数量限制、全局默认和房主覆盖；只约束 CRUD，不写 drain/排空/operation |
| 共享政策 | `account_share_policies` | 原地复用为 PUBLIC_POOL/ROOM 新写的唯一政策；`account_share_mode_policies` 仅兼容历史 |
| 共享结算 | `account_share_settlement_entries`、`account_share_mode_settlement_entries` | 前者扩展为 PUBLIC_POOL/ROOM request 新写；后者保留旧 ROOM request 并继续新写 `seat_charge/seat_refund/seat_waiver_refund` 及 reversal 字段，只加 nullable signed `owner_wallet_delta NUMERIC(20,8)`、`invite_wallet_delta NUMERIC(20,8)`；新 charge/迟到 waiver 写实际正负差额，普通 refund/即时 waiver 写 0/0，历史 NULL 不回填。同一 Repository 读取双表的类型化事实并按指标生成有符号投影，不搬历史、不把普通 seat_refund 当确认收入负项 |
| 现金余额流水 | `users.balance`、`user_balance_ledger` | `users.balance` 保持现金余额事实；原地复用流水主键、direction/amount/reason/ref、`balance_after`、metadata、用户时间/业务引用索引和现有历史。首版未来现金变更在原业务事务内追加流水；原来只有单条 autocommit mutation 的路径按 P03 在原调用点用单条原子 SQL 或局部短事务收口。历史缺口不回填、不从其他账本合成 |
| 积分 | `users.points_balance/prefer_points_billing`、`points_ledger` | 原地复用余额和历史；所有新写通过同一事务感知 Helper，积分不可提现 |
| 临时并发 | `user_concurrency_grants` | 新增最小表；用来源唯一键防重，以时间区间判活，不创建 worker/status |
| 邀请关系与消费返利 | `user_affiliates`、`user_affiliate_ledger` | 前者原地复用直接邀请关系、邀请码策略和奖励有效期；后者保留生产 `share_accrue` 审计历史，请求/小时正向抽成写 `share_accrue`，迟到小时豁免在确有原邀请收益时写 `share_reverse`；仍禁止上游充值返利的 `accrue/transfer`；旧充值物理列原地不删但新代码、DTO、查询和 UI 均不使用；不建立多级关系表 |
| 提现 | `user_withdrawal_requests`、`user_receipt_codes` | 原地复用申请、状态和收款码；只给未来扣除/释放在原事务追加余额流水，不回填历史 |
| 支付 | `payment_orders` 和上游支付实例/Webhook 表 | 原地复用；仅按需加 `currency` 等字段 |
| 商城 | `shop_*` | 原地复用订单、库存和交付历史；只给 card key 增加 nullable `content_encoding` |
| 兑换 | `redeem_codes` | 原地复用；新发票不再依赖无币种的兑换码 |
| 发票 | `invoice_management_enabled` 与四张 `invoice_*` 表 | 原地复用，固定为不可退役核心对象 |
| 福利 | `benefit_campaigns`、`benefit_claims` | 旧随机活动历史只读；新增两张最小表表达确定性领取及余额/订阅/积分/永久或临时并发奖励 |
| 迁移 | `schema_migrations`、`atlas_schema_revisions` | 一个上游 runner/lock；前者是逐文件 checksum ledger，后者是上游 Atlas baseline 状态；不创建 Pixel 第二账本 |

### 6.2 历史兼容但停止新写

首版不再写入以下类型的对象：

- 房间 queue、ending、draining、operation、binding、terms revision、review 和 recommendation；
- 上述 stop-write 不包括房间请求结算、席位一分钟预付、`paid_until/billed_until` 推进、`seat_charge/seat_refund/seat_waiver_refund` 或必要的 `share_reverse`；这些仍是首版 active 财务事实；
- 随机抽奖、排行榜和保底；旧活动奖励记录继续可查，但新确定性福利改走统一奖励发放；
- Ideas、工单、集群控制和其他退出范围模块；
- 新版不支持的文件卡密新增路径；
- 旧模型目录缓存或静态白名单；
- 上游充值返利的 `payment_fulfillment -> affiliate` 新写、`accrue/transfer` action、`source_order_id`、`aff_quota/aff_frozen_quota` 余额化及转余额接口；旧物理列原地不删但新代码不读写。当前生产未发现 `accrue/transfer/source_order_id` 业务历史；若 Phase 0 发现异常值，只列清单并另提兼容方案，不自动注册查询或启用业务。

这些对象分成三类：

1. 被财务、订单、发票、Usage 或审计引用：保留；
2. 有历史数据但无运行依赖：先只读，保留期后归档再删；
3. 无数据、无外键、无代码引用：可进入后续 DDL 删除候选。

不得因为“上游没有这张表”或“前端菜单已删除”直接 DROP。

## 7. 生产数据库只读审查结论

### 7.1 审查边界

- 主机：`207.32.218.139`；
- 审查时间：2026-09-05（Asia/Shanghai）；小时计费补充复核截至 01:28。均为执行时当前快照聚合，不是业务时间区间查询；
- 数据库：`sub2api`；主体审查使用应用角色 `sub2api`，补充的 Atlas/schedule-state 元数据聚合通过本机 socket 使用 `postgres`；
- PostgreSQL：18.6，主库，不是 recovery 副本；
- 事务：`BEGIN READ ONLY` 或 `BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY`，`statement_timeout=10s`，`lock_timeout=2s`，全部以 `ROLLBACK` 结束；
- 返回内容：catalog、约束、索引、状态/类型计数和一致性异常数量；补充查询覆盖 schedule、积分、提现、邀请、共享政策、房间小时配置、membership 状态与双游标、settlement type、waiver reversal 和小时 period 重复，均只返回聚合计数，不返回用户或订单明细；
- 未读取：API Key、卡密内容、账号凭证、用户身份字段、订单号、税号、银行资料或业务明细；
- 未执行：DDL、INSERT、UPDATE、DELETE、迁移记账、部署或重启。

### 7.2 关键结果与设计影响

| 观察项 | 生产结果 | 设计结论 |
|---|---:|---|
| `schema_migrations` | 321 行 | 继续使用上游逐文件 ledger；实施前必须做文件名/checksum adoption manifest |
| `atlas_schema_revisions` | 1 条 baseline：`153_shop_file_card_keys`，`applied/total=0/0`，错误 0，hash 长度 64 | 表和 baseline 已存在；仍须在 Phase 0 与目标 runner 计算结果比对，不能仅凭“有一行”判定可直接启动 |
| PUBLIC_POOL placement | 124,057，全部 active | 不能重建或搬迁投放关系 |
| ROOM placement | 2,771，全部 active | ROOM placement 是资格；具体房间由 room-account 决定 |
| 非法 placement 目标 | 0 | 现有互斥约束可继续使用 |
| room-account | 138 行，均有匹配 placement/room identity | 双表当前一致，不需要数据修正 |
| room-account 状态 | 138 行全部 active | 新 scheduler 仅接收 active，不需要状态转换 |
| 已挂账号房间 | 103，混合等级 0，最大等级数 1 | 房间固定同平台同等级是最简兼容规则 |
| 房间 | active 30、paused 151、suspended 55、已删除 draining 2,125 | 用三态逻辑映射，不批量改历史状态 |
| membership | active 26、queued 8、ended 80,792 | 新版只把 active 当开放关系；queue 只读。该值是高频变化快照，实施前重新核验 |
| 房间小时费配置 | 236 个未删除房间中 209 个 `hourly_rate>0`，43 个 `hourly_fee_waiver_minimum>0` | 小时费和低消豁免是生产主流配置，不能按旧蓝图删除或只做历史读取 |
| membership 小时快照 | active 26 中 14 个小时费大于 0，14 个均有 `paid_until/billed_until`；小时 active 的空游标及 `billed_until>paid_until` 异常均为 0 | 双游标是 active 财务合同；不能合列或只保留展示字段 |
| Key 多 active 房间 | 0，最大 1 | “一个 API Key 整体一个房间”与现网一致 |
| Key 多旧 live 房间 | 2 | 由 queued 历史造成；运行时不视为开放关系，但启用新加入写入前仍须完成四个冲突索引的批准删除 |
| Group route | 50,794 行，50,448 enabled，47,276 个 Key | 多分组是现有核心数据，不能压回单 Group |
| 多 enabled route Key | 1,682，最多 8 个 route | 必须保留真正多分组路由 |
| active Key 的 `group_id` 无对应 enabled route | 0 | `api_keys.group_id` 至少有匹配 enabled route |
| active Key 主投影与排序首 Route 不同 | 1 / 32,446 个有 enabled route 的 active Key | Route 表作为运行时权威；首切不改数据，下一次编辑时同步投影 |
| 跨平台 route Key | 157 | 首切兼容存量；新建/编辑时再执行同平台规则 |
| Channel pricing | 48 行，全部属于 active Channel 且有具体模型 | priced catalog 可直接以上游 Channel 表为根 |
| 上游 Channel time JSON | 0 个有效配置 | 不能把旧时间段表当作可删除冗余 |
| Pixel Channel time range | 6 行/3 个 pricing，重叠 0 | 原地读取并适配上游 PricingAt |
| 上游 Group peak | 0 个启用 Group | 生产峰谷事实仍在 Pixel schedule 表 |
| Pixel Group schedule | 11 行/5 个 Group，重叠 0 | 原地读取为绝对 Group 基础倍率；不得接入 `PeakMultiplierAt` 形成二次乘法 |
| Group schedule state | 5 行/5 个 Group；孤儿 Group、缺失 schedule、跨 Group 引用、非正基础倍率均为 0；当前 Group 倍率等于保存基础倍率 2/5 | 旧 worker 状态关系完整；首切按只读兼容解释，不能把倍率差异自动修成同值 |
| 发票 | 1 张 issued/CNY | 发票模块必须保留 |
| 发票来源 | 1 条 active redeem_code，重复 0 | 历史保留；新申请改为可靠 CNY payment order |
| active payment invoice/refund 冲突 | 0 / 0 | 历史无需修正；按 §5.10 在退款状态 claim/CAS 的最小订单锁短事务补互斥，Provider I/O 保持在事务外 |
| 兑换码 | 76,551 行，多种 type/status | 不删除或重解释历史；兑换码无币种事实 |
| 商城卡密 | 1,956 条文本卡，707 available、1,249 sold | 必须兼容旧明文读取和现有库存状态 |
| 用户积分 | 28,035 个 active 用户；200 个正积分、0 个负积分、140 个启用积分优先 | 积分已是生产资产，不能删除或并入余额；沿用 points 字段与非负约束 |
| 积分流水 | `credit/redeem_code` 3、`debit/shop_order` 3、`debit/usage_charge` 356 | 已有多入口真实写入；新实现必须用统一 Helper 兼容现有 reason/ref，不重算历史 |
| 提现申请 | SETTLED 3,376、PENDING 34、CANCELLED 418、REJECTED 36；金额公式异常 0、多 PENDING 用户 0 | 现有状态机和唯一待处理约束可原地复用；未来写入补余额流水，历史不回填 |
| 邀请关系 | `user_affiliates` 27,885 行，14,820 个已绑定 inviter；自邀请 0、个人自定义充值返利率 0、可用 quota 大于 0 为 0、冻结 quota 大于 0 为 0、负 quota 0 | 单级邀请关系和邀请码策略是生产合同；旧充值返利资产字段没有现行余额，不能据此启用第二套返利或臆造多级树 |
| 邀请流水 | 1,557,472 行，当前 action 全部为 `share_accrue` | 现网业务来源是共享消费抽成；新版正向请求/小时抽成继续写 `share_accrue`，未来只有迟到小时豁免且原结算确有邀请收益时才允许 `share_reverse`；仍不得产生上游充值返利的 `accrue/transfer` 或 `source_order_id` |
| 共享政策 | 1 条 live policy，invite ratio 大于 0，比例越界 0 | 邀请消费分润是现有核心，不应从新版删除；PUBLIC_POOL/ROOM 共用一次政策解析 |
| ROOM mode 表结算类型 | `account_share_mode_settlement_entries` 中 `usage_request` 158,165、`seat_charge` 23,734、`seat_refund` 862、`seat_waiver_refund` 559 | 请求费与小时费均有真实历史；四类均须兼容，其中旧 mode `usage_request` 只读、后三类继续 active 新写；canonical request 表行数和跨表重复仍在 Phase 0 精确核验 |
| 小时豁免 reversal | 559 条 waiver refund 中 2 条引用原 settlement、557 条为即时豁免；当前邀请流水没有 `share_reverse` | 即时豁免和迟到返冲必须区分；不能因当前无 `share_reverse` 历史就删除未来正确返冲能力 |
| 小时区间重复 | 按 `(membership_id,settlement_type,period_started_at,period_ended_at)` 聚合的 `seat_charge/seat_refund/seat_waiver_refund` 重复组为 0 | 现有行锁+双游标目前有效；任何新增 period 唯一索引仍需单独 DDL 审批，不能由本次只读结果直接授权 |
| 新福利/临时并发表 | `benefit_campaigns`、`benefit_claims`、`user_concurrency_grants` 均不存在；旧 `activity_prizes` 仅 1 条 balance 奖励 | 只新增确定性福利两表和临时并发 grant 表；不迁移旧活动历史 |
| migration checksum 格式异常 | 0 | ledger 值格式正常；仍需逐文件比对内容 checksum |

表估算值不用于判断“空表”；上述业务结论使用实际聚合计数。本轮曾用模糊 `lower(reason)` 检索提现余额流水，该查询触发 10 秒 `statement_timeout`，事务已回滚，因此不能据此声称生产历史“存在”或“不存在”提现流水；“当前提现代码未写余额流水”来自本地调用链审查。Phase 0 只允许用索引友好的 ref/reason 精确核验，不提高生产超时、不做全表模糊扫描。完整逐表外键、trigger 和 migration checksum 比对仍是实施阶段的只读门禁，而不是新增架构。

## 8. 事务、顺序和并发边界

| 操作 | 必须复用的边界 | Pixel 允许追加的内容 |
|---|---|---|
| API Key 请求 | 每个上游 Handler 各自的既有阶段顺序 | 只在入口阶段矩阵标定的位置读取 Route 候选、增加房间候选过滤和生成请求快照；房间请求按原顺序取得 Redis lease 后、Provider Forward 前，以现有 listing/RoomAccount/membership 行锁做一次短 admission recheck 并冻结快照；在原 `CheckBillingEligibility` 内扩展积分资格谓词，不移动其他调用 |
| Usage 请求计费 | `UsageBillingRepository.Apply` 的幂等事务和现有效果顺序 | 在原扣款位置复用积分优先逻辑；复用唯一量化请求费用，在事务尾部追加 request settlement、号主/邀请人余额及审计；仅对明确 `40P01` 用全新事务整体重试 |
| 席位小时计费 | Pixel 现有 membership 独立短事务语义，不进入上游 UsageBilling | 加入时一分钟预付；worker/加入前/请求前 catch-up 共用同一结算函数；按 membership 行锁和双游标推进 `seat_charge`，离开/暂停/关闭时结清并退未发生预付，豁免补偿按原快照返冲；全部资金/流水/settlement/游标同事务 |
| 投放切换 | 现有账号/placement Repository 事务 | 同事务维护外部 placement、room-account 和必要 account-group 关系 |
| 加入/离开房间 | Pixel membership Repository 的短事务，不包住 Provider I/O | 按固定对象锁序校验；加入把初始预付与 active 一起提交，离开以 `ended_at` 把最终小时结算、未用预付退款与 ended 一起提交，提交点阻止新请求快照；技术失败时保持原状态，余额不足按预期业务规则结清并结束，对外无 queue/ending operation |
| 房间账号调整 | room-account Repository 单事务 | 必要时更新 membership 兼容锚点；不建异步 operation |
| 商城下单 | 现有订单与库存事务 | 保留幂等键；余额或外部支付仍走现有服务 |
| 商城履约 | 现有支付完成/履约事务 | 锁卡密并在提交前完成解密校验，再写订单交付快照和 sold 状态 |
| 福利/兑换奖励 | 各自现有 claim/use-code 事务 | 在同一 tx 内调用窄奖励分派；积分同时写 points ledger，临时并发插入来源唯一的 grant，任一失败整体回滚 |
| 临时并发生效 | API Key/JWT 认证投影和既有 auth cache | 在 `AuthSubject.Concurrency` 形成前求永久值与有效 grant 之和；提交后失效缓存，缓存 TTL 不跨下一次生效或到期边界 |
| 支付履约 | 上游订单 claim、支付完成和履约原调用顺序 | 明确不注册或调用上游 affiliate 充值返利；除删除这一未采用的副作用外，其余顺序和事务保持不变，支付成功不得写邀请收入、quota 或 affiliate 流水 |
| 提现提交/释放 | 现有 withdrawal Repository 的用户/申请锁事务 | 原扣除或返还之后、提交之前追加引用申请 ID 的 balance ledger；人工/Provider 打款不进事务 |
| 发票申请 | Pixel 现有发票短事务 | 锁 PaymentOrder，校验 `pay_amount`、币种、退款事实和 active source |
| 退款 | 上游 Prepare/Execute/finalize 的既有调用顺序；Provider I/O 仍在事务外 | 只把各自的状态 claim/CAS 放入最小订单锁事务并增加 active invoice 条件；Provider 成功后的原单次余额扣减在原调用点原子写 `balance + ledger`，之后仍单独执行原 `markRefundOk`。不把 Provider、扣减和 finalize 合并成长事务 |
| 代理到期 | 上游每源代理事务 | 事务内调用更严格的兼容资格纯函数 |
| 数据库迁移 | 上游单 runner、单 advisory lock、逐文件事务，以及其 `schema_migrations`/Atlas 元数据维护 | 增加 Pixel forward migration 文件，不增加第二 runner 或第二锁 |

统一要求：

- 不在一个事务中再调用会自行开启事务的 Service；
- 不重排上游已有锁顺序；Pixel 自己新增的短事务必须明确并固定锁序；
- 新增幂等键优先复用 request ID、order ID、payment ID、membership ID 等稳定业务键；小时费在既有 membership 行锁下以单调 `paid_until/billed_until` 和计费区间推进，豁免返冲必须引用原 settlement；
- 请求快照只保存结算和审计必需字段，不创建通用 DispatchDecision 大对象；
- 缓存失效和通知继续在原事务提交后执行；
- 已 Forward 的请求不能被暂停/离开操作强制回滚，request 结算使用请求开始时快照；离开后的迟到 usage 不延长小时占席，只能参与截止窗口的幂等低消补偿。

## 9. 单迁移执行链与生产收养

### 9.1 迁移方案

只使用上游的一条迁移执行链：

- `backend/migrations`；
- `schema_migrations(filename, checksum, applied_at)`；
- `atlas_schema_revisions` 所承载的上游 Atlas baseline 状态；
- 一个 embedded migration FS；
- 一个 PostgreSQL advisory lock；
- 普通 migration 的逐文件事务和 `_notx.sql` 特殊路径。

这里的“单”指一个 runner、一个目录和一个 advisory lock，不表示上游只有一张物理元数据表。`schema_migrations` 与 `atlas_schema_revisions` 都属于上游执行链，新版必须按原逻辑维护；Pixel 不再增加自己的迁移元数据表。

不创建：

- `backend/pixelmigrations`；
- `pixel_schema_migrations`；
- 第二 migration runner；
- 第二 advisory lock；
- runtime schema fingerprint gate；
- 自动猜测的 baseline、Atlas 状态或跨执行链同步器。

新的 Pixel 文件使用不会与上游数字序号碰撞的命名，例如：

```text
pixel_20260904_001_account_ownership.sql
pixel_20260904_002_group_routes.sql
pixel_20260904_003_account_share.sql
```

Runner 仍按文件名排序，Pixel 文件自然排在上游数字文件后面。

### 9.2 Adoption manifest

第一次让上游 runner 接触生产库前，离线生成一份一次性 manifest，对上游每个 migration 和当前 Atlas baseline 状态分类。由于 runner 会在逐文件 checksum 校验前确保 Atlas baseline 对齐，任何实际启动都可能写入元数据，不能拿生产启动来代替只读审查。

| 分类 | 条件 | 处理 |
|---|---|---|
| `MATCH` | 生产 filename 和 checksum 与上游文件一致 | runner 正常跳过 |
| `EXTRA_DB_ROW` | 生产有、目标 FS 没有的旧 Pixel migration | 原样保留；runner 不会处理 |
| `ABSENT_SATISFIED` | 上游原文件未记账，但全部 schema 后置条件已满足 | 先以只读断言证明等价；经单独生产写授权后，原子记录该上游原始 filename 和完整文件 checksum |
| `ABSENT_NEEDED` | 上游原文件未记账且后置条件缺失 | 默认按上游原文件原样执行；若原文件对生产不安全或不兼容，则阻断 runner，另行审批一次性 pre-adoption 事务，最小补齐等价后置条件并在同一事务记录该上游原始 filename/完整 checksum |
| `CHECKSUM_CONFLICT` | 同 filename 不同 checksum | 阻断启动；核对历史版本和后置条件，不自动放宽 |

另建一个排序更靠后的 Pixel compatibility migration，不能替代 `ABSENT_NEEDED` 的 pre-adoption：runner 会先遇到缺失的上游数字 migration。生产现有 321 条 ledger 只证明规模，不证明能直接启动。完整 filename/checksum 清单、Atlas 状态和每一项 pre-adoption 处理必须在 Phase 0 形成可复核结果。

### 9.3 必要的上游 migration runner 小修

已确认至少 `024_add_gemini_tier_id.sql`、`037_ops_alert_silences.sql` 等上游文件同时包含 Goose `Up` 和 `Down`，而当前 runner 对普通文件执行整个文本。空库运行会先执行 Up 再执行 Down。

只做一个窄修复：

- checksum 仍按完整原文件计算；
- 执行前若存在 Goose marker，只截取 `-- +goose Up` 到 `-- +goose Down` 之间内容；
- 不改变文件排序、`schema_migrations`/Atlas 维护、锁、普通事务和 `_notx` 分支；
- 增加空库 migration 测试证明创建结果存在；
- 不借此引入 Goose 框架或第二迁移体系。

### 9.4 结构增量规则

每个 Pixel migration 同时服务空库和现有生产库：

1. 先检查目标表/列/索引/约束的真实定义；
2. 已存在且等价则 no-op；
3. 缺失则执行最小 `ADD/CREATE`；
4. 同名但语义不同则 fail-fast，不自动 DROP/重建；
5. 大表索引使用独立 `_notx.sql` 和 `CONCURRENTLY`；
6. 新约束先用只读查询验证违规数，再决定直接创建或 `NOT VALID -> VALIDATE`；
7. migration 不包含业务数据重算、余额 opening、支付重放或历史状态批量改名；
8. DDL、必要数据修正和退役 DROP 分成不同批次、分别授权。

## 10. 实施阶段

### Phase 0：冻结证据和兼容清单

产出：

- 固定上游提交和依赖锁文件；
- 导出目标 FS migration 的 filename/checksum；
- 只读比对生产 321 条 `schema_migrations`，并核对 `atlas_schema_revisions` 的真实定义和 baseline 状态，形成 adoption manifest；
- 对每个 `ABSENT_NEEDED` 明确“执行上游原文件”或“阻断 runner 并单独审批 pre-adoption”的唯一处理，不允许靠后置 compatibility 文件越过；
- 补齐核心表 columns/constraints/indexes/triggers 的定义对照；
- 把 157 个跨平台 Route、8 条 queued membership、积分、提现、邀请/分润 action、请求/小时双 settlement active 事实、`user_balance_ledger` 结构/原因/业务引用及退役表列入兼容清单；
- 输出 §5.14 规定的现金流水事件矩阵：逐调用点区分当前 writer 输入与重建目标 `cash_delta_8`，冻结 action/role slot、direction、reason、canonical ref、不可变 payload、未来写入 metadata 白名单、业务 claim/现有唯一索引适用性、零 delta 结果、同用户多角色的原写入顺序和幂等结果；同时建立并首轮运行索引友好的只读门禁，核实生产 `users.balance` typmod、仓库外余额 trigger、legacy seat writer 来源、半舍入点候选数量及其 owner/inviter 原正向流水唯一性，形成可在最终切换停写窗口原样刷新的 legacy wallet-delta 解析器输入和阻断清单，不排空、不回填。历史 metadata 只做有界只读抽样，任何尚未核实的新增 reason 或敏感历史值保持阻断，不在代码阶段临时命名或擅改响应；
- 核对上游 `134_affiliate_ledger_audit_snapshots.sql` 与本地内容相同但改名的 `154_affiliate_ledger_audit_snapshots.sql` 的 checksum/adoption 关系，避免 runner 重复执行等价结构；此项只解决 schema/runner 兼容，不代表启用上游充值返利；
- 用索引友好的只读 SQL 检查 affiliate 环、自邀请、关系计数、余额流水 direction/amount/ref 异常及已知业务原因分布、request 跨表重复、小时配置/双游标、非 ACTIVE 房间仍有 active 小时 membership 的组合、各 settlement type/reversal、seat period 重复、`share_reverse`、积分 ref 重复与提现收款码引用；同时核对所有首版 `users.balance` 变更调用点是否同事务写 `user_balance_ledger`、动态 RoomAccount 与 `membership.account_id` 锚点的实际调用校验，并确认余额/auth cache TTL 的有界收敛时间；不对大表做模糊扫描或无索引精确总数；
- 逐调用点冻结一分钟预付、15 秒 worker、join/request catch-up、低消宽限/迟到补偿、退出结算及缓存失效的当前顺序；明确从旧 `StartSeatBillingWorker` 中只拆取纯 seat billing 和 waiver compensation，不把 validation/lifecycle/orphan cleanup 一并复制；
- 固定当前前端的路由、route skin、360/768/1440 三档页面截图、主题和公共组件清单；单独冻结 `/usage` 双页签、余额流水四张统计卡、筛选/分页、reason/ref/metadata 展示及用户/管理权限边界；只读核对房间运营配额表/覆盖/审计，形成视觉与配置兼容基线；
- 逐 Handler/协议输出入口阶段矩阵和 characterization test，记录真实调用顺序、缺失阶段、Pixel 插入点与事务边界；对所有存在计费资格的入口标明积分资格共享 Helper。

门禁：不做任何生产写入。

### Phase 1：创建干净上游分支

产出：

- 从 `b1748c4e...` 创建新工作树；
- 原样构建后端和前端；
- 在未加入 Pixel 代码前通过上游测试；
- 修复 Goose Up/Down 执行缺陷并增加空库测试；
- 用逐文件 patch matrix 约束后续改动：上游文件、最小 Pixel 修改、对应回归测试、是否触及阶段顺序/事务。

门禁：上游原始行为测试必须先绿，后续每个 Pixel patch 都与该基线比较。

### Phase 2：数据库兼容骨架

产出：

- 在上游 Ent/schema 中只补必要的 owner、level、proxy、share、points、withdrawal、直接邀请关系/消费抽成与临时并发字段/对象；
- 为已有 Pixel 核心表编写 consolidated forward migrations；
- 原地收养 `user_balance_ledger`、`points_ledger`、`user_affiliates/user_affiliate_ledger`、提现/收款码表、`account_share_settlement_entries` 和 `account_share_mode_settlement_entries`；只给后者增加 nullable signed `owner_wallet_delta NUMERIC(20,8)`、`invite_wallet_delta NUMERIC(20,8)`，不回填历史。将 Phase 0 现金流水事件矩阵固化为单一后端常量/写入合同和单一前端 `(reason,direction)` presentation 配置，先完成 canonical ref、action/role slot、零 delta、新 seat 各类型 NULL/0/正负快照矩阵、legacy 半舍入解析、未来 writer metadata 白名单和未知 reason 展示回退测试，再允许业务接入；listing 小时配置、membership 费率快照/双游标、seat type/reversal 与现有唯一索引继续 active；邀请相关收养关系、邀请码策略、有效期、`share_accrue` 历史及小时豁免所需的 `share_reverse` action，充值返利物理字段只做“原地不删”的 schema 兼容，新代码不读写；只按 §6 清单新增 grant/确定性福利表，以及 request settlement、订单币种、卡密编码等已明确的必要列；
- 增加生产 schema assertion 测试和空库 migration 测试；
- 保持一个 runner/lock，并按上游原逻辑同时维护 `schema_migrations` 与 Atlas baseline 状态；
- 不执行生产 migration。

门禁：空库能完整创建；生产 schema 快照能全部通过等价性断言；余额流水事件矩阵、定点金额规则、canonical ref、action/role slot、零 delta、冲突 fail-fast、未来 writer metadata 白名单和页面展示回退测试全部冻结，Phase 4 不得自行新增 reason/ref 或改变 action 身份。

### Phase 3：用户账号、多分组和模型目录

依次实施：

1. 用户作用域账号 API 与权限；
2. PRIVATE/PUBLIC_POOL/ROOM 模式切换；
3. `api_key_group_routes` 鉴权投影和共享 route Helper；
4. priced catalog 根集合；
5. 模型列表、账号保存和网关终检接入；
6. 等级号池和账号候选附加条件。

门禁：每一步都复用上游 Service/Repository，不先造总抽象再填业务。

### Phase 4：账号广场

依次实施：

1. 房间和 RoomAccount 的简单 CRUD，并原地重接四项房间运营配额；配额失败只拒绝本次创建/编辑，不触发 drain/排空；
2. 按固定锁序实现 Key 加入/离开，写入合法 RoomAccount 兼容锚点，并删除四个与新语义冲突的旧唯一索引；
3. 房间过滤接入上游 scheduler；在既有 lease 之后、Provider Forward 之前增加无新表的短 admission fence，统一收口请求与离开/暂停/关闭/idle 的并发边界；
4. ACTIVE/PAUSED/CLOSED 映射；暂停事务归一双游标并关闭旧低消窗口，恢复事务从 `resumed_at` 新预付/新开窗口，余额不足 membership 原子结束且不阻塞其他合格席位；
5. 房间有效基础倍率与唯一量化请求费用快照；
6. 按当前合同重接小时费：加入一分钟预付，`paid_until/billed_until` 双游标，纯 seat billing worker，以及加入前/请求前共用 catch-up；余额不足或达到用户空闲 cutoff 时直接结清并 ended；
7. 重接一小时低消窗口、15 分钟宽限和迟到补偿；即时豁免退款，迟到豁免按原快照写 `seat_waiver_refund/share_reverse`；
8. 将旧 `StartSeatBillingWorker` 拆成明确的 seat billing/waiver compensation 启动项，不复制 room validation、lifecycle finalizer、orphan binding cleanup 或 queue/drain/ending 编排；
9. 按当前 Pixel 业务语义重接邀请码生成/轮换、统一的注册邀请码消费绑定、独立授权的管理员按 ID 关系维护，以及抽成资格的窄 Service/Repository；
10. PUBLIC_POOL 请求、ROOM 请求与 ROOM 小时费共用政策解析、邀请资格和三方拆分；request 与 seat writer 分别守住原事务和物理事实；
11. 现有 UsageBilling 事务尾部的请求 settlement、号主/邀请人余额、`user_balance_ledger`、`share_accrue` 同事务入账，分层 request/settlement 快照和仅 `40P01` 的全事务重试；
12. seat 事务内原子处理预付、`seat_charge/seat_refund/seat_waiver_refund`、余额/流水、`share_accrue/share_reverse`、双游标和 membership 状态；
13. 请求/小时双 settlement 的单一兼容 Repository，以及 `/affiliate` 所需的有符号期间/累计统计查询。

门禁：不实现 queue、drain、binding worker、通用 ending operation、terms revision 或推荐评论；但必须证明 ROOM 请求可产生 request settlement，小时链可产生且仅产生合法的 `seat_charge/seat_refund/seat_waiver_refund`，迟到豁免只按原快照写必要 `share_reverse`。所有现金流水只能使用 Phase 2 已固化的事件矩阵；worker、加入前 catch-up、请求前 catch-up 重复触发不得重扣、重分或越过 `membership.ended_at`、`listing.paused_at`，或关闭事务为各 membership 写入的 `ended_at`。

### Phase 5：OpenCode、代理和峰谷

依次实施：

1. OpenCode 薄 Provider Adapter；
2. 代理 owner/platform/level eligibility；
3. Channel time-range 到上游值对象的 Repository 适配；
4. Group schedule 作为绝对 `groupDefault` 的 Repository/cache 适配，保持 `PeakMultiplierAt` 原样；
5. 全协议模型/定价一致性测试。

门禁：不复制网关、计费器或代理 Planner。

### Phase 6：商业运营、奖励、提现与发票

依次实施：

1. 复用 `shop_*` 的单商品订单和文本卡密；
2. 余额/现有外部支付与履约，卡密必须在库存锁事务提交前完成解密校验；
3. 统一积分资格/变更 Helper、请求前 balance-or-enabled-points 检查、现有积分优先扣款与积分商城支付；
4. 确定性福利与兑换的余额/订阅/积分/永久并发/临时并发奖励；
5. 明确移除或不注册上游支付履约中的 affiliate 充值返利副作用，并用回归测试证明支付成功不会产生邀请收入、quota 或 `accrue/transfer`；其余支付步骤顺序和事务不变；
6. 复用余额提现状态机，并在原事务补新写余额流水；
7. 四张 `invoice_*` 表、后台导出和 `payment_orders.currency` 新写快照；
8. 发票申请短事务与退款状态 claim/CAS 的同订单锁互斥，Provider I/O 保持在事务外；
9. 按 Phase 2 已固化的现金事件矩阵补齐商业模块事件，不得改名或另建目录；让首版未来 `users.balance` 变更在原业务事务内追加 `user_balance_ledger`，原来只有单条 autocommit mutation 的路径按 P03 在原调用点用单条原子 SQL 或局部短事务收口，并复用同一 list/stats 查询实现服务用户端和管理端；
10. 余额/积分、请求费、小时预付/收费/退款、共享分润与邀请消费抽成、提现、发票的财务后台只读查询。

门禁：支付回调必须具备“不产生充值返利”回归测试；奖励领取、积分扣除、请求/小时邀请消费抽成、小时豁免返冲、提现、退款和卡密履约必须具备并发与幂等测试。所有首版现金余额变更必须证明 `users.balance + user_balance_ledger` 同事务提交；用户/管理 list+stats 必须通过权限隔离、相同过滤、定点精度、未知 reason 和时间边界测试。

### Phase 7：前端兼容重接

依次实施：

1. 先接入当前应用壳、主题 token、route skin 和公共组件，不恢复上游充值返利管理页面；
2. 按原路径重接我的账号、账号广场、使用记录/余额流水、商城、活动/兑换、邀请关系与消费返利、提现、发票和财务后台；账号广场保留倍率、小时价、低消、最低余额、双游标与请求/小时费用明细；
3. `/affiliate` 与活动页邀请入口复用一个组件和服务端数据源，保留当前邀请码、实时到账余额、期间筛选和邀请明细骨架；累计抽成改读 request/seat settlement 的有符号统计；
4. Profile 增加积分明细入口、临时并发数量和最近到期时间；
5. 删除账号广场页面的 queued/ending/draining、消费者 membership 暂停/恢复和排空文案/入口；保留房主/管理员对整间房的 ACTIVE/PAUSED 控制，以及当前 membership 的 `paid_until/billed_until`、低消进度和离开操作；
6. 用户端与管理端余额流水共用一份 `(reason,direction)` presentation 配置，补齐当前已写入但漏展示的 reason，并永久保留未知 reason 原码和 ref 回退；自然日查询统一使用次日零点排他上界；
7. 完成三档响应式、双主题、legacy/v2、键盘、Teleport 和 reduced-motion 回归。

门禁：不复制旧巨型页面业务逻辑，不在前端计算余额、请求价、小时净额、奖励、分润或提现资格，不改变保留路由；管理端 settlement 必须用类型徽标区分 request/seat/refund/waiver。

### Phase 8：本地验收和生产变更准备

产出：

- 全量 Go/前端测试、静态检查和空库 migration 测试；
- 生产快照兼容测试；
- 待执行 DDL 清单、锁风险、预计耗时、回滚和对账 SQL；
- 退役业务表/列的 D1/D2 候选清单，但首次发布不 DROP；四个房间冲突索引仍按独立 DDL 清单和授权处理；
- 发布与回滚 runbook；ROOM seat 只沿用现有单实例服务切换，保证同一种 settlement 不被旧、新 worker 同时处理，不增加维护期排空、候选清零控制面或账号广场生命周期。旧实例完全停止产生 ROOM seat settlement 后、新版本的 seat billing 与 waiver compensation 启动前，必须针对此刻全部可能进入新版 worker 的 legacy 行原样重跑 Phase 0 的 §5.14 全量只读门禁，以消除 Phase 0 后新增历史行造成的 TOCTOU 空窗；该短暂停写只用于取得稳定快照，不等待 15 分钟宽限或 10 分钟补偿周期，也不要求候选数量为零。任一断言失败即阻断 `ROOM seat billing + waiver compensation` 整体切换并执行版本回滚，不能只启动新 charge writer。门禁通过后，新版本对历史 NULL 快照按 §5.14 逐笔解析，对新 charge 直接读取钱包 delta 快照，不迁移、不回填历史；

只有此阶段完成后，才单独请求生产 migration 和部署授权。

## 11. 验收标准

### 11.1 上游行为保护

- Composite exact/prefix/endpoint/priority 结果与上游基线一致；
- 每个 Handler 的阶段矩阵和 characterization test 与固定上游基线一致；不要求不同入口具有同一顺序，也不补入原本不存在的阶段；
- 单 Group 的账号选择、failover、粘性和 Provider Forward 顺序不变；
- UsageBilling 原有效果顺序和事务边界不变；积分优先只在原余额扣费分支内部适配，消费分润写入只追加在原效果之后；
- 支付履约除明确不启用上游 affiliate 充值返利副作用外，订单 claim、支付状态、权益履约和事务顺序与上游基线一致；支付成功不得产生邀请收入、quota、`accrue/transfer` 或 `source_order_id` affiliate 记录；
- 各 Handler 的 `CheckBillingEligibility` 仍位于原阶段；非订阅分支只替换为共享 balance-or-enabled-points 谓词，后续平台额度、Key 限流和 RPM 顺序不变；
- 代理到期仍按上游每源代理事务提交；
- migration 仍使用一个 runner/lock 和逐文件事务，并保留上游 `schema_migrations`/Atlas 元数据语义；
- 不含 Pixel 功能的请求与纯上游基线输出一致。

### 11.2 模型与路由

- 启用 Channel 定价新增/删除模型后，所有模型候选入口同步变化；
- 目录读取失败时所有扩大权限入口失败关闭；
- 一个 Key 的多个 Group 能按既有 priority/weight/cooldown 工作；
- 容量跳过不误触发上游熔断；
- 首字节后不能跨 Group；
- continuation 永远回到原 Group/账号所有者；
- account-stats、通配符和模型映射不能扩大 priced 根集合；
- Public/Upstream/Billing Model 各自保留上游语义，Composite 后的实际目标仍在同一候选交集内；
- 157 个存量跨平台 Route 的逐入口样本与旧行为一致后才允许切流。

### 11.3 三种投放与房间

- 同一账号不能同时 PUBLIC_POOL 和 ROOM；
- `account_external_placements` 是投放模式权威，`accounts.share_*` 与 `account_groups` 只按定义维护兼容投影；
- 同时触及房间与账号的 Pixel 写事务都遵守 listing-first 锁序；非 ROOM 分支锁账号后若发现模式已变 ROOM，必须中止并从正确路径重试；
- ROOM account 与 room 的 owner/platform/level 一致；
- 一个 API Key 不能有两个 active membership；
- 同一用户不能用多个 Key 在同一房间重复占 active 席位；
- 加入写入的 `membership.account_id` 满足现有非空 CHECK 和 room-account trigger，运行时不形成长期账号绑定；
- 8 条旧 queued 记录不占席；删除四个旧唯一索引后不再阻止合法加入，active-only 与 Key 全局唯一约束仍生效；
- 消费者加入在初始一分钟预付与 active 状态同事务提交后立即生效；离开在最终小时结算、未发生预付退款与 ended 状态同事务提交后立即生效；失败时不留下半完成状态；消费者不存在 membership 暂停/恢复；
- 并发测试覆盖 request admission 与离开/暂停/关闭/idle 的所有交错：已通过行锁 fence 冻结快照的请求可完成；只取得 Redis lease 但未通过 fence 的请求在状态提交后必须释放 lease 并拒绝 Forward；idle 在排他锁内看到 lease 或查询失败时不得结束；
- `idle_timeout_minutes=0` 不自动退出；正值达到候选 cutoff 且确认没有 active concurrency lease 后，与主动离开走同一结算事务，按 cutoff 而非 worker 扫描时间停止小时计费，且不产生 queued/ending；有在途长流/WebSocket 或 lease 查询失败时本轮不结束；
- 房间暂停/关闭事务提交后不产生新请求；暂停/关闭时间是明确的小时计费 cutoff，不收不可用期间小时费。收费 membership 暂停后 `paid_until=billed_until=waiver_window_started_at=paused_at` 并清零展示投影，免费/房主自用席位的计费字段仍为空；恢复成功后从 `resumed_at` 新预付且新开低消窗口；提交前已冻结快照的在途请求仍可结算，迟到 usage 只允许触发对应旧截止窗口的幂等豁免补偿；
- 房间无可用账号时不旁路公共池；
- 429、网络错误和账号 failover 不改变 membership；
- active room 时房间倍率覆盖普通用户/Group 默认倍率和 Pixel Group schedule；非房间请求才使用绝对 `groupDefault` schedule，两者均不得通过 `PeakMultiplierAt` 或结算层二次相乘；
- 唯一量化请求费用在 Usage、积分/余额/订阅、Key 统计与 request settlement 中一致，账号配额仍保持上游口径；小时预付/收费/退款不进入这些请求字段；
- `hourly_rate_snapshot=0` 不预付；大于 0 时加入扣一分钟预付且 `paid_until=joined_at+1m、billed_until=joined_at`。worker、join catch-up、request catch-up 并发触发只推进一个合法区间，`billed_until<=paid_until`；余额不足时收口后 ended；
- 一分钟预付、已确认 `seat_charge`、未发生预付 `seat_refund`、低消 `seat_waiver_refund` 可分别对账；消费者 My Spend 满足 `hourly_net_cost = max(0, hourly_charge - hourly_refund - hourly_waiver_refund)`、`total_cost = request_cost + hourly_net_cost`；财务确认收入严格按 `seat_charge - late waiver reversal` 有符号汇总且不得 clamp，普通 `seat_refund` 和即时 waiver 对确认收入为 0；
- 低消所需用量按窗口时长比例计算；即时达标不确认 seat 收入，迟到达标严格引用原 charge 快照冲回，重试不重复退款或 `share_reverse`；
- request fingerprint 覆盖首次 `Apply` 前稳定的 source/membership/listing、RoomAccount 身份 `(listing_id,account_id)`、owner/rate/policy 配置/request consumer charge，且政策快照在首次 Normalize 前附加；实际 inviter、绑定/到期时间、effective ratio 和三方金额只进入首次 request settlement 快照。ROOM request 新行保存非空的 membership/listing 和 period/duration 事实；本次实际 account 只需来自已冻结的 RoomAccount 快照，不与 membership 兼容锚点强制相等。小时费不进入 request fingerprint，以 membership 行锁、双游标、预付业务引用及 waiver reversal 引用幂等；模拟反向消费死锁时只对 UsageBilling 的 `40P01` 最多重试三次完整事务，seat 事务按固定锁序且首版不做无身份自动重放。

### 11.4 商业、发票和财务

- 订单创建、库存锁定、支付回调和履约重复执行不重复扣款/发卡；
- 已展示卡密退款进入人工异常，不重新销售；
- 福利领取和兑换受唯一业务引用保护，余额/订阅/积分/永久并发/临时并发均不重复发放；
- 积分不能为负、不能提现；积分余额变化与 `points_ledger` 在同一事务，积分优先扣费不足部分才使用余额；
- 余额不足但已启用且持有正积分的用户能通过原计费资格阶段并在 UsageBilling 扣积分；积分未启用或积分/余额均不可用时仍在同一阶段拒绝；
- 积分扣尽、积分奖励到账或优先积分偏好变更后，用户级 auth/billing cache 在事务提交后失效，下一请求不会继续使用旧积分资格；
- `effective_concurrency` 等于永久并发加当前有效 grant；多个奖励可叠加，抵达 `starts_at/expires_at` 不经 worker 即生效/失效，缓存不得跨越边界；
- 每个符合资格的 PUBLIC_POOL/ROOM request 最多产生一笔邀请消费抽成，每个已发生 ROOM seat 区间最多确认一次小时抽成；PUBLIC_POOL 只在 `BalanceCost > 0` 的非订阅计费分支邀请人参与，积分全额抵扣仍保留该资格，订阅分支不产生 PUBLIC_POOL 邀请抽成；ROOM request 按正 `TotalCharge`，ROOM seat 按正 `seat_charge`；只有首次结算成功时才在各自事务写邀请人余额、`user_balance_ledger` 和 `share_accrue`，任一失败全部回滚；
- 邮箱、OAuth 的注册邀请码都经过同一个消费/绑定事务；“邀请码非必填”不得绕过已提交邀请码的周期、到期或周额度校验；管理员按用户 ID 的补绑/改绑与有效期重置保留独立授权事务且不消费邀请码额度；首版不存在登录用户自助补绑 API；
- 邀请码过期不影响已完成绑定，邀请奖励有效期届满则后续 request/seat charge 不再抽成；充值、订阅购买、商城下单和兑换交易本身均不产生邀请收入，只有 PUBLIC_POOL/ROOM 已计费请求和 ROOM 已确认小时费参与抽成；
- `aff_quota/aff_frozen_quota/aff_history_quota` 不参与新结算，`/user/aff/transfer` 和上游充值返利管理能力不进入新功能；生产现有 `share_accrue` 历史继续可查；
- 首版装配拆分后的纯 seat billing 和 waiver compensation worker，并保留加入前/请求前 catch-up；不装配旧 room validation/lifecycle finalizer/orphan cleanup 组合 worker。ROOM 同时产生 request settlement 与合法 `seat_charge/seat_refund/seat_waiver_refund`；仅迟到豁免且原邀请收益大于 0 时写 `share_reverse`；
- UsageBilling 首次成功和幂等重放都从已落 request settlement 恢复所有实际获得正额入账的 owner/inviter，并执行提交后余额缓存失效，通知只在首次应用时发送；seat 首次应用直接返回本次受影响用户并失效缓存，no-op 重放不虚构可恢复名单，异常窗口必须由 Phase 0 验证过的有界 cache TTL 收敛，否则在实现前单独决策 outbox；
- `content_encoding` 为空的旧明文卡密可交付，新卡密以 `aes-gcm-v1` 标记并可正确解密；解密失败时库存、订单和交付快照全部回滚；
- 发票 active source 不重复；
- 非 CNY/UNKNOWN 来源不能创建新发票；
- 发票金额严格等于 `pay_amount`，`pay_amount<=0`、任意退款中/成功/部分退款订单均不能新开票；
- 发票申请与各退款状态 claim 在同一订单锁上互斥；已 active 开票来源不能退款，外部 Provider I/O 不进入数据库事务；
- 现有一张 redeem-code 发票保持可查；
- PUBLIC_POOL/ROOM 请求扣款、账号主/有效一级邀请人入账和 request settlement 在同一 UsageBilling 事务中原子提交；ROOM 小时预付、已发生收入确认、退款/返冲和 seat settlement 分别在其 membership 短事务中原子提交；每个正向结算的三方金额之和严格等于该类已确认消费者费用；
- 提现只扣 `users.balance`；一人最多一条 PENDING，提交扣除与取消/驳回释放各自和余额流水同事务，SETTLED 不重复扣款；
- `users.balance`、`users.points_balance`、各自流水、邀请消费抽成、提现和 request/seat settlement 能按已有稳定业务引用对账；无逐笔 settlement 引用的 `share_accrue/share_reverse` 只做用户/期间/有符号金额聚合核验；
- 首版每条未来 `users.balance` 变更都与对应 `user_balance_ledger` 在原业务事务中一起提交；原为单条 autocommit mutation 的路径在原调用点局部原子提交。direction、非负 amount、`balance_after` 和稳定业务引用一致；相同业务重放不重复写流水，历史缺口不伪造回填；
- 请求现金流水只等于实际 `balance_deducted`；全积分或订阅且现金 delta 为 0 时没有消费者现金 debit，积分只进 `points_ledger`，完整 request consumer charge 只进 Usage/settlement。所有未来 writer 的 `cash_delta_8=0` 都不写现金流水。经济与 settlement 三方恒等按 10 位验证；每个未来 writer 先形成唯一有符号 `cash_delta_8`，该值同时驱动 `users.balance DECIMAL(20,8)` 和流水，流水用 10 位字段承载且 `cash_delta_8 = balance_after - balance_before`。新 seat 的 owner/inviter 实际 delta 还按类型矩阵写入同事务的 nullable 8 位钱包快照，0 与 NULL 可区分；迟到返冲优先精确取原快照反数，legacy NULL 按逐 settlement、逐角色的 §5.14 规则解析。最终全量 legacy 证据门禁任一断言不成立时，ROOM seat billing 与 waiver compensation 不得拆开启用；历史流水不要求末两位为 0，stats 金额保持字符串；
- Phase 0 现金事件矩阵中的每个新写事件都有非空 canonical ref、固定 `action_slot` 和固定 `role_slot`。同一提现或商城对象的扣款与释放/退款是不同合法 action；direction/reason/amount 是不可变 payload，不是可以换值后重试的新幂等身份。同 action 同 payload 返回既有事实，不同 payload fail-fast，不能发生“余额已变但流水冲突被忽略”。同一用户兼任多个角色时逐角色保留流水，consumer/owner/inviter 低消正反向使用独立新写 reason，并按原调用顺序形成连续、可复核的 `balance_after`；
- 财务汇总不会把 settlement、余额流水和 `share_accrue/share_reverse` 审计中的同一抽成重复计入收入；跨表重复的 usage/request 被标为异常而不是相加，seat charge/refund/reversal 保持独立事实。

### 11.5 前端兼容

- 所有保留路由和旧书签继续可访问，应用壳、主题 token、明暗模式和 route skin 不漂移；
- `/usage` 保持“请求记录/余额流水”双页签且旧书签可达，`/admin/usage` 保持独立管理员入口；普通用户接口无论传入何种参数都只能返回认证用户自己的流水；
- 余额流水列表与四张统计卡使用完全相同的过滤条件；用户页默认浏览器本地今天及前 6 日，管理页保留最近 24 小时；原始 API 的 `page_size/limit` 优先级和非法值回退、单边 date 开区间、缺省/非法 timezone 使用服务端配置、精确时间必须成对等当前兼容边界均有合同测试。精确时间、方向、原因、ref_type/ref_id、分页和 created_at 排序均可组合，结束自然日用次日零点排他上界且不遗漏最后一秒或小数秒；
- `amount/balance_after` 及 stats 金额均为十进制定点字符串且不丢精度，credit/debit 的正负号、颜色、净变动和变动后余额一致；已知 reason 有统一双语说明，未知历史 reason 仍显示原码及业务引用，用户端与管理端不再复制两份映射；
- 余额流水只统计 `user_balance_ledger`，不把积分、共享 settlement、affiliate 审计、商城流水或提现申请重复相加；API 保持 metadata object 兼容合同，未来 writer 不写敏感值，页面只展示 `(reason,direction)` 白名单字段且未知 reason 不渲染任意 JSON；
- 页面明确统计是“所选期间已记录流水”，历史缺口不被包装成完整钱包总账；当前余额始终以 `users.balance` 为准。用户默认 7 日使用客户端 IANA 时区且包含今天，管理端默认最近 24 小时；list/stats 用同一过滤快照共同刷新，允许两次查询间因并发入账产生短暂差异；
- 360px、768px、1440px 下无关键内容溢出；桌面表格和移动卡片均可完成核心操作；
- legacy 页面不会被全局强制换成 v2，弹窗/Select 等 Teleport 内容继承当前 route skin；
- `/affiliate` 页面壳与 Activities 页签共用一个无布局邀请内容组件和服务端数据源，保留当前指标卡、时间筛选、邀请链接、“实时到账余额”和邀请明细风格；`HeaderInviteLink` 只复用摘要接口；前端不计算比例、抽成资格或可提现额；
- 邀请页面的期间收益、累计收益和被邀请人明细统一来自适配器有符号的 `invite_credit`，包含 request、seat charge 及 waiver reversal；不得以 `aff_history_quota` 展示当前累计收益，不显示冻结额度或转余额入口；
- `/admin/settings` 只管理邀请总开关、消费收益有效期、关系补绑/延长；`/admin/revenue` 是 `invite_share_ratio` 唯一配置入口，旧充值 rate/freeze/cap/batch-rate 不显示、不提交；
- 管理端当前汇总 DTO 和页面只把 `share_invite_credit` 放入当前收入，不得注册或展示上游 `affiliate_rebate/affiliate_transfer` 指标；若 Phase 0 意外发现真实历史，先用独立 legacy 节点或历史接口提出兼容方案，未确认前不得混入当前收入或导出；
- 管理端 settlement 列表以 `settlement_ref=source_table:id:settlement_type` 为全局唯一身份，能够区分 request、seat charge、普通退款和豁免返冲，并可沿 `reversal_of_settlement_ref` 追到原 charge；
- 账号广场源码、界面和文案不再暴露 queued/ending/draining、消费者 membership 暂停/恢复或排空；房主/管理员的整房 ACTIVE/PAUSED 控制仍可用；小时价、低消、最低余额、`paid_until/billed_until` 和请求/小时费用明细继续可见；
- 房间配额页面仍可维护四项 CRUD 限额及房主覆盖/审计，但任何操作都不会触发 drain、排空或 membership operation；
- 积分来源流水、临时并发数量与最近到期时间可见；请求费、小时费/退款、提现、邀请消费抽成、账号主分润和发票的状态均由服务端返回；
- 浅色/深色、键盘操作、44px 触控目标和 `prefers-reduced-motion` 通过回归测试。

### 11.6 数据库兼容

- 生产全部核心表、列、索引、外键、CHECK、trigger 均有明确处理结果；
- 321 条 migration ledger 均归类，零未知 checksum 冲突；
- `atlas_schema_revisions` 的真实状态已归类，并证明 runner 首次接触生产时不会执行未批准写入；
- 首次切换不改变已有用户、API Key、账号、邀请关系、房间、membership、request/seat settlement、订单、支付、提现、发票和财务主键；
- `user_balance_ledger` 现有主键、用户关联、direction/amount、reason/ref、`balance_after`、metadata、时间和历史行原地保留；不因接入新页面或新 reason 改写旧行；
- `account_share_mode_settlement_entries` 只新增 nullable signed `owner_wallet_delta NUMERIC(20,8)`、`invite_wallet_delta NUMERIC(20,8)`；历史行保持 NULL 且不回填，新 charge/迟到 waiver 在原事务写正向/负向实际差额，普通 refund/即时 waiver 写 0/0，零值与 NULL 严格区分。legacy NULL 只按 §5.14 的原 credit、半舍入条件及唯一正向流水符号解析；最终停写快照下的生产 typmod、trigger、writer 来源或所需流水唯一性无法证明时，阻断 ROOM seat billing 与 waiver compensation 整体切换，绝不从历史 10 位 ledger amount 猜 8 位实际 delta；
- 历史 metadata 和现有 DTO object 合同保持不变；Phase 0 抽样安全性，未来 writer 只写 §5.14 允许字段，页面不渲染未知 JSON；业务 Repository 不提供 UPDATE/DELETE 流水接口；
- 不批量重写凭证、卡密、余额、积分、邀请消费抽成/账号主分润、提现流水、订单币种或历史状态；
- 空库从零可创建，生产库 adoption 后 runner 为 no-op 或只执行已批准增量；
- 首次发布不 DROP 业务表或列；唯一允许的 DROP 是四个已核对真实定义、通过查重并另获 DDL 授权的冲突唯一索引。

## 12. 逻辑审查结论

### 12.1 已确认问题

| 问题 | 风险前状态与影响 | 修订后状态与影响 |
|---|---|---|
| 原蓝图重建四内核和总 Planner | 替换上游 Handler/调度/定价，回归面覆盖全部协议 | 删除总编排，只在十一个已列明窄扩展点增量 |
| 把所有入口假定为同一阶段顺序 | WebSearch 与 ChatCompletions 已有反例；统一重排会改变安全、计费与并发语义 | 逐 Handler 建立阶段矩阵，只在原位置增加窄 Helper |
| 原蓝图采用双 migration 执行链 | 两个 runner、锁和 baseline 会改变启动链并制造漂移 | 只保留上游一个 runner/lock，同时按原语义维护 `schema_migrations` 和 Atlas baseline 元数据 |
| 把“单迁移执行链”误写成“单物理账本” | 忽略 `atlas_schema_revisions`，runner 可能在 checksum 前发生未审查写入 | adoption manifest 同时核对逐文件 ledger 与 Atlas 状态，生产接触需单独写授权 |
| 原蓝图采用新双分录 Ledger/opening | 需要资金切换、全量对账和普通请求前新写入，违背本轮最小改动 | 继续使用 `users.balance` 和现有 UsageBilling 事务，不做 opening |
| 原蓝图新增 Composite resolver | 会与上游显式路由、账号归属和 detector 产生双重规则 | 完整复用上游 `CompositeRouteResolver` |
| 把公开模型、上游模型和计费模型混为一个 ID | 别名或映射可能错误扩大目录，或让合法上游计费失配 | 分开 Public/Upstream/Billing Model，Composite 前后分别做交集检查，计费身份仍归上游 |
| 原蓝图新增 ProxyFallbackPlanner | 会改变每源代理事务与提交后动作 | 只给上游 fallback 增加兼容资格纯函数 |
| 原蓝图允许房间混合等级 | 与生产外键、现有 103 个房间和最简需求冲突 | 房间固定同平台同等级，不拆现有约束 |
| 原蓝图设计排空/暂停/退出状态机 | 旧 queue/ending/draining 已形成复杂生命周期 | 业务只暴露 ACTIVE/PAUSED/CLOSED，加入和离开立即生效 |
| 直接结束但请求准入无互斥 | 状态读取、Redis lease 与结束提交之间存在 TOCTOU，可能在 idle 判空或离开提交附近放入一条新请求 | 复用 listing/RoomAccount/membership 行锁形成短 admission fence；已冻结快照者完成，未通过者在提交后重读并拒绝，无需恢复 ending 状态 |
| 房间 PAUSED 的操作者不清 | 容易把房间可用性误实现成消费者席位暂停/恢复，重新引入旧生命周期 | PAUSED 只供房主/管理员控制整间房；消费者 membership 只有加入/离开 |
| 房间加入未定义 `membership.account_id` | 会违反现有非空 CHECK/room-account trigger，或被误解为长期账号绑定 | 加入事务选择一个合法 RoomAccount 作为兼容锚点，运行时仍由上游 scheduler 动态选择 |
| 退出 queue 时漏掉旧唯一索引 | 8 条 queued 历史仍可能阻止合法加入，同一用户跨房间也被旧全局索引误拦截 | 精确删除四个冲突索引，保留 active-only 房间索引和 Key 全局索引 |
| 房间倍率在 `ActualCost` 后再次相乘 | 消费者余额、Key 统计、Usage 与 settlement 可能出现多份金额 | 房间倍率只作为一次有效基础倍率；所有消费者侧效果引用同一量化 charge，账号配额保持上游口径 |
| 分账追加未定义幂等快照和死锁边界 | 重试可能用不同房间请求快照，消费者与号主反向锁序可能触发 `40P01` | Apply 前稳定快照进入 request fingerprint，事务内邀请结果只进首次 settlement；只对 `40P01` 最多三次全事务重试，不重排上游锁序 |
| 原蓝图把发票兑换码默认视为 CNY | `redeem_codes` 没有币种字段，默认会伪造财务事实 | 历史保留；新发票只接受可证明 CNY 的 PaymentOrder |
| 发票金额和部分退款规则不明确 | `amount` 可能包含赠送权益；按剩余额开票会引入拆分与红字逻辑 | 新发票金额只用 `pay_amount`，部分退款订单也一律不可新开票 |
| 假定退款已有覆盖全流程的锁事务 | 实际 Prepare/Execute/Provider I/O 分段，长事务会改变上游顺序并持锁跨网络 | 只给状态 claim/CAS 增加最小订单锁事务和 invoice 条件；Provider 后原单次余额扣减仅在原调用点局部原子写 `balance + ledger`，仍与 `markRefundOk` 分开，Provider I/O 保持在事务外 |
| 卡密只规定加密格式，未规定解密时点 | 提交 sold 后才发现密文损坏会形成已履约但无法交付 | 在库存锁事务提交前解密校验，失败整体回滚；格式由 nullable `content_encoding` 明示 |
| `accounts.share_*`、placement 与 account-group 权威不清 | 多入口可能各写一套模式，继续产生漂移 | placement 是模式权威，`accounts.share_*` 与 `account_groups` 仅作已有路径所需投影 |
| 原蓝图认为多分组是上游能力 | 上游基线没有 `api_key_group_routes` | 明确它是 Pixel 必要增量，但只包裹现有单 Group 流程 |
| 把 Group schedule 接入 `PeakMultiplierAt` | schedule 是绝对倍率，接入 peak 会变成 `base × schedule`，并漏掉标准型 Group | 作为绝对 `groupDefault` 输入现有倍率解析；原生 peak 代码不改且禁止同 Group 双启用 |
| 把积分和奖励排除在核心外 | 生产已有积分余额、积分优先扣款、商城和 362 条流水写入，删除会丢失真实资产能力 | 原地复用 points 字段/ledger，用一个事务感知 Helper 支持福利、兑换、商城和用量扣费 |
| 只在 UsageBilling 增加积分扣除 | 上游请求前 `CheckBillingEligibility` 仍会因余额不足提前拒绝，积分永远到不了扣款阶段 | 在原计费资格调用内部共享 balance-or-enabled-points 谓词，不移动各 Handler 的阶段或后续检查 |
| 用永久并发字段模拟临时奖励 | 到期无法可靠回退，重复奖励会覆盖管理员永久设置 | 新增无 status 的最小 grant 表；认证投影求和，时间到期自然失效 |
| 误把上游充值返利纳入新版 | 会在现有消费抽成之外形成第二套邀请收入；支付成功可能额外写 quota/冻结/转余额，而生产 1,557,472 条流水当前全部是 `share_accrue` | 明确排除 `payment_fulfillment -> affiliate`；只重接当前邀请码/直接关系/有效期和请求/小时消费抽成，旧充值物理字段首发原地不删但新代码不读写 |
| 注册邀请码存在两条绑定路径 | 标准 Consume 路径校验周期/到期/周额度，旧 OAuth `BindInviterByCode` 旁路不完整；可导致同一邀请码因入口不同而行为不同 | 所有用户注册字段复用一个 `ConsumeAndBindInvitation` 事务；管理员按 ID 的补绑/改绑是独立授权用例且不消费邀请码额度；不新增用户自助补绑 API |
| PUBLIC_POOL、ROOM 请求与 ROOM 小时费邀请资格不同 | PUBLIC_POOL 额外要求非订阅分支的 `BalanceCost>0`，ROOM request 按正 `TotalCharge`，ROOM seat 按正 `seat_charge`；笼统写成“所有 usage”会扩大或遗漏支出 | 冻结并测试三种场景资格，但共用一个显式接收场景/时点的 inviter resolver 和 split 实现；waiver reversal 只读原 charge 快照 |
| 误删小时席位费，或原样搬回整包 worker | 仅保留请求费会让 209 个现有小时房间配置失效并切断 seat 收入/退款；直接装配旧 `StartSeatBillingWorker` 又会把 validation、ending、orphan 等复杂生命周期一并带回 | 成组保留一分钟预付、双游标、纯 seat worker、join/request catch-up、豁免补偿及 seat settlement；拆掉 queue/drain/ending 编排，正反向测试同时证明“不漏计费、不复活旧生命周期” |
| 只写“暂停不计费”而不截断游标 | worker 恢复后可能从旧 `billed_until` 补收整个暂停空档，低消窗口也会跨暂停累计 | pause 把双游标和低消窗口统一截到 `paused_at`；resume 从 `resumed_at` 新预付并新开窗口，旧窗口只接受迟到补偿 |
| 动态 RoomAccount 仍套用 membership 锚点强相等 | scheduler 选择同房其他合法账号时会被 UsageBilling 误判快照不匹配，迟到结算也可能因账号已移除失败 | 请求开始冻结既有 `(listing_id,account_id)`；Apply 校验 membership 基础归属，不把兼容 `membership.account_id` 当本次实际账号，也不新增同义 `room_account_id` |
| 空闲退出只比较时间、不看在途 lease | 长流或 WebSocket 仍在运行时可能被释放房间，后续请求突然失去 membership | idle worker 先确认无 active concurrency lease；有在途或查询失败均跳过，本轮不结束 |
| 把消费者预付 debit 与 seat 收入确认混为 `hourly_charge` | 页面退款、财务收入和邀请返冲会互相错减，时间窗结果难以解释 | My Spend 明确按余额流水计算预付 debit 减两类 refund；财务/邀请按 seat settlement 的 charge 减 late reversal，兼容字段注明数据源 |
| 假定 seat no-op 重放能恢复上一笔缓存名单 | seat 没有 request ID 类稳定调用身份，游标已推进后重放拿不到原受益人，文档承诺无法兑现 | 首次应用直接返回受影响用户并失效；Phase 0 验证 cache TTL，有界收敛不满足时阻断并单独审批 outbox |
| PUBLIC_POOL/ROOM/seat 各写一套政策和拆分 | 两张政策、两类 settlement、各处重复 UNION，后续改比例或邀请资格容易产生差异 | `account_share_policies`、inviter resolver 和 split 统一；request/seat writer 各守原事务与物理表，只有一个有符号读取适配器 |
| PUBLIC_POOL 手写拆分、ROOM 调 Helper | 同一比例与舍入规则有两个实现，改一处可能造成场景金额差异 | 两种投放共用 policy、inviter eligibility、split 和 credit Helper，场景只提供快照 |
| 把事务内邀请结果塞进 request fingerprint | 为提前取得 inviter/有效期会在网关与事务内各查一次，形成 TOCTOU；不提前查又无法在幂等 claim 前计算指纹 | request fingerprint 只放 Apply 前稳定字段；事务内邀请结果只写首次 settlement 不可变快照，重放读取该事实 |
| 幂等重放提前返回 | 数据库已经入账但进程可能在缓存失效前中断；重放恢复 credit user IDs 后提前返回，余额缓存仍旧 | 重放不再写账或重发通知，但仍幂等失效结果中的余额缓存 |
| 财务报表同时累加 settlement 与各类流水 | 同一次分润在结算、余额流水和 affiliate 审计各出现一次，直接求和会重复计算收入；waiver 还可能遗漏负向返冲 | request/seat settlement 负责有符号收入统计，余额流水和 `share_accrue/share_reverse` 只做资产/邀请抽成审计；跨表重复 request 标异常，seat reversal 按引用抵减 |
| 蓝图遗漏使用记录中的余额流水 | 只保留 `user_balance_ledger` 表或财务后台会丢失用户 `/usage` 双页签、统计、筛选和旧书签 | 将用户/管理端 list+stats、权限、字段、交互和前端视觉作为核心功能整体承接，余额流水仍不新增导出 |
| 自然日结束使用 `23:59:59` | 后端采用 `< end`，结束日最后一秒及其小数秒可能被列表和统计漏掉 | 日期范围统一转换为 `[当日零点,次日零点)`；精确时间同样 end-exclusive，列表和统计共用过滤器 |
| 未冻结余额流水查询参数的旧兼容行为 | 重建时可能把 `page_size/limit` 优先级、非法值回退、单边日期或缺省/非法时区改成另一套规则，导致旧页面页数或时间窗漂移 | 按现有 Handler 固化参数合同和回归测试：非空 `page_size` 优先、非法所选分页值回退默认、日期允许单边、缺省/非法 IANA 时区使用服务端配置，精确时间必须成对且优先 |
| 用户端与管理端各复制 reason 展示规则 | 新增或历史 reason 容易只在一端补齐，当前已有返冲/历史奖励只能落入不完整展示 | 两端共用单一 `(reason,direction)` presentation 配置，已知码显示双语业务说明，未知码永久显示原码与业务引用 |
| 把余额流水误认为天然覆盖全部余额变化 | 现有部分商城路径只写专用流水，提现路径也存在未写通用流水的缺口；用户会把不完整记录误认成完整钱包历史 | Phase 0 盘点全部现金变更；首版未来写入在原业务事务追加通用流水，原单条 autocommit mutation 按 P03 在原调用点局部原子收口；历史不伪造、不跨表拼接，页面明确只展示实际 `user_balance_ledger` |
| Phase 4 写流水、Phase 6 才冻结 reason/ref | 实现者会临时命名 reason 或复用错误 ref，后续再统一会造成历史码漂移和幂等冲突 | Phase 0 先产出事件矩阵，Phase 2 固化常量、canonical ref、未来写入白名单和页面展示配置；Phase 4/6 只能消费该合同 |
| 把 request consumer charge 直接写成现金 debit | 积分部分抵扣或订阅请求会虚增现金支出，流水 amount 与 `users.balance` 实际变化、`balance_after` 均不一致 | 现金流水只写实际 cash delta；积分写独立流水，零现金变化不写消费者 cash ledger，经济费用仍由 Usage/settlement 表达 |
| 把余额和流水都当作 10 位现金精度 | 生产 `users.balance` 只有 8 位，而流水字段可存 10 位；当前部分 writer 直接把 10 位经济值写流水，可能让末两位与数据库实际余额变化不一致 | 不改余额列；settlement 经济值和 platform 余数保持 10 位。每个未来 writer 先形成唯一 `cash_delta_8` 并同时用于余额与流水，或由同一 SQL before/after 取得；仅新写保证流水末两位为 0，历史原样保留，两套精度分别对账 |
| set/clamp、退款或奖励产生零余额变化 | 若仍插入零金额行，会污染统计笔数、分页和幂等语义 | 所有未来 writer 在 `cash_delta_8=0` 时只保留原领域 claim/no-op 事实，不插入现金流水；历史零金额行继续兼容展示 |
| 先改余额再静默忽略流水唯一冲突，或同一对象的反向动作共用一个逻辑身份 | 业务重放可能二次改余额但没有第二条流水；提现提交/释放、商城支付/退款又可能被误判为同一 payload 冲突 | canonical action 使用稳定 ref 加固定 `action_slot/role_slot`，并优先在余额变化前由领域状态或唯一业务行 claim；同语句实现时冲突必须连同余额变化回滚。同 action 同 payload 读既有事实，不同 payload fail-fast，禁止 `UPDATE` 后裸用 `ON CONFLICT DO NOTHING` |
| 同一用户同时承担 consumer/owner/inviter | 聚合成一笔会丢失角色含义；只把 direction/reason 当幂等身份又可能被换 payload 绕过，分别写但无顺序还会使 `balance_after` 不确定 | canonical action 增加固定 `role_slot`，direction/reason/amount 只作不可变 payload；新写的 consumer/owner/inviter 低消正反向使用独立 reason，用户行先按原规则去重加锁，并按 Phase 0 原调用顺序逐笔写入、链式更新 `balance_after` |
| 历史 owner 低消返冲与 consumer 退款共用 reason | 只按 reason 映射会把 debit 的账号主返冲误显示成消费者退款；历史 metadata 又不保证都有角色字段 | presentation 以 `(reason,direction)` 为键；旧 waiver reason 的 credit/debit 分别显示消费者退款/账号主返冲，新 owner reason 只用于重建后写入 |
| `share_accrue/share_reverse` 没有稳定 settlement 逐笔引用 | 若要求逐行精确关联，需要新增引用列、精度和唯一性设计，超出当前最小兼容范围 | 明确它们只做用户/期间/有符号金额聚合审计；逐笔业务事实和对账使用 settlement 与带引用的 balance ledger |
| 邀请页期间与累计收益使用不同事实 | 当前期间收益来自消费 settlement，但累计卡片读旧 `aff_history_quota`，同页金额可能无法解释；活动页还复制了同一逻辑 | 期间、累计和邀请人明细统一汇总 request/seat settlement 的有符号 `invite_credit`；`/affiliate` 与活动入口复用一个组件和摘要接口，旧 quota 不进入当前展示 |
| 提现只改余额不写流水 | 申请扣款及取消/驳回返还无法从余额流水按申请核对 | 保留现有锁事务，在提交前追加 debit/release 流水；历史不伪造回填 |
| “复杂分销”被误解为无限级代理 | 当前只有单一 `inviter_id`，强行新增层级会引入关系树、逆向退款和历史快照迁移 | 首版明确为一级邀请关系参与 owner/inviter/platform 三方消费分账，不新建多级树 |
| 功能重建顺带重做前端 | 路由、明暗主题、legacy/v2 边界和用户操作习惯会大面积漂移 | 冻结当前应用壳、token、route skin、核心路径与页面骨架，只重接业务状态和数据 |

### 12.2 待确认风险

这些项目不需要再做产品架构设计，但会阻断相应生产变更：

| 风险 | 当前证据 | 实施门禁 |
|---|---|---|
| migration/Atlas 兼容 | 生产 `schema_migrations` 321 行，尚未逐文件与目标 FS 比对；Atlas 有 1 条 `153_shop_file_card_keys` baseline 且无错误，但尚未与目标 runner 计算值比对 | Phase 0 同时生成逐文件 manifest、Atlas hash/baseline 对照和必要 pre-adoption 方案；任何未知项不得启动新二进制 |
| 157 个跨平台 Route | 生产只读聚合已确认，但尚未形成逐入口行为样本 | 首切保留；Phase 3 characterization 通过后才能切流；新建/编辑执行同平台约束 |
| 8 条 queued membership | 旧“live”索引仍可能让历史 queue 阻塞新加入；较早快照曾确认其中 2 个 Key 在旧 live 口径下跨房间 | 新运行态不把 queue 当开放关系；Phase 4 启用加入前须先完成四个冲突索引的批准删除，否则 queued 仍可能阻塞合法写入；是否物理结束记录属于后续数据修正 |
| membership 旧唯一索引 | 全局 consumer 索引禁止同一用户用不同 Key 跨房间；两个 listing-consumer 索引会让 queued 阻止同房间加入；queue-rank 索引保留已退役的队列排名语义 | 查重结果为 0；精确删除四个旧索引，保留现有 active-only 房间索引和 Key 全局索引；实际 DDL 单独审批 |
| 1 个 Key 的主 Group 投影滞后 | `group_id` 属于 enabled routes，但不是稳定排序首项 | 首切不改数据，运行时以 Route 表为准；用户下次保存 Route 时同步投影 |
| Group schedule legacy state | 5 条 state 覆盖全部 5 个 scheduled Group，引用完整；仅 2 条当前 Group 倍率等于保存基础倍率 | Phase 0 按当前时间段和旧 worker 语义核验 3 条差异；未编辑数据继续只读兼容，不自动回填或改值 |
| 卡密新写加密 | 现有 707 条 available 文本卡，未验证新基线密钥配置 | 实施前验证 secret codec 和密钥持久性；不满足则阻断新卡密导入 |
| PaymentOrder 币种 | 现有表没有稳定 currency 列 | 只给新订单写 nullable currency；历史只接受可证明快照，不回填猜测值 |
| Affiliate migration 收养 | 上游 `134_affiliate_ledger_audit_snapshots.sql` 与本地 `154_affiliate_ledger_audit_snapshots.sql` 已核实内容相同但文件名不同；只按文件名会重复执行等价结构 | Phase 0 比对相关文件 checksum、真实后置条件和生产 ledger，按 adoption manifest 处理；这只是 schema/runner 兼容，不授权或启用上游充值返利；未知项阻断 runner |
| 邀请流水 action 差异 | 生产 1,557,472 行当前均为 `share_accrue`，而上游充值返利定义 `accrue/transfer`；小时迟到豁免需要独立 `share_reverse` | 核对生产 CHECK、索引和业务引用；正向抽成只写 `share_accrue`，原 seat 邀请收益返冲只写 `share_reverse`，不得改名/批量转换历史或产生 `accrue/transfer` |
| 请求/小时双 settlement active 事实 | `account_share_mode_settlement_entries` 已确认 usage_request 158,165、seat charge 23,734、seat refund 862、waiver refund 559，seat period 重复组为 0；canonical request 表行数、跨表 request 重复和全部金额恒等仍待 Phase 0 核验 | request 新写启用前完成双表行数、跨表查重与金额校验；mode 表继续 seat 新写；统一适配器按 type/ref 有符号读取，异常只列清单，未经授权不修数据 |
| legacy seat charge 返冲精度 | 旧 writer 的正向流水 amount 可能只是 10 位 economic credit，不一定等于 `users.balance NUMERIC(20,8)` 的实际 delta；直接 CAST 又会在“负余额 + 精确半舍入点”多扣 `0.00000001` | 新 seat 在现有 settlement 行保存 nullable 8 位 owner/inviter wallet delta；legacy NULL 逐 settlement、逐角色解析，非半舍入点 CAST，半舍入点用唯一原正向流水的 `balance_after` 符号恢复 PostgreSQL 结果。Phase 0 建立并首轮运行全量门禁，最终切换在旧实例停写后刷新；证据不足则阻断 ROOM seat billing 与 waiver compensation 整体切换，不排空、不回填、不猜金额 |
| 积分余额与流水 | 已确认无负余额，但尚未逐用户核对 ledger 最后一笔 `balance_after` 和 ref 重复 | Phase 0 用索引友好查询形成差异清单；不自动以流水或余额覆盖另一方 |
| 提现余额口径和历史流水 | 当前实现允许提现全部 `users.balance`；本地调用链未写 ledger，生产模糊查询已超时回滚 | 首版明确保持总余额提现且积分不可提；未来新写补 ledger，历史不回填，Phase 0 仅精确索引核验 |
| 临时并发新表 | 生产无 grant 表，尚未核对是否有旧 metadata/人工流程把临时奖励写入永久并发 | Phase 0 只读检查配置和相关 metadata；不猜测拆分历史 `users.concurrency`，新奖励从切换后开始记 grant |
| 余额流水历史 metadata | 当前 API 为兼容会返回完整 object，页面虽不 dump 未知 JSON，但尚未只读核实历史 key/value 是否含敏感值 | Phase 0 做有界只读抽样；未来 writer 禁止写敏感值。若历史样本确认泄露风险，另提保持客户端兼容的服务端脱敏方案并经确认后实施，不在本轮暗改响应 |
| 前端视觉基线 | 已确认现有栈、route skin 和公共组件，但尚未形成全核心页多尺寸截图基线 | Phase 0 固定路由/主题/尺寸基线；Phase 7 逐页视觉与交互回归，未通过不得切换 |
| 退役表 | 已知模块多、依赖尚未形成完整表级清单 | 首发不删；后续只读依赖审查、归档和单独 DDL 审批 |

### 12.3 误判撤销

- 撤销“需要独立 Pixel migration runner/ledger”的判断；
- 撤销“为了统一必须建立新 double-entry Ledger”的判断；
- 撤销“所有协议必须通过一个 DispatchPlanner”的判断；
- 撤销“所有 Handler 的安全、计费、并发阶段具有同一先后顺序”的判断；
- 撤销“需要新的 Composite/Route Scope/Supply Pool/RuleGraph”的判断；
- 撤销“房间必须有不可变条款版本和持久化 OpenRoomBinding”的判断；
- 撤销“生产峰谷表可以直接删除并切到上游字段”的判断；
- 撤销“Pixel Group schedule 可以作为 `PeakMultiplierAt` 额外因子”的判断；
- 撤销“上游迁移执行链只有一张物理元数据表”的判断；
- 撤销“兑换码可默认按 CNY 开票”的判断；
- 撤销“积分、临时并发、提现和邀请消费抽成不属于新版核心”的判断；
- 撤销“为了简化账号广场应取消席位小时费、预付、低消豁免及返冲”的判断；
- 撤销“采用上游基线就应保留上游充值返利模块”的判断；
- 撤销“复杂分销必须等于新建多级代理树”的判断；
- 撤销“重建功能时可以一并重做前端视觉”的判断。

## 13. 已冻结的产品决策

| 主题 | 决策 |
|---|---|
| 账号投放 | 每个用户账号仅一种 PRIVATE/PUBLIC_POOL/ROOM 模式；placement 是权威，旧字段与 account-group 仅作兼容投影 |
| 多分组 | 一个 Key 可有多个 Group route；主 Group 保留兼容投影 |
| Route 平台 | 新建/编辑的 Route 集合同平台；157 个生产存量集合首切原样兼容，不自动改数据 |
| 模型目录 | active Channel priced union 为根，随后求能力交集，失败关闭 |
| 房间归属 | 一个 API Key 整体最多一个 active 房间 |
| 用户占席 | 同一用户可用不同 Key 加不同房间；同一房间最多一个 active 席位 |
| 房间平台/等级 | 单平台、单账号等级 |
| 房间生命周期 | 房主/管理员控制 ACTIVE/PAUSED/CLOSED；消费者 membership 对外只有加入/离开，无 queue/drain/ending operation 或席位暂停恢复；技术/事务失败整体回滚，余额不足是显式业务终态，结清后原子 ended；成功即生效 |
| 房间计费 | 请求计费与席位小时费并存：active room 的倍率只作为上游请求定价的一次有效基础倍率；小时费按 membership 快照一分钟滚动预付，以 `paid_until/billed_until` 推进，纯 worker 与 join/request catch-up 共用逻辑；低消豁免及迟到补偿保留，暂停/离开后不继续计时 |
| 房间分账 | request charge 与已确认 seat charge 分别按账号主、有效一级邀请人、平台三方拆分；seat waiver 按原快照逆向；号主/邀请人净收益进入现有余额并可按余额提现规则申请提现 |
| 公共号池 | Group/用户统一定价，号主不能自定义倍率；与 ROOM 共用 `account_share_policies`、邀请资格和三方拆分；只参与 request settlement，不产生席位小时费 |
| OpenCode | 独立产品平台、薄 Provider Adapter |
| 代理 | 平台共享或用户专属；兼容备用；无备用不直连；不自动回切 |
| 商城 | 单商品、文本卡密、余额与现有外部支付；新卡密用 `content_encoding=aes-gcm-v1`，履约提交前解密校验；无购物车/新 Provider |
| 福利 | 确定性活动；奖励支持余额、订阅、积分、永久并发和临时并发，不保留随机抽奖新写 |
| 兑换 | 复用现有兑换码；支持余额、订阅、积分、永久并发和临时并发，不建通用奖励引擎 |
| 积分 | 沿用 `users.points_balance + points_ledger`，可奖励、商城支付和优先抵扣；永久有效、不可提现、不可兑换现金 |
| 临时并发 | 独立 grant、可叠加、按 `[starts_at,expires_at)` 生效；不改永久并发、不跑到期 worker |
| 邀请消费返利 | 以当前 Pixel 共享消费链路为准：保留邀请码策略、单级 inviter、绑定与奖励有效期；PUBLIC_POOL request 仅非订阅计费分支的 `BalanceCost>0`（积分抵扣不取消资格），ROOM request 按正 `TotalCharge`，ROOM seat 按正 `seat_charge` 使用全局 `account_share_policies.invite_share_ratio` 抽成并写 `share_accrue`；迟到 seat waiver 仅按原快照写必要 `share_reverse` |
| 上游充值返利 | 不采用；支付履约不产生邀请收入，不启用 quota/冻结/成熟/转余额、个人充值返利比例、`source_order_id` 或 `accrue/transfer`；旧物理列原地不删但新代码不读写，当前未发现对应生产业务历史 |
| 复杂分销边界 | 首版是一级邀请关系参与 owner/inviter/platform 多场景三方消费分账；不实现多级代理树 |
| 提现 | 保留当前 `users.balance` 总余额提现、收款码、申请/取消和后台结算/驳回；积分不可提，不新增可提现子账户或自动打款 Provider |
| 发票 | 保留完整简单闭环；新申请仅无退款的 CNY PaymentOrder，金额取 `pay_amount`，部分退款也不可新开票；历史原样保留 |
| 发票/退款事务 | 退款状态 claim/CAS 是唯一跨步骤、状态编排事务例外；Provider 成功后的原单次余额扣减仅在原调用点局部原子写 `balance + ledger`，不与 `markRefundOk` 合并。其余 Prepare、扣减、Provider I/O、finalize 顺序和边界不变，网络 I/O 永远在事务外 |
| 资金事实 | `users.balance` 与 `users.points_balance` 各自为事实字段；请求扣费沿用 UsageBilling，小时预付/结算沿用独立 membership 短事务及现有审计流水；不建通用新 Ledger/opening |
| 使用记录/余额流水 | 保留 `/usage` 请求记录与余额流水双页签，以及 `/admin/usage` 同源管理查询；`users.balance` 是现金事实，`user_balance_ledger` 是不可变审计；未来 writer 以唯一 `cash_delta_8` 同时驱动钱包和流水，零 delta 不写。事件矩阵在 Phase 0/2 先冻结 canonical ref、action/role slot 和不可变 payload，未来首版现金变更在原事务或原调用点局部原子写流水，历史缺口不回填、不跨账本拼接；new seat 在现有 settlement 保存 owner/inviter 实际 delta，legacy NULL 只读解析并以最终停写快照的全量证据作为 ROOM seat billing 与 waiver compensation 整体启用门禁；metadata object 与现有查询参数兼容合同保留，未来写入与页面展示使用白名单 |
| 前端 | 保留现有 Vue/Tailwind 栈、应用壳、主题 token、legacy/v2 route skin、核心路径与页面骨架；旧业务逻辑不得整块复制 |
| 生产承接 | 原库原表优先，不建空库搬核心数据 |
| 峰谷 | Channel time-range 适配上游价格对象；Group schedule 是绝对 `groupDefault`，不接 `PeakMultiplierAt` |
| 迁移 | 上游单目录、单 runner、单 lock；保留 `schema_migrations` 与 Atlas baseline 两类上游元数据 |
| 表删除 | 首次发布不删业务表/列；四个冲突唯一索引是唯一必要例外且须单独 DDL 授权；业务表后续按独立退役清单处理 |

## 14. 实施起点

文档确认后，实施只从 Phase 0 和 Phase 1 开始，第一批代码只包含：

1. 创建精确上游工作树；
2. 固定上游原始测试基线并输出逐 Handler/协议入口阶段矩阵；
3. 修复 migration Goose Up/Down 的窄问题；
4. 建立生产 schema、`schema_migrations`、Atlas baseline、未启用上游 affiliate migration 的同结构重命名碰撞和新增核心表的只读 adoption manifest 工具；
5. 输出现金余额变更调用点、`user_balance_ledger` 事件矩阵（`cash_delta_8` 来源、reason/ref、action/role slot、不可变 payload、业务 claim/唯一键、零 delta 结果、原事务/顺序、未来写入与页面 metadata 白名单）与历史缺口，以及 legacy seat 的 typmod/trigger/writer 来源/半舍入点流水唯一性、积分/提现/邀请流水、request/seat 双 settlement、小时配置/双游标/worker/reversal 和临时并发旧痕迹的索引友好只读核查清单；
6. 固定现有前端路由、route skin、主题、公共组件与多尺寸视觉基线；
7. 输出“上游文件 -> Pixel 最小 patch -> 回归测试 -> 是否触碰调用顺序/事务”的逐文件清单。

上述门禁形成可复核结果以前，不开始 Phase 2 及之后的账号广场、使用记录/余额流水、奖励、邀请消费返利、提现、商城、前端重接或财务业务代码，也不连接生产执行任何写操作。需要 pre-adoption、DDL、索引删除或迁移记账时，必须另列精确 SQL、影响、锁风险与回滚方式，再单独取得生产写授权。
