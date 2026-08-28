# “有个想法”用户经验分享板块实施方案

**文档状态**：方案审阅版，仅用于确认设计，不执行代码修改和数据库迁移  
**生成日期**：2026-08-28  
**预计复杂度**：高（内容审核、对象存储、账本、提现和后台治理同时涉及）

## 1. 方案结论

“有个想法”建议作为一个独立的内容分享领域建设，而不是把文章当作账号广场评论的扩展。它可以复用认证、设置、AI 调用、OSS/S3 客户端、用户余额/积分和管理员认证等基础设施，但文章、审核任务、互动、附件、打赏和审计数据必须使用独立的业务表、服务和接口。

本方案对你提出的关键问题给出以下建议：

| 问题 | 推荐结论 | 原因 |
| --- | --- | --- |
| 是否只允许登录用户阅读 | 是。列表、详情、标签页、附件访问全部要求登录 | 内容属于站内社区，便于控制滥用、统计有效阅读和保护作者；后端路由不能绕过认证 |
| 余额打赏是否允许作者提现 | 是。打赏金额直接结算进被打赏人的 `users.balance`，走现有提现逻辑 + 人工审核，不加独立子账本 | 与现有提现行为一致（现有提现本就是扣 `users.balance` 混合池、靠人工审核把关）；不需要来源隔离子账本，也不新增“可提现余额”概念 |
| 文章是否全部人工审核 | 不建议全部人工审核 | 成本高、发布延迟大、审核积压明显；采用“AI 低风险自动通过，高风险人工审核，AI 失败保持待审核”的闭环更成熟 |
| AI 是否复用评论审核 | 复用同一套 endpoint/API Key/model 配置和 HTTP 调用能力，但使用独立 prompt、任务表和结果表 | 保持现有账号广场评论审核行为不变，避免两种业务状态相互污染 |
| 图片和附件保存位置 | 使用独立的私有 OSS/S3 兼容存储配置 | 不复用收款码 OSS 配置；文章附件不应公开暴露对象 key |
| 是否提供评论交流 | 不提供，作为硬边界 | 不创建评论、回复、赞赏留言等表和 API，前端也不展示评论入口 |

## 2. 已确认的产品规则

1. 板块名称固定为“有个想法”。
2. 只有已登录用户可以阅读、发布、点赞、收藏、打赏和举报。
3. 用户可以发表文章、经验、建议、教程和个人见解。
4. 文章不提供评论、回复、楼中楼、讨论串和赞赏留言。
5. 其他用户可以点赞、收藏、积分打赏、余额打赏和举报；积分与余额打赏均为正式产品能力。
6. 文章作者可以为文章添加标签，但标签需要经过治理，不能任意制造同义词和敏感词。
7. 文章发布和修改都需要经过审核；已发布文章修改时不能直接覆盖线上版本。
8. 低风险内容由系统内置 AI 自动通过；高风险内容进入人工审核；审核服务异常时不得自动公开。
9. 图片和附件使用 OSS/S3 兼容对象存储，默认私有访问。
10. 该功能是额外开发，必须与账号广场、网关、支付、提现、现有评论审核等功能隔离。

## 3. 范围与明确非目标

### 3.1 本期范围

- 登录用户浏览、搜索、筛选和阅读文章。
- Markdown 文章编辑、草稿、发布、修改、软删除。
- 标签、点赞、收藏、去重浏览统计、举报。
- 积分打赏和余额打赏；两种资产使用同一套赞赏幂等/审计框架，但使用各自的账本类型和额度策略。
- AI 审核、人工审核队列、审核日志、文章版本审核。
- 私有 OSS 附件上传、鉴权访问、短时预签名 URL、清理和后台监控。
- 管理员内容治理、标签治理、附件治理、赞赏审计和基础运营数据。

### 3.2 明确不做

以下内容在第一版不实现，也不应通过“临时字段”或隐藏接口预留成半成品：

- `idea_comments`、`idea_replies`、讨论串、评论通知。
- 赞赏留言、匿名打赏、站外打赏链接。
- 公开未登录阅读。
- 直接把文章投稿混入账号广场评论表或网关内容审核日志。
- 直接把现有混合余额池全部标记为作者可提现余额；只有“有个想法”产生且可追溯的作者收益才具备提现资格。
- 直接复用收款码 OSS 的 bucket、公开 URL 或权限模型。
- 失败时自动绕过审核、绕过附件鉴权或绕过账本校验的 fallback。

## 4. 领域边界和整体架构

### 4.1 模块边界

新增 `Ideas` 领域，分为以下层次：

```text
HTTP 路由/Handler
        |
Ideas Service（文章、审核、互动、打赏、附件、举报）
        |
Ideas Repository（文章/版本/标签/互动/账本业务记录）
        |
PostgreSQL + Redis + 私有 OSS/S3
```

可复用但不改变业务语义的基础能力：

- JWT 认证和管理员认证。
- `SettingService` 的系统设置读取、加密字段保存和审计回调。
- 现有 S3 SDK v2 客户端工厂模式。
- 通用 AI 审核 HTTP 客户端；账号广场评论仍保留原有 prompt、状态和测试。
- 现有用户余额/积分缓存失效能力，但赞赏必须在自己的事务内完成账本变更。
- 现有提现后台审核框架，但要增加“收益来源/冻结期/可提现余额”的明确边界。

### 4.2 关键隔离原则

- 所有用户 API 放在已认证的 `/api/v1` 路由组下，并继续经过 `BackendModeUserGuard`，不增加公开白名单。
- 所有管理员 API 放在 `/api/v1/admin/ideas` 下，继续使用现有管理员认证。
- 文章审核任务和账号广场评论审核任务使用不同业务表、不同 worker 名称和不同日志前缀。
- 文章附件使用 `ideas/{user_id}/{post_id}/{asset_uuid}.{ext}` 前缀，禁止与收款码、商城文件共用前缀。
- 赞赏事务只写专用业务记录和现有统一账本，不修改账号广场结算逻辑。
- 任一新功能开关关闭时，旧功能路由、设置和数据行为保持原样。

## 5. 信息架构和用户端体验

### 5.1 导航入口

