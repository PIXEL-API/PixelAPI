# Sub2API 主站-子站动态账号调度架构设计方案

## 0. 当前实现状态（以代码为准）

本节记录当前仓库已经落地的能力，避免文档反向牵引实现。

已确认完成：

- 主站数据库迁移：`backend/migrations/141_subsite_control_plane.sql` 已新增 `subsites`、`account_leases`、`quota_reservations`、`subsite_heartbeats`。
- 主站内部控制面接口：当前实际路径为 `/api/internal/...`，由 `backend/internal/server/routes/subsite_internal.go` 注册。
- 主站后台管理接口：当前实际路径为 `/api/admin/subsites...`，支持子站创建、编辑、激活、暂停、恢复和租约管理。
- 子站 agent：入口为 `backend/cmd/subsite-agent/main.go`，配置来自 YAML 或 `SUBSITE_*` 环境变量。
- 子站 agent 数据面：已支持 `/v1/messages`、`/v1/chat/completions`、`/v1/responses`、OpenAI images、Gemini `/v1beta/models/*path`，并支持 Responses WebSocket。
- 子站本地 usage 队列：使用 SQLite 持久化，路径由 `SUBSITE_USAGE_QUEUE_PATH` 或配置文件控制。
- 子站部署模板：`deploy/docker-compose.subsite-agent.yml`、`deploy/.env.subsite.example`、`deploy/sub2api-subsite-agent.service`、`deploy/subsite-agent.env.example`。

当前第一阶段不作为阻塞项：

- 子站自助注册 `/register`、join token 自动接入。
- 自动账号调度、自动扩缩容、复杂地域路由。
- mTLS。
- 统一 API 域名无感分流。
- 多主站高可用。

生产测试前必须人工完成：

- 在主站后台创建子站，保存一次性显示的 `subsite_secret`。
- 在主站后台激活子站。
- 为子站手动创建至少一个账号租约。
- 在子站服务器填入 `SUBSITE_ID`、`SUBSITE_MASTER_SECRET`、`SUBSITE_MASTER_URL`、`SUBSITE_PUBLIC_URL`。
- 使用真实或受控测试账号跑通一次非流式、一次流式、一次 usage 重复上报幂等验证。

## 1. 目标说明

当前所有上游账号统一由 **主站** 管理，包括 Claude、OpenAI、Gemini 等账号或调用凭证。
希望通过 **主站 + 多个子站** 的方式实现请求分发、账号动态调度、用户日志同步和全局额度控制。

核心目标：

- 主站管理所有上游账号；
- 主站维护统一账号池；
- 主站根据子站负载、健康状态、地域、模型能力等因素动态分配账号；
- 每个子站独立部署；
- 用户登录、前端操作、额度查看、账号管理都在主站完成；
- 用户实际模型请求由子站处理；
- 主站只承载控制面流量，不承载模型流式响应和大体积响应；
- 子站使用主站分配的动态账号凭证调用上游；
- 子站将用户使用记录、请求日志、用量数据同步给主站；
- 用户额度以主站为准；
- 主站做全局限流、全局并发、账号调度和风控；
- 一个上游账号同一时间只能分配给一个子站，不能多个子站共享。

---

## 2. 总体架构

整体架构可以理解为：

```text
主站 = 控制面 / 管理面 / 权威数据中心
子站 = 数据面 / 请求执行节点 / 边缘调用节点
```

架构示意：

```text
                          ┌────────────────────────┐
                          │        主站 Master       │
                          │                        │
                          │ 用户系统                 │
                          │ 登录/前端/管理后台        │
                          │ 用户额度                 │
                          │ API Key 管理             │
                          │ 上游账号池               │
                          │ 子站管理                 │
                          │ 账号调度                 │
                          │ 全局限流/并发             │
                          │ 日志中心                 │
                          │ 计费结算                 │
                          │ 风控中心                 │
                          └───────────┬────────────┘
                                      │
                     账号租约 / 请求票据 / 日志同步 / 心跳
                                      │
        ┌─────────────────────────────┼─────────────────────────────┐
        │                             │                             │
┌───────▼────────┐           ┌────────▼───────┐           ┌────────▼───────┐
│    子站 A       │           │    子站 B       │           │    子站 C       │
│ 请求执行节点     │           │ 请求执行节点     │           │ 请求执行节点     │
│ 本地限流         │           │ 本地限流         │           │ 本地限流         │
│ 调用上游         │           │ 调用上游         │           │ 调用上游         │
│ 本地日志缓存     │           │ 本地日志缓存     │           │ 本地日志缓存     │
│ 心跳上报         │           │ 心跳上报         │           │ 心跳上报         │
└───────┬────────┘           └────────┬───────┘           └────────┬───────┘
        │                             │                             │
        └─────────────────────────────┼─────────────────────────────┘
                                      │
                              ┌───────▼────────┐
                              │   上游 AI 服务   │
                              │ Claude/OpenAI   │
                              │ Gemini/其他     │
                              └────────────────┘
```

为了解决主服务器带宽瓶颈，必须把控制面和数据面拆开：

```text
控制面：子站 ↔ 主站
用途：鉴权、额度预冻结、账号租约、心跳、日志同步、结算、风控指令

数据面：用户 ↔ 子站 ↔ 上游 AI 服务
用途：模型请求、流式响应、大体积响应、WebSocket 长连接
```

主站不应该作为常态模型请求代理。只要用户请求和流式响应仍然经过主站，主服务器带宽瓶颈就没有真正解决。

---

## 3. 核心原则

### 3.1 主站是唯一权威数据源

所有长期数据都应该以主站为准：

- 用户数据；
- 用户额度；
- 用户 API Key；
- 上游账号；
- 账号状态；
- 子站状态；
- 请求日志；
- 用量记录；
- 计费记录；
- 风控记录。

子站只作为执行节点，不应该成为长期数据源。

---

### 3.2 子站只负责请求执行

子站主要负责：

- 接收被调度过来的用户请求；
- 校验主站签发的请求票据；
- 使用主站分配的账号凭证调用上游；
- 记录本地临时日志；
- 向主站上报请求结果和用量；
- 定期向主站发送心跳；
- 接收主站账号分配、回收、禁用等指令。

子站不应该负责：

- 用户注册；
- 用户登录；
- 用户充值；
- 用户长期额度管理；
- 用户长期日志查询；
- 上游账号永久管理；
- 管理后台。

---

### 3.3 一个账号同一时间只能属于一个子站

这是整个方案中非常关键的约束。

```text
账号 A 当前分配给子站 1
则账号 A 不能同时出现在子站 2、子站 3
```

这样可以避免：

- 多个子站同时使用同一个账号；
- 上游账号并发失控；
- 用量统计错乱；
- 账号被风控；
- 无法判断哪个子站消耗了额度；
- 回收账号时状态混乱。

建议在数据库层面也强制约束：

```text
upstream_account.owner_subsite_id 同一时间只能有一个值
account_lease 同一账号只能存在一个 active 租约
```

---

### 3.4 账号分配使用租约机制

不要简单地把账号永久分给某个子站，推荐使用 **租约机制**。

主站给子站的不是永久账号所有权，而是一段时间内的使用权。

示例：

```json
{
  "lease_id": "lease_abc123",
  "account_id": "acc_001",
  "subsite_id": "site_001",
  "status": "active",
  "assigned_at": "2026-05-09T10:00:00Z",
  "expires_at": "2026-05-09T10:30:00Z",
  "max_concurrency": 5,
  "max_tokens": 500000,
  "max_requests": 1000
}
```