建议在普通用户侧边栏的“我的账户”区域加入“有个想法”，并在顶部操作区加入可选快捷入口。截图中红框位置是现有顶部 Flex 布局产生的空白区域，不是现成插槽；实现时应在 `AppHeader.vue` 的现有操作区中按 Flex 顺序加入入口，不能使用绝对定位覆盖其他控件。

涉及文件：

- `frontend/src/components/layout/AppSidebar.vue`
- `frontend/src/components/layout/AppHeader.vue`
- `frontend/src/router/index.ts`
- `frontend/src/utils/featureFlags.ts`

### 5.2 页面和路由

推荐路由：

| 路由 | 页面 | 权限 |
| --- | --- | --- |
| `/ideas` | 文章广场，最新/热门/精选切换、搜索、标签筛选 | 登录用户 |
| `/ideas/new` | 新建文章、Markdown 编辑、附件上传、标签选择 | 登录用户 |
| `/ideas/:id` | 文章详情、作者摘要、点赞/收藏/打赏/举报 | 登录用户 |
| `/ideas/:id/edit` | 编辑文章新版本 | 文章作者、管理员 |
| `/ideas/mine` | 我的草稿、已发布、待审核、被拒绝和收益概览 | 登录用户 |
| `/ideas/tags/:slug` | 标签文章列表 | 登录用户 |
| `/admin/ideas` | 管理员内容列表和筛选 | 管理员 |
| `/admin/ideas/moderation` | AI/人工审核队列 | 管理员 |
| `/admin/ideas/reports` | 举报处理 | 管理员 |
| `/admin/ideas/tags` | 标签治理 | 管理员 |
| `/admin/ideas/assets` | OSS 附件与孤儿文件治理 | 管理员 |
| `/admin/ideas/rewards` | 赞赏、冻结、逆转和对账 | 管理员 |

### 5.3 广场列表

默认列表只返回 `published` 且未软删除、未下架的当前版本。建议提供：

- 最新：按公开时间倒序。
- 热门：点赞、收藏、有效浏览和近期增长的加权排序；公式写入服务层，不在前端计算。
- 精选：管理员设置精选标记后进入，需显示精选标识。
- 标签筛选：按规范化 slug 查询。
- 关键词搜索：标题、摘要和正文建立 PostgreSQL 索引；全文搜索初期不引入新搜索服务。

列表卡片显示：标题、摘要、作者公开昵称、发布时间、标签、阅读数、点赞数、收藏数；不显示任何评论数和评论入口。

### 5.4 文章详情

- Markdown 渲染使用现有 `marked`，输出必须经过 `dompurify` 清洗。
- 禁止原始 HTML、脚本、iframe、外链自动执行。
- 图片和附件只渲染后端返回的短时授权 URL。
- 展示文章当前公开版本、审核通过时间和作者信息；不公开审核内部原因。
- 底部仅提供点赞、收藏、积分打赏、余额打赏（开关打开时）、举报、分享复制链接，不出现评论区。
- 对被隐藏、删除或作者无权查看的文章统一返回明确的业务错误，不泄露正文和附件地址。

### 5.5 编辑和版本

- 草稿可以随时保存，不进入公开列表。
- 首次发布创建 `pending_review` 版本。
- 已发布文章再次编辑时创建新 revision，状态为 `pending_revision`；审核期间继续展示旧版本。
- 新版本通过后原子替换当前公开版本；新版本拒绝则旧版本继续可见。
- 作者只能编辑自己的文章，管理员可在审计授权下编辑或隐藏。
- 删除使用软删除，保留审计、账本和举报关联数据。

## 6. 内容状态机

```text
draft
  | 发布
  v
pending_review --AI低风险通过--> published
      |                         |
      | AI高风险                | 作者编辑
      v                         v
manual_review <------------- pending_revision
      | 通过                    | 通过
      v                         v
published <---------------- approved_revision
      |
      | 管理员下架
      v
hidden

pending_review / manual_review --拒绝--> rejected
任意非 deleted 状态 --作者/管理员删除--> deleted（软删除）
AI 调用异常 --> moderation_failed（继续人工可见队列，不自动公开）
```

状态约束：

- `published` 必须存在审核通过的当前 revision。
- `pending_revision` 不得替换 `published` 的公开内容。
- `moderation_failed` 只能由重试或人工审核推进，不能由定时任务自动发布。
- `hidden`、`rejected`、`deleted` 的附件 URL 不得再签发给普通用户。
- 所有状态变化写入不可变 `idea_moderation_events` 或 `idea_post_audit_events`，包括操作者、原因、来源 IP、请求 ID 和时间。

## 7. AI 审核和人工审核方案

### 7.1 配置复用策略

现有账号广场评论审核使用：

```text
account_share_comment_review_enabled
account_share_comment_review_url
account_share_comment_review_api_key
account_share_comment_review_model
```

建议抽取通用的审核客户端和配置读取层，但保留账号广场现有服务接口、prompt、状态表和测试不变。“有个想法”默认读取同一套 endpoint/API Key/model，增加独立的：

```text
ideas_enabled
ideas_publish_enabled
ideas_moderation_enabled
ideas_moderation_high_risk_action
ideas_moderation_max_attempts
```

不要复制保存第二份 AI 密钥；如果未来需要不同模型，再扩展显式的 `ideas_moderation_*` 配置并通过后台审计保存。

### 7.2 建议的模型输出

文章审核需要三态，而不是沿用评论审核的二态：

```json
{
  "decision": "pass | review | reject",
  "risk_level": "low | medium | high",
  "reason": "简短中文原因",
  "categories": ["spam", "illegal", "privacy", "abuse"]
}
```

- `pass + low`：自动发布。
- `review + medium/high`：进入人工队列，不公开。
- `reject`：进入拒绝状态，并向作者展示经过脱敏的原因。
- 超时、HTTP 非 2xx、JSON 无法解析、字段不合法：记为 `moderation_failed`，保持不可见并按退避策略重试。
- AI 返回的内容不得直接作为 HTML 或管理员指令执行。

### 7.3 审核任务处理

- 独立 worker 使用集群租约，避免多实例重复处理。
- 任务领取、尝试次数、下次重试时间和最后错误写入审核事件。
- 保存 model/url 快照、prompt 版本、输入内容 hash，避免配置变化后无法追溯。
- 文章正文过长时按明确的截断策略处理，并记录截断标记；不能静默丢失关键内容。
- 审核队列支持管理员批量通过、拒绝、隐藏，但批量操作必须保留逐条审计。
- 人工审核页面展示标题、正文、附件缩略图、作者风险摘要和 AI 原因，不展示作者私密字段。

### 7.4 与账号广场评论审核的回归边界

抽取通用客户端时必须保证：

- 账号广场原有 `pass/reject` 行为和 prompt 不变。
- 评论审核失败仍按现有重试和状态语义执行。
- 文章审核不会写入账号广场评论表，也不会影响账号广场评论 worker 的租约、批量大小和停止流程。
- 账号广场相关单元测试、集成测试和人工回归全部通过后，才允许打开文章 AI 审核开关。

## 8. 标签治理

### 8.1 用户侧规则

- 每篇文章最多 5 个标签；标签长度、字符集和总字数设置硬上限。
- 标签输入先 trim、Unicode 规范化、大小写归一化，再生成 slug。
- 普通用户只能从已存在的标签中选择，或提交“待审核标签申请”；不直接在文章发布事务中创建无限新标签。
- 标签选择器提供热门、最近使用和搜索。

### 8.2 管理侧规则

管理员可以创建、重命名、合并、停用和设置标签排序；合并必须在事务中迁移关联并保留旧 slug 重定向。

敏感词、广告词、联系方式等标签进入黑名单，命中后文章进入人工审核。标签治理记录操作者、前后值和影响文章数。

## 9. 点赞、收藏和浏览统计

### 9.1 点赞

- `idea_post_likes` 使用 `(post_id, user_id)` 唯一约束。
- 重复点赞必须幂等返回当前状态，不能增加计数。
- 取消点赞只删除自己的关系，不影响文章其他统计。
- 文章计数使用数据库原子更新或异步聚合，禁止先读后写覆盖并发结果。

### 9.2 收藏

- `idea_post_favorites` 使用 `(post_id, user_id)` 唯一约束。
- 收藏不改变公开热度分值，单独用于用户“我的收藏”。

### 9.3 浏览

- 只有登录用户计为有效浏览。
- 同一用户对同一文章在短时间窗口内去重，防止刷新刷量；Redis 只做去重加速，最终统计需可从数据库事件或聚合表重建。
- 记录 user、post、revision、时间、请求 ID 的最小必要字段，避免存储完整 User-Agent 等不必要隐私。
- 管理员可以查看异常增长，但不能通过前端接口直接修改浏览数。

## 10. 积分打赏和余额打赏

### 10.1 已确认的打赏产品策略

你已确认积分打赏和余额打赏都打开。作者端体验要求是：余额打赏成功后，作者收益直接结算到可提现余额，可以直接发起提现；不设置常规等待冻结期。

工程实现仍必须保留独立作者收益子账本、账务来源标记、幂等键、事务锁、风控冻结和违规逆转能力。这里的“直接结算”定义为正常订单在同一笔数据库事务提交后立即进入 `available`，而不是把所有用户现有混合余额都视为可提现余额，也不是跳过异常订单的风控和审计。产品发布目标是两种打赏同时开启，系统仍保留独立开关以便故障时快速关闭单一资产类型。

积分是不可提现的平台积分；余额打赏属于可提现的作者收益，二者在 API、账本原因码、限额和对账中必须明确区分。

### 10.2 通用赞赏规则

- 不允许给自己打赏。
- 单笔、单日、单文章和单用户累计额度可配置。
- 赞赏必须携带客户端幂等键；同一幂等键重复请求返回同一业务结果。
- 使用稳定的付款人/收款人锁顺序，避免死锁。
- 事务内完成：赞赏记录占用、付款方扣减、作者收入增加、双方流水、缓存失效；任何一步失败整体回滚。
- 禁止使用“先更新余额，再 `ON CONFLICT DO NOTHING` 写流水”的非原子路径。
- 违规文章下架时，按业务规则冻结后续收入；是否逆转已结算收入必须由管理员操作并留下审计记录。
- 赞赏页面不提供留言字段，接口也拒绝未知的 message/comment 参数，防止后续误加评论能力。

### 10.3 积分账本字段

`idea_post_rewards` 至少保存：付款用户、作者、文章和 revision、资产类型、数量、业务幂等键、状态、创建时间、逆转时间和审计元数据。积分变更复用现有积分事务能力，但需要新增明确的 `idea_reward` 原因码和专用唯一约束。

### 10.4 余额打赏及未来提现

余额打赏使用以下账务模型：

```text
asset_type       balance | points
source_type      recharge | affiliate | activity | idea_reward | adjustment
gross_amount     付款金额
platform_fee     平台服务费
author_amount    作者净收入
withdrawable_at  可提现时间；正常订单为入账事务提交时间
settlement_status pending | available | frozen | reversed
reversal_status  none | pending | completed
```

已确认规则和必须保留的安全边界：

- 只有 `idea_reward` 产生的作者净收入进入可提现收益；充值余额、赠送余额、邀请收益和其他活动余额不能因为该功能上线而自动获得提现资格。
- 正常余额打赏在扣款、作者入账、收益记录、双方流水和幂等记录全部成功提交后，立即将作者净收入标记为 `available`，`withdrawable_at` 记录同一事务的提交时间。
- 被举报、风控命中、作者账户冻结、文章违规下架或订单争议时，管理员可以将相关收益转为 `frozen`，必要时执行 `reversed`；这属于异常治理，不改变正常订单“立即可提现”的产品体验。
- 现有 `users.balance` 不能单独代表可提现余额；需要 `idea_author_earnings` 等独立可追溯子账本，并在提现时校验收益来源。
- 提现申请必须指定或由服务端推导 `idea_reward` 来源，不能从混合余额池任意扣款；提现扣减、提现记录和收益子账本必须在同一事务中保持一致。
- 兼容现有 `/api/v1/user/withdrawals` 和 `/api/v1/admin/withdrawals` 时，增加来源、可提现额度、冻结状态和对账字段，但不能改变已有充值余额提现的历史语义。
- 平台费率、最低提现额、每日/月限额、支付渠道和收款资料仍需使用系统设置配置；在这些参数缺省或非法时，提现接口应明确失败，不能使用隐式默认值绕过资金规则。
- 每笔余额打赏和提现都要支持完整对账：付款方扣款、平台手续费、作者可提现、实际提现、退款/逆转。