租约好处：

- 主站可以随时知道账号归属；
- 子站掉线后账号不会永久占用；
- 可设置过期时间；
- 可限制并发、请求数、token 数；
- 可回收、续租、释放；
- 便于做全局调度。

---

### 3.5 额度以主站为准

用户额度不能以子站为准。

推荐模式：

```text
主站预冻结额度
子站执行请求
子站上报实际用量
主站最终结算
```

这样可以避免：

- 用户余额不足但请求已经发出；
- 多个子站并发请求导致额度穿透；
- 子站同步延迟导致超用；
- 子站异常不上报导致主站额度不准。

---

### 3.6 子站不能直接写主站数据库

不推荐：

```text
子站 → 直接连接主站数据库 → INSERT/UPDATE
```

推荐：

```text
子站 → 主站 API → 主站校验 → 写入数据库
```

或者：

```text
子站 → 消息队列 → 主站消费 → 写入数据库
```

原因：

- 降低数据库暴露风险；
- 避免子站被攻破后直接操作主库；
- 方便鉴权、签名、限流；
- 方便版本兼容；
- 方便日志幂等处理；
- 方便审计。

---

### 3.7 主站默认不承载数据面流量

本方案的首要目标是解决主服务器带宽限制，因此主站不应作为常态反向代理承接模型请求和流式响应。

推荐边界：

```text
主站处理小流量控制消息：
- 用户登录；
- API Key 创建和管理；
- 子站鉴权；
- 请求授权；
- 额度预冻结；
- 账号租约下发；
- 请求日志接收；
- 用量结算；
- 后台管理操作。

子站处理大流量数据消息：
- 模型请求体；
- 模型响应体；
- SSE 流式响应；
- WebSocket 长连接；
- 文件、图片等大体积内容；
- 上游请求和响应。
```

如果第一阶段仍采用 `用户 → 主站 → 子站 → 上游`，主站仍会被流式响应和大响应占满带宽，只能作为兼容、灰度、调试或应急模式，不能作为解决带宽问题的主路径。

---

## 4. 主站职责

主站负责所有核心管理能力。

### 4.1 用户系统

包括：

- 用户注册；
- 用户登录；
- 用户信息管理；
- 用户权限；
- 用户分组；
- 用户封禁；
- 用户 API Key 管理；
- 用户套餐；
- 用户余额；
- 用户额度。

---

### 4.2 前端和管理后台

所有用户登录、前端操作、后台管理都在主站进行。

包括：

- 用户登录页面；
- 用户控制台；
- 额度查看；
- 请求记录查看；
- API Key 创建；
- 账号管理后台；
- 子站管理后台；
- 日志查询；
- 风控面板；
- 统计报表。

子站不提供用户登录和管理页面。

---

### 4.3 上游账号池管理

主站统一维护所有上游账号，包括：

- Claude 账号；
- OpenAI 账号；
- Gemini 账号；
- 其他 AI 服务账号；
- API Key；
- Cookie；
- Token；
- OAuth 凭证；
- 账号健康状态；
- 账号可用模型；
- 账号额度；
- 账号并发限制。

账号状态建议：

```text
idle        空闲，可分配
assigned    已分配给子站
draining    准备回收，不接收新请求
disabled    禁用
error       异常
cooldown    冷却中
checking    健康检查中
```

---

### 4.4 子站管理

主站维护所有子站信息：

- 子站 ID；
- 子站名称；
- 子站域名；
- 子站地域；
- 子站状态；
- 子站密钥；
- 子站证书；
- 最大 QPS；
- 最大并发；
- 当前负载；
- 当前分配账号数；
- 当前未同步日志数；
- 最近心跳时间；
- 子站版本；
- 子站健康评分。

子站状态建议：

```text
pending       已在主站创建，等待管理员激活
active        正式承接用户请求
maintenance   维护中，不接收新请求
unhealthy     心跳异常、错误率过高或待人工检查
disabled      禁用，不允许授权、不允许续租
```

当前实现中，新建子站进入 `pending`。agent 心跳只更新心跳时间、版本和健康信息，不会把 `pending` 自动提升为 `active`。必须由管理员在后台激活后，子站才会通过 authorize 校验并承接新请求。

---

### 4.5 账号调度

主站根据以下因素动态调度账号：

- 子站负载；
- 子站 QPS；
- 子站并发；
- 子站延迟；
- 子站错误率；
- 子站地域；
- 子站可用性；
- 子站健康评分；
- 账号健康评分；
- 账号支持模型；
- 账号剩余额度；
- 账号错误率；
- 账号冷却状态；
- 用户请求模型；
- 用户所在区域；
- 全局并发策略。

---

### 4.6 全局限流和全局并发

主站需要控制：

- 用户级 QPS；
- 用户级并发；
- API Key 级 QPS；
- API Key 级并发；
- 子站级 QPS；
- 子站级并发；
- 账号级并发；
- 模型级并发；
- 全局请求量；
- 全局 token 消耗速度。

示例：

```text
用户 user_001 最大并发 5
子站 site_001 最大并发 100
账号 acc_001 最大并发 3
模型 claude-sonnet 全局最大并发 500
```

---

### 4.7 日志中心和计费中心

主站最终保存所有请求日志和计费数据：

- request_id；
- user_id；
- api_key_id；
- subsite_id；
- account_id；
- lease_id；
- provider；
- model；
- prompt_tokens；
- completion_tokens；
- total_tokens；
- cost；
- status；
- error_code；
- latency_ms；
- created_at；
- completed_at。

---

## 5. 子站职责

子站是请求执行节点，职责尽量简单。推荐子站部署精简代理程序，而不是部署完整主站项目。

### 5.1 接收请求

子站接收用户直接发起的模型请求，并在执行前向主站申请授权。

请求可以来自：

1. 用户客户端携带 API Key 直接请求子站，子站再向主站 authorize；
2. 用户客户端携带主站签发的 ticket 直连子站；
3. 单域名入口转发到某个子站；
4. 管理后台调试或应急时，由主站代理到子站。

---

### 5.2 校验请求授权

子站必须校验主站返回的授权上下文或主站签发的请求票据。

校验内容包括：

- 授权上下文或 ticket 是否由主站签发；
- 是否过期；
- 是否属于当前子站；
- 是否绑定当前用户；
- 是否绑定当前 request_id；
- 是否绑定指定模型；
- 是否绑定当前账号租约；
- 是否超过授权上限；
- ticket 模式下是否已经被使用。

无合法授权的请求，子站必须拒绝。

---

### 5.3 使用主站分配的账号

子站只能使用主站分配给自己的账号。

子站不能：

- 使用未分配账号；
- 使用已过期租约账号；
- 使用其他子站账号；
- 使用已被主站回收账号；
- 使用 disabled/error/cooldown 状态账号。

---

### 5.4 本地日志缓存

子站请求执行完成后，应先在本地持久化日志，再同步主站。

本地缓存可以使用：

- SQLite；
- PostgreSQL；
- Redis Stream；
- 文件队列；
- NATS JetStream；
- Kafka；
- RabbitMQ。

小规模第一版可以使用 SQLite 或本地数据库。

---

### 5.5 日志同步

子站将请求记录、用量数据同步给主站。

必须支持：

- 批量上报；
- 失败重试；
- 幂等；
- 去重；
- 补传；
- 对账；
- 同步状态跟踪。

---

### 5.6 心跳上报