## 11. OSS/S3 附件方案

### 11.1 存储配置

“有个想法”使用独立设置命名空间，建议字段包括：

```text
ideas_oss_enabled
ideas_oss_endpoint
ideas_oss_region
ideas_oss_bucket
ideas_oss_access_key_id
ideas_oss_secret_access_key（加密保存，仅返回 configured 标记）
ideas_oss_prefix
ideas_oss_force_path_style
ideas_oss_presign_expire_seconds
ideas_oss_image_max_size_bytes
ideas_oss_attachment_max_size_bytes
ideas_oss_max_assets_per_post
ideas_oss_allowed_mime_types
ideas_oss_storage_quota_bytes
```

不要复用收款码 OSS 的公开 URL 字段。对象 key 示例：

```text
ideas/{user_id}/{post_id}/{revision_id}/{asset_uuid}.{safe_ext}
```

### 11.2 上传流程

1. 用户先创建草稿或取得文章编辑会话。
2. 后端校验 MIME、扩展名、文件大小、用户配额和文章状态。
3. 后端生成短时上传凭证或由后端代理上传；客户端不得自行决定对象 key。
4. 上传完成后调用确认接口，后端读取对象头并进行魔数校验，写入 `idea_post_assets`。
5. 只有绑定到有效 revision 的资产才允许在正文中引用。
6. 文章删除、版本替换或上传中断后，异步清理未绑定对象；清理失败进入重试队列，不静默丢失。

按“限制适中、体验不压迫”的产品要求，建议默认值为：单篇最多 9 张图片、3 个普通附件；单张图片不超过 10 MB，单个普通附件不超过 50 MB，正文最多 20,000 个汉字/字符。管理员可以在后台调整这些产品额度，但不能关闭服务端 MIME、扩展名、魔数和图片解码安全校验。

首期只支持常见图片和明确白名单附件类型；SVG、HTML、可执行文件、宏文档和未知二进制默认禁止。图片需要限制像素面积、帧数和解码内存，避免压缩炸弹。以上是“不过度严格”的内容额度建议，安全类型校验仍然必须严格执行。

### 11.3 访问模型

- bucket 默认私有。
- 普通用户访问文章附件时，后端先验证登录态、文章可见状态和资源归属，再签发短时 GET presigned URL，或通过鉴权代理流式读取。
- 作者可查看自己的草稿附件，管理员可在审核页面查看待审附件。
- URL 不包含长期公开 token，不把 access key、secret key 或原始对象 key 返回前端。
- 文章下架、删除、作者被封禁或资源解绑后，立即停止签发新 URL；已发出的短时 URL 通过短过期时间降低风险。

### 11.4 后台管理

后台需要提供：

- OSS 开关、连接配置、连接测试和配置审计。
- 当前对象数量、估算用量、按用户/文章的占用排行。
- 失败上传、删除失败、孤儿文件扫描和重试。
- 按时间、用户、文章和 MIME 类型筛选资产。
- 单个资源隔离、批量清理和清理结果导出。
- 凭据轮换提示；保存 secret 时只显示“已配置”，禁止回显明文。
- 设置变更前后的差异、操作者、时间、请求 ID。

数据库备份不会自动包含 OSS 对象。正式发布前需要明确对象存储的版本控制、跨区域备份、生命周期和灾难恢复责任；否则只能保证元数据可恢复，不能声称文章附件完整可恢复。

## 12. 数据模型设计

建议新增以下独立表。字段名称可在 Ent schema 和项目现有命名规范下调整，但约束语义不能削弱。

| 表 | 核心字段和约束 |
| --- | --- |
| `idea_posts` | `id`、`author_user_id`、`current_revision_id`、`status`、`slug`/短 ID、`published_at`、计数缓存、`deleted_at`、版本号；作者和状态索引 |
| `idea_post_revisions` | `post_id`、`revision_no` 唯一、标题、摘要、Markdown 正文、正文 hash、审核状态、审核通过时间、创建者；文章修改必须新建 revision |
| `idea_tags` | `name`、`slug` 唯一、状态、排序、使用次数、重定向 slug；禁止硬删除已有使用记录的标签 |
| `idea_post_tags` | `post_id`、`tag_id` 唯一；文章和标签双向索引 |
| `idea_post_likes` | `post_id`、`user_id` 唯一，创建时间 |
| `idea_post_favorites` | `post_id`、`user_id` 唯一，创建时间 |
| `idea_post_assets` | `post_id`、`revision_id`、对象 key、原始文件名、MIME、大小、hash、宽高、状态、上传者、删除时间；对象 key 唯一 |
| `idea_post_rewards` | 付款人、收款人、post/revision、asset_type、数量、幂等键唯一、状态、冻结/逆转字段、账本引用 |
| `idea_post_reports` | 举报人、文章、原因、补充说明、状态、处理人、处理结果、审计字段；同一用户对同一文章的未处理举报可去重 |
| `idea_moderation_events` | post/revision、阶段、决策、风险等级、原因、模型/url/prompt 快照、尝试次数、错误、操作者、时间 |
| `idea_post_views` | 登录用户、文章、revision、时间桶或去重 hash；按查询模式建立索引，必要时转为聚合表 |
| `idea_post_audit_events` | 状态变更、标签变更、附件操作、管理员操作的不可变审计记录 |
| `idea_post_rewards` | 付款人、收款人、post/revision、asset_type、数量、幂等键唯一、状态；余额走 `user_balance_ledger`、积分走 `points_ledger`，不再建独立作者收益子账本 |

必须明确不创建：

```text
idea_comments
idea_replies
idea_reward_messages
```

关键数据库约束：

- 金额和积分在 DB 层使用 `NUMERIC`；Go 层沿用现有 `users.balance` / 账本的 float64 口径，通过现有 `user_repo` 的余额调整方法（内部 float64→decimal 写入）复用，不另起一套 NUMERIC 转换逻辑。
- 所有唯一幂等键、点赞、收藏、标签关联均有数据库唯一约束。
- 文章状态、审核决策、赞赏状态和结算状态使用 CHECK 约束或受控枚举。
- 软删除文章不应被默认列表查询返回；删除不级联删除账本和审计数据。
- 新迁移必须使用下一个实际可用编号；实施前重新检查，不假定当前最高编号仍为 278。
- 迁移文件使用 `IF NOT EXISTS`/`IF EXISTS`，已应用迁移不得修改。