子站定期向主站上报状态。

心跳内容示例：

```json
{
  "subsite_id": "site_001",
  "version": "1.2.3",
  "status": "active",
  "active_requests": 32,
  "qps": 15.2,
  "error_rate": 0.03,
  "avg_latency_ms": 4200,
  "assigned_accounts": 12,
  "pending_sync_logs": 5,
  "cpu_usage": 0.61,
  "memory_usage": 0.72,
  "timestamp": "2026-05-09T10:00:00Z"
}
```

---

### 5.7 精简子站代理程序

子站不建议部署完整主站项目。完整项目包含面板、用户、支付、账号管理、报表、后台任务等能力，暴露在子站会扩大攻击面，也会增加维护和配置复杂度。

推荐做独立的轻量代理程序，例如：

```text
sub2api-server        主站完整服务
sub2api-subsite-agent 子站精简代理
```

子站代理程序只包含：

```text
HTTP/SSE/WebSocket 代理入口
主站 authorize 客户端
主站 usage batch 客户端
主站 heartbeat 客户端
账号租约本地缓存
上游凭证内存缓存
本地日志队列
本地限流和并发槽
draining 排空逻辑
健康检查接口
```

子站代理程序不包含：

```text
用户注册/登录
用户控制台
管理后台
支付系统
套餐系统
API Key 创建
上游账号永久管理
长期日志查询
主数据库直连能力
全局调度决策
```

第一版可采用单二进制部署：

```text
sub2api-subsite-agent
  --config subsite.yaml
```

配置示例：

```yaml
subsite:
  id: site_us_01
  public_url: https://us-01-api.example.com
  region: us
  capabilities:
    - anthropic
    - openai
  max_qps: 100
  max_concurrency: 200

master:
  base_url: https://master.example.com
  subsite_secret: ${SUBSITE_SECRET}

local:
  listen_addr: 0.0.0.0:8080
  queue_path: ./data/usage_queue.db
```

主站面板只管理子站元数据、账号分配、租约、状态和风控策略。子站代理程序通过心跳拉取或接收这些控制信息。

---

### 5.8 子站 URL 暴露方案

用户如何访问子站有三种可选方案。

#### 方案 A：主站展示可用子站 URL

```text
用户登录主站
主站展示当前推荐入口：
https://us-01-api.example.com
https://hk-01-api.example.com
https://sg-01-api.example.com
用户复制其中一个作为 API Base URL
```

优点：

- 实现最简单；
- 主站不承载数据面流量；
- 子站问题容易定位；
- 每个子站可以独立限流、维护、下线。

缺点：

- 用户需要选择或切换 URL；
- 子站 URL 会暴露；
- 子站故障时用户需要换入口。

适合作为第一阶段 MVP。

#### 方案 B：一个统一 API 域名，DNS 或边缘网关分流

```text
用户只使用：
https://api.example.com

DNS / CDN / 边缘网关 根据地区、健康状态、权重把流量转到：
https://us-01-api.example.com
https://hk-01-api.example.com
https://sg-01-api.example.com
```

优点：

- 用户体验最好；
- 用户不用理解子站；
- 可以按地域和健康状态自动切换。

缺点：

- 如果统一入口本身回源代理所有数据流，可能再次形成带宽瓶颈；
- DNS 切换有缓存延迟；
- CDN/边缘网关成本和配置复杂度更高；
- SSE/WebSocket 需要确认网关完整支持长连接。

注意：统一域名可以用，但统一入口不能部署在主站服务器上做全量反代。否则主站带宽瓶颈会回来。统一入口应放在 DNS、CDN、Anycast、云负载均衡或独立边缘网关层。

#### 方案 C：主站返回推荐子站，客户端自动切换

```text
用户请求主站轻量接口：
GET /api/client/subsite/recommend

主站返回：
{
  "base_url": "https://hk-01-api.example.com",
  "expires_at": "2026-05-09T10:10:00Z"
}

客户端后续模型请求直接打到该 base_url
```

优点：

- 主站只返回很小的调度结果；
- 不承载模型数据面；
- 可以动态按用户地区、子站健康、账号租约推荐入口。

缺点：

- 需要客户端支持自动发现；
- 对只支持固定 Base URL 的第三方客户端不友好。

推荐落地顺序：

```text
第一阶段：方案 A，主站面板展示多个可用子站 URL
第二阶段：方案 C，为自有客户端提供自动推荐入口
第三阶段：方案 B，用统一域名 + 边缘层实现无感分流
```

---

## 6. 推荐请求流程

### 6.1 用户登录和前端操作

```text
用户 → 主站前端
主站完成登录、额度展示、API Key 管理、请求记录查询
```

所有用户前端操作都在主站进行。

---

### 6.2 用户发起模型请求

推荐第一阶段直接采用 **子站数据面直连模式**。用户的模型请求不再进入主站，而是直接请求子站；子站在执行前向主站请求授权和额度预冻结。

```text
1. 用户在主站创建 API Key
2. 用户选择或获得一个可用子站入口
3. 用户携带 API Key 直接请求子站
4. 子站只读取必要元数据，不信任用户身份和额度
5. 子站向主站发起内部 authorize 请求
6. 主站校验 API Key、用户状态、额度、限流、风控
7. 主站为本次请求预冻结额度
8. 主站确认子站是否有可用账号租约
9. 主站返回 request_id、reservation_id、lease_id、account_id、授权上限
10. 子站校验授权结果与自身租约一致
11. 子站使用租约内账号调用上游
12. 子站把模型响应直接返回给用户
13. 子站先在本地持久化请求日志和用量
14. 子站批量或实时向主站同步日志和用量
15. 主站按 request_id 幂等入库
16. 主站根据实际用量结算额度，多退少补
```

---

### 6.3 请求模式选择

#### 模式一：子站数据面直连，主站控制面授权

```text
用户 → 子站 → 上游
子站 → 主站：授权、预冻结、日志同步、心跳
```

优点：

- 主站不承载模型请求和流式响应；
- 能直接降低主服务器带宽压力；
- 子站承担实际请求流量；
- 用户 API 兼容性较好，不强制用户先调用 ticket 接口；
- 子站可以按地域、带宽、上游连通性横向扩容。

缺点：

- 子站地址会暴露；
- 子站必须严格校验主站授权结果；
- 子站必须本地持久化未同步日志；
- 子站与主站之间要处理授权延迟和主站不可用问题。

适合：

- 当前主站带宽受限场景；
- 需要真实分摊流量的多实例部署；
- 大量 SSE、WebSocket、长响应请求；
- 多地域边缘节点。

---

#### 模式二：主站签发 ticket，用户直连子站

```text
用户 → 主站获取 ticket
用户 → 子站携带 ticket 请求
子站 → 上游
子站 → 主站同步日志
```

优点：

- 主站压力较小；
- 子站承担实际请求流量；
- 扩展性更好；
- 适合多地域节点。

缺点：

- 子站地址会暴露；
- 必须做好 ticket 校验；
- 必须防止用户绕过主站；
- 跨域和鉴权更复杂。

适合：

- 请求量较大；
- 子站数量较多；
- 需要边缘节点分流。

---

#### 模式三：所有请求先到主站，由主站代理到子站

```text
用户 → 主站 → 子站 → 上游
```

优点：

- 用户只接触主站；
- 子站不暴露；
- 鉴权集中；
- 兼容旧客户端；
- 适合调试和灰度验证。

缺点：