## 13. API 契约草案

### 13.1 用户 API

建议挂在现有已认证 `/api/v1` 路由组：

```text
GET    /ideas
GET    /ideas/:id
POST   /ideas
PATCH  /ideas/:id
DELETE /ideas/:id
GET    /ideas/mine
GET    /ideas/tags
GET    /ideas/tags/:slug
POST   /ideas/:id/publish
POST   /ideas/:id/assets/presign
POST   /ideas/:id/assets/:asset_id/complete
GET    /ideas/:id/assets/:asset_id/url
POST   /ideas/:id/like
DELETE /ideas/:id/like
POST   /ideas/:id/favorite
DELETE /ideas/:id/favorite
POST   /ideas/:id/rewards
POST   /ideas/:id/reports
GET    /ideas/mine/earnings
```

要求：

- 所有写接口校验登录用户、文章归属、状态、版本号和 CSRF/请求幂等语义（按项目现有机制）。
- 赞赏写接口要求 `Idempotency-Key`，服务端返回同一 key 的原始结果。
- 接口 DTO 不定义 `comment`、`reply`、`message` 字段；未知字段按项目 JSON 校验策略拒绝或明确忽略，不能写入数据库。
- 列表和详情统一返回 `can_edit`、`can_reward`、`can_view_asset` 等能力字段，前端只负责展示，后端仍是最终授权方。

### 13.2 管理 API

```text
GET    /admin/ideas
GET    /admin/ideas/:id
POST   /admin/ideas/:id/publish
POST   /admin/ideas/:id/reject
POST   /admin/ideas/:id/hide
POST   /admin/ideas/:id/restore
DELETE /admin/ideas/:id
GET    /admin/ideas/moderation
POST   /admin/ideas/moderation/:event_id/approve
POST   /admin/ideas/moderation/:event_id/reject
POST   /admin/ideas/moderation/:event_id/retry
GET    /admin/ideas/reports
POST   /admin/ideas/reports/:id/resolve
GET    /admin/ideas/tags
POST   /admin/ideas/tags
PATCH  /admin/ideas/tags/:id
POST   /admin/ideas/tags/:id/merge
GET    /admin/ideas/assets/usage
POST   /admin/ideas/assets/orphan-scan
POST   /admin/ideas/assets/:id/retry-delete
GET    /admin/ideas/rewards
POST   /admin/ideas/rewards/:id/freeze
POST   /admin/ideas/rewards/:id/reverse
```

后台 API 必须带分页、筛选、排序上限和管理员操作审计。批量审核、批量下架和批量清理不能绕过逐项权限和结果记录。

## 14. 前端文件落位

建议新增：

```text
frontend/src/views/ideas/IdeasListView.vue
frontend/src/views/ideas/IdeaDetailView.vue
frontend/src/views/ideas/IdeaEditorView.vue
frontend/src/views/ideas/MyIdeasView.vue
frontend/src/views/ideas/IdeaTagView.vue
frontend/src/views/admin/ideas/AdminIdeasView.vue
frontend/src/views/admin/ideas/IdeaModerationView.vue
frontend/src/views/admin/ideas/IdeaReportsView.vue
frontend/src/views/admin/ideas/IdeaTagsView.vue
frontend/src/views/admin/ideas/IdeaAssetsView.vue
frontend/src/views/admin/ideas/IdeaRewardsView.vue
frontend/src/components/ideas/IdeaCard.vue
frontend/src/components/ideas/IdeaMarkdownEditor.vue
frontend/src/components/ideas/IdeaAttachmentPicker.vue
frontend/src/components/ideas/IdeaRewardDialog.vue
frontend/src/components/ideas/IdeaReportDialog.vue
frontend/src/stores/ideas.ts
frontend/src/api/ideas.ts
frontend/src/types/ideas.ts
```

修改现有文件：

- `frontend/src/router/index.ts`：添加登录保护路由和管理员路由。
- `frontend/src/components/layout/AppSidebar.vue`：加入侧栏入口，使用 feature flag 控制。
- `frontend/src/components/layout/AppHeader.vue`：在现有 Flex 操作区加入可选快捷入口。
- `frontend/src/utils/featureFlags.ts`：注册 `ideas`、`ideasRewardsPoints`、`ideasRewardsBalance`、`ideasOSS` 等前端开关。

前端禁止添加评论组件、评论 API 类型和评论状态。Markdown 预览必须走统一清洗函数，避免每个页面各自实现不一致的 XSS 规则。

## 15. 后端文件落位和 Wire/Ent 影响

建议新增：

```text
backend/internal/service/ideas.go
backend/internal/service/ideas_moderation.go
backend/internal/service/ideas_rewards.go
backend/internal/service/ideas_assets.go
backend/internal/service/ideas_tags.go
backend/internal/repository/ideas_repo.go
backend/internal/repository/ideas_moderation_repo.go
backend/internal/repository/ideas_rewards_repo.go
backend/internal/repository/ideas_assets_repo.go
backend/internal/handler/ideas_handler.go
backend/internal/handler/admin/ideas_handler.go
backend/internal/handler/dto/ideas.go
backend/internal/repository/ideas_oss_store.go
backend/migrations/NNN_ideas_plaza.sql
```

需要修改：

- `backend/internal/server/routes/user.go`：注册已认证 `/ideas` 用户路由。
- `backend/internal/server/routes/admin.go`：注册 `/admin/ideas` 管理路由。
- `backend/internal/handler/handler.go`、`backend/internal/handler/wire.go`：加入 handler 聚合和 provider。
- `backend/internal/service/wire.go`、`backend/internal/repository/wire.go`：加入 service/repository/OSS 工厂。
- `backend/cmd/server/wire.go`、生成的 `wire_gen.go`：重新生成依赖注入代码和 cleanup worker 停止逻辑。
- `backend/internal/handler/dto/settings.go`、`setting_service.go`：加入开关、审核和 OSS 配置 DTO/读写/脱敏。