- 主站仍然承载全部请求和响应流量；
- 流式响应会长期占用主站连接；
- 主站带宽瓶颈不会被解决；
- 多一个转发链路，延迟更高。

适合：

- 管理后台调试；
- 小流量灰度；
- 子站故障时的应急回退；
- 对子站直连暂时不可用的兼容场景。

不建议作为第一阶段主路径。

---

#### 推荐方式

第一阶段主路径应该使用：

```text
用户 → 子站 → 上游
子站 → 主站：authorize / usage / heartbeat
```

稳定后再增强为：

```text
主站签发一次性 ticket 或短期授权令牌，子站本地校验后执行请求
```

---

## 7. 请求授权设计

请求授权用于证明该请求已经经过主站鉴权、额度检查、限流检查和账号租约检查。

第一阶段可以不强制用户先向主站获取 ticket。更实用的方式是：用户直接请求子站，子站在请求开始前向主站申请一次内部授权，主站返回本次请求的授权上下文。

```http
POST /api/internal/requests/authorize
```

请求示例：

```json
{
  "subsite_id": "site_001",
  "api_key": "sk-xxx",
  "model": "claude-sonnet",
  "request_type": "stream",
  "estimated_tokens": 200000,
  "client_request_id": "client_req_optional",
  "timestamp": "2026-05-09T10:00:00Z",
  "nonce": "random_string"
}
```

响应示例：

```json
{
  "request_id": "req_123",
  "reservation_id": "rsv_123",
  "subsite_id": "site_001",
  "user_id": "user_001",
  "api_key_id": "key_001",
  "account_id": "acc_001",
  "lease_id": "lease_001",
  "provider": "claude",
  "model": "claude-sonnet",
  "max_tokens": 200000,
  "expires_at": "2026-05-09T10:05:00Z"
}
```

子站必须校验：

```text
响应签名是否来自主站
响应中的 subsite_id 是否等于当前子站
lease_id 是否仍在本地有效租约内
account_id 是否属于当前子站
model 是否匹配用户请求
expires_at 是否未过期
max_tokens 是否未被本地请求超出
```

该模式的优势是用户侧协议变化小，缺点是每次请求开始前子站必须访问主站。为了避免主站故障时产生免费调用，主站不可用时默认拒绝新请求，只允许明确配置的短时降级策略。

---

### 7.1 一次性 request_ticket 模式

示例：

```json
{
  "ticket_id": "tkt_123",
  "request_id": "req_123",
  "user_id": "user_001",
  "api_key_id": "key_001",
  "subsite_id": "site_001",
  "account_id": "acc_001",
  "lease_id": "lease_001",
  "model": "claude-sonnet",
  "max_tokens": 200000,
  "expires_at": "2026-05-09T10:10:00Z",
  "nonce": "random_string"
}
```

ticket 需要签名，例如：

```text
signature = HMAC_SHA256(master_secret, ticket_payload)
```

或者使用 JWT/JWS。

子站校验：

```text
ticket 是否有效
ticket 是否过期
ticket 是否属于当前子站
ticket 是否绑定当前模型
ticket 是否绑定当前 request_id
ticket 是否已经使用过
```

建议 ticket：

- 有效期短，例如 1-5 分钟；
- 一次性使用；
- 绑定子站；
- 绑定用户；
- 绑定模型；
- 绑定 request_id；
- 绑定账号租约；
- 防重放。

---

## 8. 账号租约机制

### 8.1 租约目的

账号租约用于确保：

```text
同一个账号同一时间只能被一个子站使用
```

同时便于主站回收和调度账号。

---

### 8.2 租约字段

```text
account_leases
- id
- lease_id
- account_id
- subsite_id
- status
- max_concurrency
- current_concurrency
- max_tokens
- used_tokens
- max_requests
- used_requests
- assigned_at
- expires_at
- renewed_at
- released_at
```

---

### 8.3 租约状态

```text
active      使用中
renewing    续租中
draining    排空中
released    已释放
expired     已过期
revoked     被主站强制撤销
```

---

### 8.4 分配流程

```text
1. 主站检查账号是否 idle
2. 主站检查账号健康状态
3. 主站检查账号是否支持目标模型
4. 主站检查子站是否可用
5. 主站创建租约
6. 主站将账号 owner_subsite_id 设置为目标子站
7. 主站将账号状态设置为 assigned
8. 子站收到账号凭证或调用授权
9. 子站开始使用账号
```

---

### 8.5 回收流程

不能直接强制回收账号，推荐使用 draining 排空机制。

```text
1. 主站将账号状态设置为 draining
2. 主站通知子站停止给该账号分配新请求
3. 子站等待已有请求完成
4. 子站上报账号已释放
5. 主站将租约状态设置为 released
6. 主站将账号状态设置为 idle
7. 主站可重新分配给其他子站
```

如果子站长时间不响应：

```text
1. 主站标记子站异常
2. 账号进入 checking 或 cooldown
3. 暂时不要立即分配给其他子站
4. 等待确认账号状态后再重新调度
```

---

### 8.6 第一阶段最小租约策略

第一阶段不要一开始就实现复杂自动调度，可以先采用“手动分配 + 短租约 + 子站续租”的方式。

推荐规则：

```text
租约有效期：10-30 分钟
续租窗口：过期前 1-5 分钟
子站心跳间隔：10-30 秒
心跳超时：连续 3-5 次失败后标记子站异常
账号回收：先 draining，再 released
异常子站账号：先 checking/cooldown，不立即分给其他子站
```

数据库层必须保证：

```text
同一个 account_id 同一时间只能有一个 active/renewing/draining 租约
upstream_accounts.owner_subsite_id 必须与 active lease 的 subsite_id 一致
租约状态变更必须在事务内完成
租约续期必须校验当前租约仍归属当前子站
```

子站本地也要维护租约缓存，但只能作为执行缓存，不能成为权威来源。主站一旦撤销租约，子站必须停止给该账号分配新请求。

---

## 9. 用户额度管理

### 9.1 额度以主站为准

子站只能上报实际使用量，不能直接修改用户最终额度。

---

### 9.2 预冻结 + 结算

推荐额度流程：

```text
请求开始前：主站预冻结额度
请求完成后：主站按实际用量结算
请求失败：释放冻结额度或按规则扣减
请求超时：定时任务处理冻结记录
```

在子站数据面直连模式下，预冻结必须发生在子站调用上游之前：

```text
1. 子站收到用户请求
2. 子站向主站申请 authorize
3. 主站按模型、max_tokens、用户套餐估算冻结额度
4. 冻结成功后主站返回 reservation_id
5. 子站才能调用上游
6. 子站上报实际 token 和状态
7. 主站按实际用量结算 reservation
```

主站不可用时，默认不允许新请求继续调用上游。否则子站可能产生无法结算的真实上游成本。

---

### 9.3 额度字段

```text
user_quotas
- user_id
- total_quota
- used_quota
- frozen_quota
- available_quota
- updated_at
```

其中：

```text
available_quota = total_quota - used_quota - frozen_quota
```

---

### 9.4 冻结记录

```text
quota_reservations
- id
- reservation_id
- request_id
- user_id
- estimated_tokens
- actual_tokens
- status
- expires_at
- created_at
- settled_at
```

状态：

```text
frozen      已冻结
settled     已结算
released    已释放
expired     已过期
failed      请求失败
```

---

## 10. 日志同步设计

### 10.1 日志上报内容

子站向主站上报：

```json
{
  "request_id": "req_123",
  "subsite_id": "site_001",
  "account_id": "acc_001",
  "lease_id": "lease_001",
  "user_id": "user_001",
  "api_key_id": "key_001",
  "provider": "claude",
  "model": "claude-sonnet",
  "status": "success",
  "prompt_tokens": 1000,
  "completion_tokens": 2000,
  "total_tokens": 3000,
  "latency_ms": 5200,
  "error_code": null,
  "created_at": "2026-05-09T10:00:00Z",
  "completed_at": "2026-05-09T10:00:05Z"
}
```

---

### 10.2 幂等要求

每个请求必须有全局唯一 `request_id`。

主站数据库需要设置唯一约束：

```text
UNIQUE(request_id)
```

重复上报时：

- 不重复插入日志；
- 不重复扣费；
- 不重复结算额度。

---

### 10.3 批量上报

推荐子站批量同步：

```http
POST /api/internal/usage/batch
```

请求示例：

```json
{
  "subsite_id": "site_001",
  "batch_id": "batch_20260509_001",
  "records": [
    {
      "request_id": "req_001",
      "user_id": "user_123",
      "api_key_id": "key_456",
      "model": "claude-sonnet",
      "prompt_tokens": 1000,
      "completion_tokens": 2000,
      "total_tokens": 3000,
      "status": "success",
      "latency_ms": 4200,
      "created_at": "2026-05-09T10:00:00Z"
    }
  ]
}
```

响应示例：

```json
{
  "accepted": 100,
  "duplicated": 3,
  "failed": 0
}
```

---

### 10.4 本地缓冲和补偿

子站必须本地保存未同步日志。

本地表：

```text
local_request_logs
- request_id
- payload
- sync_status
- retry_count
- last_retry_at
- created_at
```

同步状态：

```text
pending     待同步
syncing     同步中
synced      已同步
failed      同步失败
```

如果主站不可用：

```text
1. 子站继续本地保存日志
2. 定时重试同步
3. 主站恢复后补传
4. 主站按 request_id 幂等处理
```

---

## 11. 主站与子站认证

### 11.1 基础认证

每个子站拥有：

```text
subsite_id
subsite_secret
```

请求主站时需要签名。

签名内容：

```text
timestamp
nonce
body_hash
```

签名算法：

```text
signature = HMAC_SHA256(subsite_secret, timestamp + nonce + body_hash)
```

主站校验：

- subsite_id 是否存在；
- signature 是否正确；
- timestamp 是否过期；
- nonce 是否重复；
- 子站状态是否正常；
- IP 是否在白名单内。

---

### 11.2 推荐增强安全

更安全的方式：

```text
mTLS 双向 TLS
```

每个子站拥有自己的客户端证书。

优点：

- 防止伪造子站；
- 防止中间人攻击；
- 便于吊销子站；
- 适合主站-子站强信任通信。

---

## 12. 凭证下发风险与建议

### 12.1 模式 A：真实凭证下发到子站

```text
主站把真实上游账号凭证下发给子站
子站直接调用上游
```

优点：

- 性能好；
- 延迟低；
- 主站压力小；
- 子站可独立执行请求。

缺点：

- 子站被攻破后，上游账号可能泄露；
- 子站理论上可以绕过主站调用；
- 主站只能依赖子站上报统计；
- 凭证安全要求高。

如果使用此模式，必须做到：

- 凭证加密传输；
- 子站尽量不落盘；
- 如需落盘必须加密；
- 租约短期有效；
- 主站可随时撤销；
- 子站隔离部署；
- 定期轮换凭证；
- 异常时立即回收账号；
- 记录所有凭证下发行为。

---

### 12.2 模式 B：短期临时凭证

```text
主站不长期下发真实凭证
只下发短期临时调用授权
```

临时凭证可限制：

- 只能由指定子站使用；
- 只能调用指定模型；
- 只能使用指定账号；
- 有效期短；
- 最大并发；
- 最大 token；
- 最大请求数。

这是比较推荐的中长期方案。

---

### 12.3 模式 C：子站不接触真实凭证

```text
子站接收请求
主站持有真实凭证
主站调用上游
```

优点：

- 最安全；
- 子站被攻破损失小；
- 主站完全掌握调用；
- 计费更准确。

缺点：

- 主站压力最大；
- 延迟更高；
- 子站分流意义降低。

---

### 12.4 推荐选择

如果目标是让子站真正分担请求压力：

```text
第一阶段：可以使用模式 A，但必须配合租约、加密、审计和隔离
第二阶段：升级到模式 B，使用短期临时凭证
高安全场景：使用模式 C
```

---

## 13. 数据库设计建议

### 13.1 子站表

```text
subsites
- id
- name
- domain
- region
- status
- secret_hash
- public_key
- max_qps
- max_concurrency
- current_qps
- current_concurrency
- health_score
- version
- last_heartbeat_at
- created_at
- updated_at
```

---

### 13.2 上游账号表

```text
upstream_accounts
- id
- provider
- account_name
- encrypted_credentials
- status
- supported_models
- max_concurrency
- current_concurrency
- owner_subsite_id
- lease_id
- lease_expires_at
- health_score
- remaining_quota
- last_error_code
- last_used_at
- created_at
- updated_at
```

---

### 13.3 账号租约表

```text
account_leases
- id
- lease_id
- account_id
- subsite_id
- status
- max_concurrency
- current_concurrency
- max_tokens
- used_tokens
- max_requests
- used_requests
- assigned_at
- expires_at
- renewed_at
- released_at
- created_at
- updated_at
```

---

### 13.4 请求日志表

```text
requests
- id
- request_id
- user_id
- api_key_id
- subsite_id
- account_id
- lease_id
- provider
- model
- status
- prompt_tokens
- completion_tokens
- total_tokens
- cost
- latency_ms
- error_code
- error_message
- created_at
- completed_at
```

---

### 13.5 用户额度表

```text
user_quotas
- user_id
- total_quota
- used_quota
- frozen_quota
- available_quota
- updated_at
```

---

### 13.6 额度冻结表

```text
quota_reservations
- id
- reservation_id
- request_id
- user_id
- estimated_tokens
- actual_tokens
- status
- expires_at
- created_at
- settled_at
```

---

### 13.7 子站心跳表

```text
subsite_heartbeats
- id
- subsite_id
- active_requests
- qps
- error_rate
- avg_latency_ms
- assigned_accounts
- pending_sync_logs
- cpu_usage
- memory_usage
- disk_usage
- version
- created_at
```

---

## 14. 调度策略建议

### 14.1 按子站负载调度

考虑指标：

- 当前并发；
- QPS；
- CPU；
- 内存；
- 平均延迟；
- 错误率；
- 请求队列长度。

负载较低的子站优先分配更多账号。

---

### 14.2 按地域调度

根据用户 IP 或区域选择距离更近的子站。

例如：

```text
亚洲用户 → 亚洲节点
欧洲用户 → 欧洲节点
美国用户 → 美国节点
```

---

### 14.3 按账号健康度调度

账号健康评分高的优先使用。

健康评分受以下因素影响：

- 429 频率；
- 403 频率；
- 超时率；
- 成功率；
- 平均延迟；
- 剩余额度；
- 最近错误。

异常账号自动进入：

```text
cooldown
checking
disabled
```

---

### 14.4 按模型能力调度

账号不一定支持所有模型。

账号表需要记录：

```text
supported_models
```

调度时必须匹配用户请求模型。

---

### 14.5 按子站能力调度

子站也可以设置能力标签：