Ent 是否生成 schema 要遵循现有项目对复杂 SQL 表的约定：标准 CRUD 可使用 Ent，账本、幂等和聚合查询可使用事务 SQL，但不得出现两套互相不一致的模型定义。执行 `go generate ./ent`、`go generate ./cmd/server` 前必须先确认生成物没有覆盖用户已有未提交改动。

新 worker 必须纳入现有 `provideCleanup`，确保服务退出时停止审核、资产清理和统计任务；不能遗留 goroutine 或 ticker。

## 16. 系统设置和功能开关

### 16.1 后端设置

建议将设置分为三类：

1. **总开关**：`ideas_enabled`。
2. **发布与互动开关**：`ideas_publish_enabled`、`ideas_rewards_points_enabled`、`ideas_rewards_balance_enabled`、`ideas_reports_enabled`。
3. **审核与存储开关**：`ideas_moderation_enabled`、`ideas_oss_enabled` 及配额、超时、重试设置。

配置变更需要：

- 管理员权限和设置审计。
- 密钥只写入加密存储，读取时返回 configured 布尔值。
- 参数范围校验，例如大小、过期时间、额度、重试次数不能为负或无限大。
- 开关关闭时明确 API 行为（返回功能未开启），不能悄悄降级到公开访问或本地文件。

### 16.2 前端开关

在 `frontend/src/utils/featureFlags.ts` 中注册入口和页面开关，但前端开关只用于体验控制，后端必须再次鉴权和校验设置。前端隐藏入口不等于关闭接口。

## 17. 对现有功能的扩散影响评估

| 受影响模块 | 可能风险 | 设计上的隔离/缓解 | 上线前验证 |
| --- | --- | --- | --- |
| 账号广场评论 | 抽取审核客户端时改变 prompt、状态或 worker 行为 | 只抽取 HTTP 客户端和配置读取，保留评论业务层；文章使用独立表和任务名 | 原有评论审核单元、集成和人工回归全部通过 |
| 网关/请求内容审核 | 文章正文被误记为网关请求，影响风控统计或延迟 | 文章审核不进入 `content_moderation.go` 网关请求链路 | 网关请求延迟、日志量和审核指标前后对比 |
| 账号广场结算 | 误用其余额转账逻辑导致重复扣款或账本不一致 | 赞赏建立独立事务和 `idea_reward` 原因码，不改账号广场结算 SQL | 并发、重复请求、回滚和对账测试 |
| 用户余额 | 混合余额池被误认为可提现收入，或余额打赏与既有扣款并发导致资产不一致 | 余额打赏成功后正常作者收益立即进入可提现余额，但收益必须独立记录 `idea_reward` 来源，并与 `users.balance`、提现子账本和流水在同一事务中保持一致 | 充值/邀请/活动/赞赏余额隔离、并发扣款、重复请求和对账测试 |
| 积分 | 直接改 `points_balance` 造成无流水或重复扣减 | 使用专用积分原因码、业务唯一键和事务锁 | 并发打赏、超额、重复幂等测试 |
| 提现 | 将非文章收益放出，或“立即结算”被错误实现成绕过风控和审核 | 提现只允许 `idea_reward` 作者净收入来源；正常收益入账即 `available`，异常订单仍可冻结/逆转；复用现有审核入口但不改变旧来源语义 | 提现资格、即时可提、账户冻结、拒绝、取消、结算和逆转测试 |
| OSS 收款码/商城文件 | bucket、prefix 或密钥混用，误删其他业务文件 | 独立 `ideas_oss_*` 配置和 key 前缀；清理任务只扫描自己的 prefix | 连接测试、跨业务隔离、孤儿清理演练 |
| 认证和 Backend Mode | 未登录读取或绕过后端模式保护 | 路由挂在现有认证组并继续使用 `BackendModeUserGuard` | 未登录、过期 token、backend mode 和管理员边界测试 |
| 系统设置 | 新字段污染现有设置 DTO 或泄露密钥 | 分组 DTO、密钥 configured 标记、设置审计和回滚 | 保存/读取/脱敏/非法参数测试 |
| 侧栏和顶部导航 | Flex 布局错位、移动端溢出或管理员菜单串入 | 使用现有导航声明和响应式布局，不绝对定位；管理员与普通用户入口分别控制 | 桌面、平板、手机和管理员/普通用户截图回归 |
| 数据备份恢复 | DB 可恢复但 OSS 对象丢失 | 在上线门禁中明确对象备份和恢复责任，增加元数据/对象一致性检查 | 备份恢复演练和对象丢失告警 |
| 资源与数据库负载 | 热门列表、浏览事件、审核 worker、附件扫描造成峰值 | 分页上限、索引、Redis 去重、批量租约、限频和分阶段开关 | 压测、慢查询、队列积压和错误率监控 |
| 用户隐私 | 正文、附件、举报和审核原因泄露 | 登录授权、私有 OSS、最小化日志、管理员审计和脱敏返回 | 越权访问、删除后 URL、日志脱敏测试 |

## 18. 分阶段 Sprint 实施计划

每个 Sprint 结束都必须有可运行、可演示、可测试的增量；积分和余额打赏的产品目标均为开启，但在对应账务、提现和回归验收完成前不得提前打开生产开关。

### Sprint 0：架构和资金规则冻结

**目标**：只设计和确认，不写业务代码、不执行迁移。

原子任务：

1. 冻结文章状态机、版本策略和无评论边界。
2. 冻结登录访问规则、作者可见范围和管理员权限。
3. 冻结 AI 三态输出、失败闭环、prompt 版本和人工审核 SLA。
4. 冻结 OSS 私有访问、文件白名单、配额和备份责任。
5. 冻结积分打赏额度、幂等和逆转规则。
6. 冻结余额来源、冻结期、平台费、最低提现额、实名/收款资料和提现适配策略。
7. 检查实际最新迁移编号、未提交文件、Wire 生成物和设置命名冲突。

验收：产品规则和资金规则有明确书面确认；未确认项全部标记为“不开关、不实施”。

### Sprint 1：内容基础闭环

依赖：Sprint 0 完成。

实现：

1. 新增文章、revision、标签和审计迁移。
2. 新增用户路由、handler、service、repository 和 DTO。
3. 完成草稿、发布、详情、编辑、软删除和我的文章。
4. 完成 Markdown 编辑/预览和统一 XSS 清洗。
5. 完成标签选择、规范化和基础筛选。
6. 在侧栏和顶部操作区加入受开关控制的入口。
7. 添加管理员文章列表、详情、手动通过/拒绝/隐藏。

验收：登录用户可以完整创建草稿并提交待审核；未登录、其他作者和删除文章无法越权；页面不存在评论区域和评论 API。

### Sprint 2：AI 审核与后台治理

依赖：Sprint 1 的 revision 和状态机。

实现：

1. 抽取通用审核 HTTP 客户端，保持账号广场评论行为不变。
2. 新增文章审核 worker、租约、重试和事件记录。
3. 实现 `pass/review/reject` 三态和失败保持不可见。
4. 完成人工审核队列、批量逐条审计和作者拒绝原因展示。
5. 完成举报、处理、标签黑名单和标签治理。

验收：低风险自动发布、高风险进入人工队列、服务异常不公开；账号广场评论审核回归测试通过。

### Sprint 3：互动能力

依赖：Sprint 1 的文章查询和公开版本。

实现：

1. 点赞和收藏唯一约束、幂等切换和计数。
2. 登录用户浏览去重和统计聚合。
3. 最新/热门/精选排序和管理员置顶。
4. 作者数据概览（阅读、点赞、收藏、文章状态），不增加评论指标。

验收：并发点赞、重复请求和刷新浏览不会重复计数；热门列表分页和索引有效。

### Sprint 4：积分与余额打赏基础账务

依赖：Sprint 0 资金规则、Sprint 1 文章、现有积分/余额事务能力。

实现：

1. 新增 `idea_post_rewards`、作者收益子账本和积分/余额原因码。
2. 实现积分和余额两种资产的幂等、限额、自赞防护和原子账本事务。
3. 对余额打赏在同一事务内完成付款方扣款、作者 `available` 收益入账、`users.balance` 同步和双方流水；正常订单提交后立即可提现。
4. 实现作者收益统计、管理员审计、异常冻结和违规逆转。
5. 增加反作弊规则：频率、设备/IP 聚合、异常金额和多账号模式。

验收：重复请求只有一笔业务记录；任何失败都不产生半笔扣款；积分不可提现；余额打赏正常订单立即进入可提现收益；两种资产、流水和子账本均可对账。

### Sprint 5：OSS 图片与附件

依赖：Sprint 1 revision、Sprint 0 OSS 配置确认。

实现：

1. 独立 OSS 设置、加密保存、连接测试和后台页面。
2. 私有上传、魔数/MIME 校验、尺寸/数量/配额限制。
3. 资源确认、版本绑定、短时 presigned URL 和鉴权代理。
4. 孤儿扫描、删除重试、使用量统计和审计。

验收：未登录或无权用户无法取得附件 URL；跨业务 prefix 不可见；上传中断和删除失败可恢复处理。

### Sprint 6：余额提现联调与资金风控

依赖：Sprint 4、现有提现系统回归、资金规则和提现参数确认。

实现：

1. 完善作者收益子账本、来源字段、平台费和即时可提现余额。
2. 适配现有用户/管理员提现 API，增加 `idea_reward` 来源和资格校验。
3. 增加账户级风控冻结、订单级逆转、异常账户处理和管理员强制复核。
4. 增加对账报表，核对付款方扣款、作者余额、可提现收益、提现申请和逆转。

验收：充值余额、活动余额、邀请收益和文章赞赏收益按规则隔离；正常文章赞赏收益可立即申请提现；被冻结、已逆转或非 `idea_reward` 来源无法申请提现；旧提现流程回归通过。

### Sprint 7：灰度和正式发布

灰度顺序：

1. 仅管理员可见和测试文章。
2. 内部测试用户，积分打赏、余额打赏和附件开关分别打开并观察对账。
3. 小范围普通用户，观察审核积压和资源消耗。
4. 全量开放；积分和余额打赏均按产品目标开启，同时保留独立紧急关闭开关和资金日报。

监控指标：接口错误率、P95 延迟、审核队列长度、AI 失败率、人工处理时长、OSS 上传/签名/删除失败率、孤儿对象数量、赞赏重复/逆转异常、提现拒绝率、数据库锁等待和慢查询。

## 19. 测试策略

### 19.1 后端单元和集成测试

- 状态机合法/非法迁移。
- revision 并发编辑和旧版本保持公开。
- Markdown 清洗和恶意 HTML/XSS。
- AI 三态解析、超时、非 2xx、非法 JSON、重试和 fail-closed。
- 标签规范化、黑名单、合并和重定向。
- 点赞/收藏唯一约束和幂等。
- 浏览去重和聚合重建。
- 积分赞赏并发、重复 key、自赞、余额不足、事务回滚和账目对账。
- 余额收益来源、冻结、逆转、提现资格和现有提现回归。
- OSS MIME/魔数、路径穿越、配额、私有访问、过期 URL、孤儿清理和跨 prefix 隔离。
- 登录、管理员、作者、其他用户、已删除文章和 Backend Mode 的越权测试。

### 19.2 前端测试

- 受保护路由跳转和登录过期。
- 列表筛选、详情、编辑、版本状态和错误提示。
- 移动端/平板/桌面导航不溢出。
- Markdown 预览不执行脚本。
- 点赞/收藏/打赏按钮防重复提交。
- 审核、举报、附件上传和 presigned URL 失败状态。
- 确认任何页面没有评论相关文字、按钮、请求或空状态。

### 19.3 契约、压测和发布前检查

- OpenAPI/DTO 与前后端实际响应一致。
- 列表分页、热门排序、审核队列和附件签名压测。
- 数据库迁移在空库、已有数据和重复执行环境分别演练。
- 备份恢复演练必须同时验证 DB 元数据和 OSS 对象恢复责任。
- 使用现有未提交工作区做变更前快照，确保生成代码和迁移没有覆盖无关改动。

按照仓库约束，测试脚本和调试文件只放 `test/`；验证完成后删除仅为本次开发生成的临时脚本、截图和输出文件，不把临时文件带入提交。

## 20. 灰度、发布和回滚

### 20.1 发布门禁

必须全部满足后才进入下一阶段：