```text
site_001: claude, openai
site_002: gemini
site_003: claude
```

调度时只选择支持目标 provider/model 的子站。

---

## 15. 风险与应对

### 15.1 上游服务条款风险

如果将订阅账号通过平台分发给多个用户或子站，可能违反上游平台服务条款。

风险：

- 账号被封；
- 账号限流；
- 订阅取消；
- 支付风控；
- IP 风控；
- 组织封禁。

建议：

- 确认上游服务条款；
- 避免异常高并发；
- 控制账号调用行为；
- 做账号冷却；
- 保留合规审计；
- 不要过度共享个人订阅账号。

---

### 15.2 子站凭证泄露风险

风险：

- 子站被攻破；
- 上游账号 Token/Cookie/API Key 泄露；
- 攻击者绕过主站调用上游。

建议：

- 凭证短期化；
- 凭证加密；
- 尽量不落盘；
- 子站隔离部署；
- 定期轮换；
- 异常自动撤销；
- 主站保留凭证分发审计。

---

### 15.3 子站日志不同步

风险：

- 主站额度不准；
- 用户用量缺失；
- 请求记录缺失；
- 计费异常。

建议：

- 子站本地持久化日志；
- 批量同步；
- 失败重试；
- 幂等处理；
- 定期对账；
- 长时间不上报则暂停子站；
- 使用预冻结额度减少超用风险。

---

### 15.4 账号回收冲突

风险：

- 主站回收账号时，子站仍有请求在跑；
- 同一个账号短时间出现在两个子站；
- 上游账号并发异常。

建议：

- 使用 draining 状态；
- 子站停止新请求；
- 等待已有请求完成；
- 子站确认释放；
- 超时后进入 checking/cooldown；
- 不要强制立即分配给其他子站。

---

### 15.5 主站单点故障

风险：

- 用户无法登录；
- 请求无法调度；
- 子站无法获取新租约；
- 日志无法同步；
- 额度无法结算。

建议：

- 主站无状态化部署；
- 数据库高可用；
- Redis 高可用；
- 子站保留短期租约；
- 主站不可用时子站进入保护模式；
- 默认拒绝新请求，避免产生无法结算的上游成本；
- 如明确启用降级，只允许已有租约和已授权请求继续执行；
- 禁止新租约、新用户、新大额请求；
- 主站恢复后必须先补传日志并对账，再恢复正常调度。

---

### 15.6 用户绕过主站直连子站

风险：

- 用户绕过额度检查；
- 用户绕过限流；
- 用户直接刷子站。

建议：

- 子站所有请求必须经过主站 authorize 或携带主站签发 ticket；
- authorize 响应和 ticket 都必须短期有效；
- ticket 一次性使用；
- 授权上下文必须绑定用户、模型、子站、request_id、lease_id 和 account_id；
- 子站拒绝无授权请求；
- 子站限制来源；
- 子站接口签名校验。

---

### 15.7 用户隐私风险

请求日志可能包含敏感信息：

- prompt；
- response；
- 代码；
- 密钥；
- 文件内容；
- 商业数据；
- 个人隐私；
- 医疗、金融、法律信息。

建议：

- 默认只同步元数据；
- 不记录完整 prompt/response；
- 如需记录，必须加密；
- 设置日志保留周期；
- 支持用户删除；
- 对敏感字段脱敏；
- 不记录 Authorization、Cookie、上游 Token。

推荐同步：

```json
{
  "request_id": "req_xxx",
  "user_id": "user_xxx",
  "model": "claude-sonnet",
  "prompt_tokens": 1000,
  "completion_tokens": 2000,
  "total_tokens": 3000,
  "status": "success",
  "latency_ms": 8520
}
```

不推荐同步：

```json
{
  "authorization": "Bearer xxx",
  "cookie": "xxx",
  "full_prompt": "...",
  "full_response": "..."
}
```

---

## 16. 后台控制开关

主站后台建议提供以下控制能力：

```text
新增子站
编辑子站 public_url / region / capabilities
查看子站推荐入口 URL
生成子站 subsite_secret
生成一次性 join_token
确认子站接入
激活子站
暂停某个子站
恢复某个子站
强制回收某个账号
暂停某个账号
禁用某个账号
账号进入冷却
设置子站最大 QPS
设置子站最大并发
设置子站最大账号数
设置账号最大并发
设置模型级限流
设置用户级限流
设置用户全局封禁
设置子站维护模式
清空子站租约
查看子站未同步日志
触发子站补传日志
触发账号健康检查
查看子站 agent 版本
触发子站 agent 配置刷新
```

---

## 17. 分阶段落地建议

### 17.1 第一阶段：控制面和数据面拆分

目标：

```text
主站统一管理账号
主站管理用户和额度
主站管理子站
主站面板展示可用子站 URL
主站手动或半自动分配账号给子站
一个账号同一时间只属于一个子站
子站只部署精简 subsite-agent
用户模型请求直连子站
子站承担模型请求和流式响应带宽
主站只处理 authorize / usage / heartbeat 等控制面请求
子站同步日志给主站
```

第一阶段可以先不做复杂自动调度。

重点完成：

- 子站注册；
- 子站心跳；
- 精简 subsite-agent 二进制；
- 主站后台子站管理页面；
- 主站展示可用子站 URL；
- 手动或半自动账号分配；
- 子站请求入口；
- 子站向主站请求 authorize；
- 主站校验 API Key、用户状态、额度和限流；
- 主站预冻结额度；
- 子站本地执行上游调用；
- 子站直接向用户返回响应；
- 子站本地持久化请求日志；
- 子站批量请求日志上报；
- 主站按 request_id 幂等入库和结算；
- 账号不能重复分配；
- 子站本地日志缓存。

第一阶段暂不建议做：

- 全自动账号调度；
- 复杂地域路由；
- mTLS；
- 消息队列；
- 多主站高可用；
- 统一域名无感分流；
- 用户先取 ticket 再请求子站的双跳协议。

这些能力可以后续补，但第一阶段必须先把主站带宽从模型数据面中移除。

---

### 17.2 第二阶段：加入完整租约和自动调度

加入：

- 账号租约；
- 租约过期；
- 租约续期；
- 租约释放；
- draining 排空；
- 自动账号调度；
- 子站健康评分；
- 账号健康评分；
- 用户额度预冻结；
- request_ticket；
- 日志幂等上报。

第二阶段的目标是把第一阶段的手动/半自动分配升级为自动调度，同时保持一个账号同一时间只属于一个子站。

---

### 17.3 第三阶段：增强高可用和风控

加入：

- 全局限流；
- 全局并发；
- mTLS；
- 子站异常检测；
- 账号异常检测；
- 自动冷却；
- 自动扩缩容；
- 对账系统；
- 主站高可用；
- Redis/数据库高可用；
- 消息队列；
- 灰度发布；
- 多地域调度。

---

## 18. 最推荐的最终流程

最终推荐流程：

```text
1. 用户登录主站
2. 用户在主站创建 API Key
3. 用户选择或获取可用子站入口
4. 用户携带 API Key 直接请求子站
5. 子站向主站发起 authorize 控制面请求
6. 主站校验 API Key、用户状态、额度、并发和风控
7. 主站确认子站状态和账号租约
8. 主站预冻结额度
9. 主站返回 request_id、reservation_id、lease_id、account_id 和授权上限
10. 子站校验授权结果属于当前子站和当前租约
11. 子站使用租约内账号调用上游
12. 子站将上游响应直接返回给用户
13. 子站本地保存请求日志和用量
14. 子站批量或实时上报日志给主站
15. 主站按 request_id 幂等入库
16. 主站按实际 token 结算额度
17. 主站更新账号、子站、用户统计
18. 主站按心跳、错误率和用量决定续租、回收或冷却
```