- 开发、迁移和灰度前所有新功能开关默认关闭且可独立关闭；通过验收后按产品目标开启积分与余额打赏。
- 数据库迁移已备份、演练并获得明确执行授权。
- 账号广场评论审核、网关、支付、余额和提现回归通过。
- OSS 私有访问和对象恢复责任已验证。
- 赞赏对账和反作弊报告可用。
- 审核失败、队列积压和对象清理失败均有告警。

### 20.2 回滚顺序

1. 先关闭 `ideas_publish_enabled`、互动、打赏和 OSS 上传开关，保留管理员只读排障。
2. 若 AI 审核异常，关闭文章自动发布，保留人工队列；禁止 fail-open。
3. 若余额或提现出现对账问题，立即关闭余额打赏和新增提现申请，保留已有申请的人工处理和审计。
4. 若附件出现越权，关闭附件 URL 签发并轮换 OSS 凭据，检查审计和短时 URL 过期。
5. 代码回滚只回滚未依赖新数据的应用版本；已应用迁移原则上不删除表，必要时通过向前兼容修复迁移和数据脚本处理。
6. 任何账务数据修复必须先导出审计、明确影响范围并取得用户授权，不能使用 `reset`、删表或覆盖历史流水。

## 21. 数据库安全门槛

本功能会新增文章、审核、互动、附件和赞赏数据，实际执行迁移前必须向你说明并获得明确同意，至少包括：

- 每个迁移文件的表、索引、约束和预计锁影响。
- 是否包含余额/积分字段或账本结构变更。
- 迁移前备份、执行窗口、回滚/前向修复方案。
- 重复执行和部分失败后的处理方式。
- 对现有账号广场、提现和账本数据的只读验证结果。

迁移必须使用下一个实际可用编号（当前仓库勘察到的最高编号为 278，但实施前要重新确认），采用幂等 SQL，不修改已经应用的历史迁移。

## 22. 已确认的产品决策（2026-08-28）

你已对上一版方案提出的三项待确认问题作出决定，后续实施必须以本节为准：

1. **打赏资产**：积分打赏和余额打赏都打开，二者都是正式产品能力。
2. **余额结算与提现**：余额打赏成功后，作者收益直接结算到可提现余额，可以直接发起提现；正常订单不设置常规等待冻结期。
3. **内容额度**：文章、图片和附件的产品限制按“不过度严格”的建议执行，允许管理员在后台调整。当前建议默认值为：正文最多 20,000 个汉字/字符、单篇最多 9 张图片、3 个普通附件、单张图片不超过 10 MB、单个普通附件不超过 50 MB。

上述“直接结算”不等于跳过资金安全控制：

- 正常订单只有在幂等记录、付款方扣款、作者收益入账、双方流水和收益子账本全部在同一事务提交后，才标记为可提现。
- 只有可追溯的 `idea_reward` 作者净收入具备提现资格，充值余额、活动赠送余额、邀请收益等其他来源不会自动获得提现资格。
- 被举报、风控命中、账户冻结或文章违规下架时，管理员仍可冻结或逆转相关收益，并保留完整审计。
- 文件类型、MIME、扩展名、魔数、图片解码和 XSS 安全校验不因“限制不严格”而放宽；放宽的是内容额度，不是安全边界。

仍需在进入具体编码前补充的只是工程参数确认，而不是重新讨论产品方向：平台费率、最低提现额、每日/月提现上限、可用收款方式、管理员人工审核角色和处理时限。若未另行指定，实施时必须把这些参数做成后台设置并在缺省/非法时明确拒绝相关操作，不能使用隐式默认值绕过资金规则。

## 23. 本轮交付边界

- 本轮只完成实施方案文档。
- 未修改业务代码、前端代码、数据库 schema、迁移文件或配置。
- 未执行数据库迁移、余额/积分变更、OSS 写入或提现操作。
- 未覆盖、回滚或删除工作区已有未提交改动。
- 你已经确认本方案的三项核心产品决策；在你明确回复“继续”或“开始实施”前，不进入代码、数据库、OSS 或提现实施阶段。

## 24. 简化决策（2026-08-28，覆盖上文子账本/风控相关设计）

在评审后，你作出以下四项简化决策，实施时以本节为准，覆盖上文第 10、12、17 节中「独立作者收益子账本」「可提现余额」「自动化风控」「NUMERIC 账本」等相关表述：

1. **余额打赏不打子账本、不加可提现余额**：余额打赏成功后，直接把打赏金额结算进被打赏人的 `users.balance`，其余不再额外增加来源标记或子账本；作者后续提现直接走现有提现逻辑。这与现有系统行为一致（现有提现本就是扣 `users.balance` 混合池、由人工审核把关），因此不引入新的资金来源隔离，也不新增「可提现余额」概念。

2. **资金口径跟随现有逻辑**：余额走 `users.balance` + `user_balance_ledger`（复用 `user_repo` 现有余额调整方法），积分走 `points_ledger`；Go 层沿用现有 float64 口径，不另起 NUMERIC 转换或新账本结构，尽量简单、不重复造轮子。

3. **打赏上限 + 人工审核替代自动化风控**：余额打赏设置上限——每个用户对单篇文章只能打赏一次、单次余额打赏上限 5 元（积分打赏同样每人每篇一次，额度走配置）。提现环节由你人工审核，不额外实现自动反作弊/风控判定。

4. **复用现有框架**：文章、审核、互动、附件、打赏全部复用现有路由/服务/仓储/设置/OSS/S3 工厂等基础能力，不额外重复实现同类组件。

由此对实施计划的影响：

- Sprint 4（打赏）从「新增作者收益子账本 + 来源标记 + 平台费 + 结算状态机」简化为「`idea_post_rewards` 业务记录 + 一笔事务内：付款人扣减、被打赏人入账、`user_balance_ledger` 双方流水、幂等键」。
- Sprint 6（提现）不再需要「适配来源/可提现额度」改造，仅确认余额打赏的钱能通过现有提现正常申请、由人工审核处理即可。
- 第 10.4 节、第 12 节 `idea_author_earnings`、第 17 节「用户余额/提现」行中关于子账本与来源隔离的描述按本节省略执行。
- 资金正确性仍需保留：打赏事务内的幂等键、付款/入账/双方流水在同一事务提交、失败整体回滚、`FOR UPDATE` 锁顺序固定，这些不变。