---

## 19. 开发实施总纲

正式开发时建议按“主站能力、子站 agent、通信协议、数据存储、运维控制”五条线推进。

### 19.1 最终产品形态

```text
主站 sub2api-server
- 部署完整项目；
- 提供用户前台、管理后台、支付、账号管理、额度、日志、统计；
- 管理所有子站、账号租约、请求授权、用量结算；
- 不承载常态模型请求和流式响应。

子站 sub2api-subsite-agent
- 部署轻量代理程序；
- 不提供用户系统和后台面板；
- 负责接收用户模型请求、向主站 authorize、调用上游、返回响应；
- 本地缓存租约和日志；
- 向主站上报 usage、heartbeat、release、draining 状态。

用户客户端
- 可以是第三方 API 客户端，也可以是未来自有客户端；
- 只保存用户自己的 API Key 和子站 URL；
- 不持有上游账号凭证、主站密钥、子站密钥。
```

### 19.2 第一阶段开发范围

第一阶段目标是先把主站带宽从模型数据面中移除，不追求自动化调度完整度。

必须完成：

```text
1. 主站新增子站管理模型
2. 主站后台可创建、编辑、暂停、恢复子站
3. 主站后台展示可用子站 URL
4. 主站支持手动/半自动把账号分配给子站
5. 主站提供 authorize API
6. 主站提供 usage batch API
7. 主站提供 heartbeat API
8. 主站支持额度预冻结和最终结算
9. 主站按 request_id + api_key_id 幂等处理日志和计费
10. 子站 agent 提供模型请求入口
11. 子站 agent 请求前调用主站 authorize
12. 子站 agent 校验授权上下文
13. 子站 agent 使用当前租约账号调用上游
14. 子站 agent 直接把响应返回给用户
15. 子站 agent 本地持久化未同步日志
16. 子站 agent 批量上报 usage
17. 子站 agent 定时 heartbeat
18. 子站异常时主站可暂停该子站
```

第一阶段暂不做：

```text
统一 API 域名无感分流
mTLS
消息队列
自动扩缩容
复杂地域路由
全自动账号调度
多主站高可用
用户本地代理执行节点
```

### 19.3 新子站接入流程

后期随时增加服务器时，推荐把子站接入设计成“主站预创建 + 子站 agent 带密钥接入 + 管理员激活”的受控流程。第一版不建议开放完全自助注册。

标准接入流程：

```text
1. 管理员在主站后台新增子站
2. 主站生成 subsite_id 和 subsite_secret
3. 主站记录 public_url、region、capabilities、max_qps、max_concurrency
4. 子站状态为 pending
5. 管理员在新服务器部署 sub2api-subsite-agent
6. agent 配置 master_url、subsite_id、subsite_secret、public_url
7. agent 启动后向主站发送 heartbeat
8. 主站校验签名并记录版本、public_url、心跳时间和本地队列深度
9. 管理员在主站后台确认并激活子站
10. 主站将子站状态更新为 active
11. 管理员为该子站创建账号租约
12. 主站面板展示该子站 URL
13. 子站开始承接用户模型请求
```

子站 agent 配置示例：

```yaml
subsite:
  id: site_sg_02
  public_url: https://sg-02-api.example.com
  region: sg
  capabilities:
    - anthropic
    - openai

master:
  base_url: https://master.example.com
  subsite_secret: ${SUBSITE_SECRET}

local:
  listen_addr: 0.0.0.0:8080
  queue_path: ./data/usage_queue.db
```

安全要求：

```text
subsite_secret 只显示一次或只允许重置
子站首次 heartbeat 必须签名
pending 子站不能拿账号租约
只有 active 子站才能 authorize 成功
disabled / unhealthy / maintenance 子站不能获取新授权
```

新增子站时不要立即从旧子站强行抢账号。账号迁移必须遵循：

```text
1. 新子站 active
2. 主站优先把 idle 账号分配给新子站
3. 如需迁移旧账号，旧子站账号先进入 draining
4. 旧子站停止为该账号分配新请求
5. 已有请求完成
6. 旧子站 release 租约
7. 主站再把账号分配给新子站
```

后续可以升级为一次性接入令牌：

```text
1. 主站生成 join_token
2. join_token 有效期 10 分钟，只显示一次
3. 新 agent 启动时携带 join_token 调用 register
4. 主站验证 join_token 后绑定 subsite_id
5. agent 写入本地配置或接收子站密钥
6. 管理员确认激活
```

这种模式适合频繁扩容，但第一版建议先使用手动预创建，降低接入风险。

### 19.4 推荐模块拆分

主站新增模块：

```text
SubsiteService
- 子站注册、编辑、暂停、恢复；
- 子站能力、区域、public_url、版本管理；
- 子站状态和健康评分维护。

SubsiteAuthService
- 子站 HMAC 签名校验；
- timestamp / nonce / body_hash 校验；
- 防重放。

RequestAuthorizeService
- 校验用户 API Key；
- 校验用户状态、额度、限流、风控；
- 选择当前子站可用租约；
- 创建 request_id；
- 创建 quota reservation；
- 返回授权上下文。

AccountLeaseService
- 创建租约；
- 续租；
- draining；
- released / expired / revoked 状态变更；
- 保证一个账号同一时间只属于一个子站。

UsageIngestService
- 接收子站 usage batch；
- 按 request_id 幂等入库；
- 结算额度；
- 更新账号、用户、子站统计。
```

子站 agent 模块：

```text
ProxyHandler
- 提供 OpenAI / Claude / Gemini 兼容入口；
- 支持普通 HTTP、SSE、WebSocket；
- 只做必要请求解析，不做最终身份信任。

MasterClient
- 调用 authorize；
- 调用 usage batch；
- 调用 heartbeat；
- 拉取租约/配置；
- 使用 HMAC 签名。

LeaseStore
- 本地缓存当前子站租约；
- 处理租约过期、撤销、draining；
- 禁止使用不属于当前子站的账号。

UpstreamExecutor
- 使用租约账号调用上游；
- 复用现有项目中的上游请求构造、SSE 处理、错误映射能力；
- 不包含管理后台和主站业务。

UsageQueue
- 请求完成后先本地落盘；
- 支持 pending / syncing / synced / failed；
- 支持重试和补传。

HeartbeatReporter
- 定期上报 active_requests、qps、error_rate、pending_sync_logs、版本等信息。
```

### 19.5 第一阶段接口清单

主站控制面接口（当前代码实际路径）：

```http
POST /api/internal/subsites/heartbeat
GET  /api/internal/subsites/config
POST /api/internal/requests/authorize
POST /api/internal/requests/cancel
POST /api/internal/usage/batch
POST /api/internal/leases/renew
POST /api/internal/leases/release
```

子站数据面接口建议：

```http
POST /v1/messages
POST /v1/chat/completions
POST /v1/responses
GET  /v1/responses        (Responses WebSocket)
POST /v1/images/generations
POST /v1/images/edits
POST /v1beta/models/*path
GET  /v1beta/models/*path
GET  /healthz
GET  /readyz
```

后台管理接口（当前代码实际路径）：

```http
GET    /api/admin/subsites
POST   /api/admin/subsites
PATCH  /api/admin/subsites/{id}
POST   /api/admin/subsites/{id}/pause
POST   /api/admin/subsites/{id}/resume
GET    /api/admin/subsites/{id}/leases
POST   /api/admin/subsites/{id}/activate
POST   /api/admin/subsites/{id}/leases
POST   /api/admin/subsites/{id}/leases/{lease_id}/drain
POST   /api/admin/subsites/{id}/leases/{lease_id}/release
POST   /api/admin/subsites/{id}/leases/{lease_id}/renew
```

### 19.6 数据库第一阶段最小表

第一阶段至少需要：

```text
subsites
- id
- name
- public_url
- region
- capabilities
- status
- join_token_hash
- join_token_expires_at
- secret_hash
- max_qps
- max_concurrency
- version
- last_heartbeat_at
- health_score
- created_at
- updated_at

account_leases
- id
- lease_id
- account_id
- subsite_id
- status
- max_concurrency
- max_requests
- max_tokens
- used_requests
- used_tokens
- assigned_at
- expires_at
- renewed_at
- released_at
- created_at
- updated_at

quota_reservations
- id
- reservation_id
- request_id
- user_id
- api_key_id
- estimated_cost
- actual_cost
- status
- expires_at
- created_at
- settled_at

subsite_heartbeats
- id
- subsite_id
- active_requests
- qps
- error_rate
- avg_latency_ms
- pending_sync_logs
- cpu_usage
- memory_usage
- version
- created_at
```

关键约束：

```text
account_leases: 同一个 account_id 只能存在一个 active/renewing/draining 租约
usage_logs: request_id + api_key_id 唯一
usage_billing_dedup: request_id + api_key_id 唯一
quota_reservations: request_id 唯一
subsites: public_url 唯一
```

### 19.7 用户本地客户端安全边界

未来可以做自有客户端，但它只能是“入口选择客户端”，不能是“请求执行节点”。

允许客户端做：

```text
登录主站
获取可用子站 URL
测速和选择最近子站
保存用户自己的 API Key
把模型请求发送到子站
在子站失败时切换到其他子站
展示用量和余额
```

禁止客户端做：

```text
保存上游账号 Cookie / Token / API Key
保存 subsite_secret
保存 master_secret
持有账号租约真实凭证
直接调用上游 AI 服务
本地决定额度、计费、账号归属
作为子站 agent 部署在用户机器上
```

原因：

```text
用户本地环境默认不可信。
客户端可以被反编译、调试、抓包和篡改。
任何下发到客户端的真实秘密，都应视为已经泄露。
```

安全原则：

```text
子站 agent 只能部署在你控制的服务器上。
用户本地只能部署普通客户端或入口选择器。
真正的上游凭证、主站密钥、子站密钥永远不能下发到用户本地。
```

### 19.8 开发顺序建议

建议按以下顺序开发：

```text
1. 新增 subsites / account_leases / quota_reservations 迁移
2. 实现主站 SubsiteService 和后台子站管理
3. 实现子站 pending / active / maintenance / unhealthy / disabled 状态机
4. 实现主站子站签名校验
5. 实现新子站预创建和激活流程
6. 实现主站 authorize API
7. 实现主站 usage batch API
8. 实现主站 heartbeat API
9. 新增 sub2api-subsite-agent 入口
10. 子站 agent 实现 MasterClient
11. 子站 agent 实现本地 UsageQueue
12. 子站 agent 复用现有上游调用能力跑通单模型请求
13. 跑通用户 -> 子站 -> 上游 -> 子站 -> 用户
14. 跑通子站 usage 上报和主站结算
15. 加入租约续期、release、draining
16. 后台展示子站状态、租约、未同步日志
17. 做异常场景测试：主站不可用、子站断线、重复 usage、账号回收、流式中断
```

第一版验收标准：

```text
主站带宽不再承载模型响应
用户可以通过主站展示的子站 URL 发起请求
子站没有主站后台和用户系统
无 authorize 不调用上游
usage 重复上报不重复扣费
子站断线不丢用量日志
账号不会同时分配给两个子站
主站可以暂停子站并阻止新请求
```

### 19.9 当前生产测试步骤

以当前仓库实现为准，第一轮生产测试建议只接入受控流量。

主站侧：

```text
1. 部署主站新版本，启动时自动执行 141_subsite_control_plane.sql。
2. 进入后台“子站管理”，新建子站，填写 name、public_url、region、capabilities。
3. 复制一次性显示的 subsite_secret。
4. 子站保持 pending，先不要激活。
```

子站侧 Docker Compose：

```text
1. 在子站服务器复制 deploy/docker-compose.subsite-agent.yml 和 deploy/.env.subsite.example。
2. 将 .env.subsite.example 复制为 .env.subsite。
3. 填入 SUBSITE_ID、SUBSITE_PUBLIC_URL、SUBSITE_MASTER_URL、SUBSITE_MASTER_SECRET。
4. 执行 docker compose --env-file .env.subsite -f docker-compose.subsite-agent.yml up -d --build。
5. 访问 /healthz 和 /readyz，确认 agent 存活。
6. 在主站后台确认 last_heartbeat_at 已更新。
```

激活和租约：

```text
1. 在主站后台激活子站。
2. 为子站创建至少一个账号租约。
3. 使用子站 public_url 发起测试请求，Authorization 仍使用主站 API Key。
4. 确认响应由子站返回，主站只产生 authorize / usage / heartbeat 控制面流量。
5. 确认 usage 最终进入主站日志和计费。
```

必须验证：

```text
1. 无 API Key 请求被拒绝。
2. pending / maintenance 子站无法 authorize。
3. 重复 usage 上报不会重复扣费。
4. 临时停止主站后，子站 usage_queue SQLite 文件仍保留未同步记录。
5. 主站恢复后，usage_queue 被批量上报并清空已确认记录。
6. 主站暂停子站后，新请求被拒绝，已有流式请求自然结束。
```

回滚方式：

```text
1. 主站后台暂停子站。
2. 停止子站 agent。
3. 释放或排空该子站账号租约。
4. 用户流量切回主站原入口或其他已验证子站。
```

---

## 20. 总结

该方案整体可行，并且比普通多实例部署更安全、更可控。

核心设计应该坚持：

```text
1. 主站是唯一权威数据源。
2. 子站只部署精简代理程序，不部署完整面板。
3. 子站只是数据面请求执行节点。
4. 用户登录、前端、额度、日志查询全部在主站。
5. 主站面板负责展示可用子站 URL 或统一入口配置。
6. 一个账号同一时间只能分配给一个子站。
7. 账号分配必须使用租约机制。
8. 账号回收必须使用 draining 排空机制。
9. 用户额度必须由主站预冻结和最终结算。
10. 子站不能直接写主站数据库。
11. 子站日志同步必须支持幂等、重试和补偿。
12. 子站请求必须经过主站 authorize 或校验主站签发的 ticket。
13. 上游真实凭证下发要短期、加密、可撤销。
14. 主站需要全局限流、全局并发和风控能力。
15. 主站和子站通信必须签名，最好支持 mTLS。
16. 子站异常时，主站可以暂停、回收、降级和对账。
```

一句话总结：

> 推荐将主站设计为控制中心和权威数据中心，将子站设计为无用户体系的轻量代理程序；主站通过子站 URL 管理、账号租约、请求 authorize/ticket、额度预冻结、日志幂等同步和 draining 回收机制，实现安全可控的多子站动态账号调度。

---
