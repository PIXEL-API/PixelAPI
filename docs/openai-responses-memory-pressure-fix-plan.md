# OpenAI Responses 生产内存问题修复方案

日期：2026-09-04
状态：方案已收敛，尚未实施
范围：Sub2API/Pixel 的 OpenAI Responses HTTP 热路径；生产部署、重启、数据库、nginx 和 systemd 均不在本方案编写动作内

## 1. 结论

推荐按两个发布单元解决：

1. 稳定性 hotfix：在读取请求体之前增加独立的、按字节加权的内存准入；正常 HTTP 路径在进入网络等待前释放整棵 JSON map；重试改走 raw body helper；所有退出路径严格归还许可证。
2. 生命周期修复：把请求原文、分析视图、尝试级上游正文和计费 drain 的所有权拆开，在 retry/failover 不再可能且 Transport 已结束请求体读取后尽早断开大对象引用。

第一单元先把最坏内存变成有上界，第二单元再降低每个活跃请求的常驻内存。不能只做定时重启、强制 GC、降低 worker 数或设置 MemoryMax；这些措施最多改变爆炸时间或故障范围，不能消除当前保活链。

## 2. 已确认的现象和根因

### 2.1 现场证据

- T+5 到 T+10 的 inuse_space 约从 4058.98 MiB 增至 4963.42 MiB，增加约 904.44 MiB。
- Responses 主链约增加 895.19 MiB。
- ReadRequestBodyWithPrealloc 归因约增加 417 MiB。
- json.Marshal 归因约增加 411 MiB。
- trackedBody 仍保留约 874 MiB。
- 同期 goroutine 约从 4405 降至 4165，不支持“goroutine 数量持续泄漏”这一解释。
- UsageRecord 只有约 3.01 MiB 和 1 个 goroutine。昨晚针对异步 usage context 的修复有效，但它解决的是上一条引用链，不是本次在途 Responses 请求的主要保活轴。

### 2.2 当前调用链

1. backend/internal/handler/openai_gateway_handler.go:340 在用户并发槽之前完整读取并解压 body。
2. backend/internal/service/openai_responses_request_analysis.go:19-40 保存 Body 和多个 gjson.Result；这些结果通过 unsafe string view 指向原始 slice。它们不是额外的字节副本，但会阻止原始 body 被 GC 回收。
3. handler 为 compact 前的 session hash 另存 sessionHashBody；规范化产生新 slice 时，新旧 body 会同时存活。
4. backend/internal/service/openai_gateway_service.go:3670 把 body 完整解码为 map[string]any；复杂变换后在约 4050 行再次完整 Marshal。
5. HTTP 错误重试为了 invalid_encrypted_content 长期保留 openAIReqBody map，rejected-field retry 还会重新 Unmarshal。
6. backend/internal/service/openai_gateway_service.go:6454 用 bytes.NewReader(body) 创建上游请求。Response、Request、Body reader 和 handler 局部变量都可能继续引用同一 backing array。
7. 当前只有流式路径清理 resp.Request；非流式路径在最长 180 秒响应读取期间仍可能通过 resp.Request 保留请求体。
8. backend/internal/server/http.go:164-173 明确没有全局 ReadTimeout。直接把现有阻塞式用户槽移到 body 读取前，会让慢上传长期占用业务槽并改变现有错误优先级。

### 2.3 根因定义

这次问题更准确地说是高并发、长响应条件下的大对象 live-set 放大和生命周期过长，而不是请求结束后永不释放的传统泄漏：

    原始 body
      + analysis/gjson 对原始 body 的引用
      + compact 前旧 body
      + map[string]any 对象树
      + Marshal 后上游 body
      + HTTP Request/Response 引用
      + retry/failover 所需重放引用
      + 客户端断开后的有界 usage drain

只要大量长请求同时处于这些阶段，GC 就不能回收仍然可达的对象，内存会随活跃请求数和正文大小继续上升。

## 3. 目标、边界和默认决策

### 3.1 必须达到的目标

- 在同等请求体、并发、协议、成功率和吞吐下，活跃请求的总 body 预算有硬上界。
- HTTP 正常路径进入上游网络等待前，不再保留完整 map 与第二次不必要的 Unmarshal。
- 请求进入不可重试阶段后，原始 body、analysis raw view、attempt body 和 resp.Request 能按明确状态机释放。
- 保持 model 映射、clean relay、moderation、cyber preflight、session hash、重试、failover、usage exactly-once 和断连计费语义。
- 完成请求并经过两次 GC 后，heap trough 回到固定基线，而不是只把 OOM 延后。

### 3.2 默认决策

| 决策 | 推荐选择 | 原因 |
|---|---|---|
| 入口保护 | 新增独立的按字节内存准入 | 不复用用户/账号并发槽，不改变现有调度语义 |
| 满载响应 | 503 + Retry-After + 稳定错误码 | 表达全局临时容量不足，避免与用户配额 429 混淆 |
| body 上限 | 第一版保持现有 256 MiB 兼容边界 | 先解决保活和总预算，不能靠缩小合法输入掩盖问题 |
| 压缩正文 | 继续保持解压后 64 MiB 上限 | 当前代码已有 decompression bomb 防护 |
| usage drain | 保留现有流式 30 秒、非流式 180 秒契约 | 不能以丢 usage 或少计费换取内存下降 |
| user/account slot | 保持当前位置 | 避免慢上传占业务槽，也不改变等待与 SSE ping 语义 |
| 首次生产切换 | 单实例低峰原子切换 + 快速回滚 | 当前单节点直接启动第二个完整实例会带来 scheduler/outbox 重复消费风险 |
| 发布方式 | 两个发布单元 | 先建立硬上界，再优化生命周期，降低一次性改动风险 |

### 3.3 明确不做

- 不用定时重启作为长期方案。
- 不把 MemoryMax、GOMEMLIMIT 或强制 GC 当作根因修复。
- 不把几十至数百 MiB 的 body、map 或 encoder buffer 放入 sync.Pool。
- 不使用 io.Pipe + json.Encoder 绕开所有权问题；它会破坏可重放和重试语义，并增加阻塞 goroutine。
- 不在 gjson 仍别名原 slice或 HTTP/2 Transport 仍可能读取时原地覆盖、清零或复用 body。
- 不顺带修改账号调度、重试次数、SSE keepalive、nginx timeout、数据库或 scheduler。

## 4. 目标架构

### 4.1 请求体内存准入

新增 OpenAI Responses 专用 BodyMemoryGate，使用项目已经依赖的 golang.org/x/sync/semaphore.Weighted。准入必须发生在第一次读取 body 之前。

建议配置项：

- gateway.openai_responses_body_budget.enabled
- gateway.openai_responses_body_budget.capacity_bytes
- gateway.openai_responses_body_budget.wait_timeout_seconds
- gateway.openai_responses_body_budget.read_timeout_seconds
- gateway.openai_responses_body_budget.retry_after_seconds

配置规则：

- enabled=true 时所有数值必须为正，否则启动时 fail-fast。
- capacity_bytes 必须至少容纳一个合法的最大 reservation，否则配置校验失败。
- 生产值不能凭经验写死；应由同版本 soak 的每字节 p99 live-set 放大倍数、应用稳态基线、非堆内存和主机恢复余量计算。
- 发布门必须回读运行时生效值；不能仅凭配置文件内容判断已启用。

reservation 规则：

- identity 且 Content-Length 已知：按声明长度向上取整到固定粒度，并受全局 body limit 约束。
- gzip、zstd、deflate：按现有 64 MiB 解压后最坏值预留，不能只按压缩后 Content-Length 计费。
- Content-Length 未知或 chunked：按当前全局最大 body 值预留。
- 请求头声明超过合法上限时直接走现有 413，不进入业务调度。

这里选择“读取前一次性保守预留”，而不是边读边申请。边读边申请会让多个请求各自持有一部分额度后同时等待下一块，产生新的资源死锁或长时间停顿风险。

许可证生命周期：

1. 获取成功后才允许 ReadRequestBodyWithPrealloc 开始读取。
2. 任何读取、解压、JSON 校验、权限、路由、上游和 panic 路径都只释放一次。
3. 第一发布单元允许许可证保守地持有到整个请求结束，以先建立硬上界。
4. 第二发布单元完成明确所有权后，才把释放点前移到 request payload 确实不可再重放且 Transport 已结束请求体读取的位置。

慢上传保护：

- 获取许可证后，通过 http.NewResponseController(c.Writer).SetReadDeadline 设置 Responses 端点局部 body read deadline。
- body 读取完成后用零值 deadline 清除，不影响长时间 SSE 响应。
- 不设置全局 http.Server.ReadTimeout。
- 如果当前 writer 不支持 deadline，必须显式失败并记录低基数错误，不能静默退化成无期限持有许可证。

容量不足行为：

- 在等待超时后返回 503。
- 返回 Retry-After。
- 使用稳定的内部错误码 gateway_memory_budget_exhausted。
- 日志只记录 reservation、capacity、in_use、wait_duration、编码和 endpoint，不记录请求体、Authorization 或敏感 header。

### 4.2 请求级 payload 所有权

新增一个请求级 ResponsesRequestPayload 所有者，集中管理：

- 当前可重放的 canonical body。
- compact 前原文派生出的 session hash、clean-relay signal 和 request payload hash。
- 内存准入许可证。
- 是否仍允许 retry/failover。
- 请求体 reader 是否已 EOF/Close。
- 是否已完成 Finalize。

约束：

- handler 和 service 不再各自长期保存独立的 []byte slice header。
- 不暴露可被调用方长期保存的可变 body；只允许在受控作用域内读取。
- Finalize 使用 sync.Once，清理所有字段并归还许可证。
- 不能通过清零 backing array来“释放”；清零会和 HTTP/2 写协程产生数据竞争或请求损坏。只解除已确认安全的所有权引用。
- Transport 侧使用带 EOF/Close 通知的只读 Body 包装器。上游 Do 返回响应头并不被视为“请求体写协程一定结束”的充分证据。

### 4.3 分析结果物化

将 OpenAIResponsesRequestAnalysis 拆成两层：

1. 临时 raw analysis：只在同步分析阶段存在，允许持有 gjson raw view。
2. owned metadata：只保存后续阶段真正需要的自有数据或小型摘要。

owned metadata 至少包含：

- model、modelExists、stream、promptCacheKey、previousResponseID。
- function-call-output 校验结果。
- request payload hash。
- 已计算的 session hash 和 legacy hash。
- image intent 等布尔判断。
- clean-relay 所需 installation/session signal，不再传整份 bodyForSession。
- moderation/cyber 输入的受控快照或已完成判断；若输入本身很大，必须在最后一个审核消费者结束后立即释放，不能默认缓存到响应结束。

调整点：

- 删除 sessionHashBody 长生命周期；在 compact 规范化前一次性计算小型派生值。
- WithBodyAndModel 不再浅复制带 raw gjson alias 的 analysis。
- analysis raw view 在所有 route/account 审核消费者结束前不能提前释放；如 failover 需要重复审核，应复用 owned moderation snapshot，而不是保留整份 gjson root。
- setOpsRequestContext 和 setOpsUpstreamRequestBody 继续遵守现有小体积截断上限，不得扩大为完整正文留存。

### 4.4 上游 JSON 准备作用域

把 HTTP 路径的正文变换收口为 prepareOpenAIResponsesUpstreamRequest 一类的短生命周期 helper：

输入：canonical body、owned metadata、account/route policy。
输出：最终上游 bytes、计费所需小型 metadata、retry state。
禁止输出：map[string]any 或持有原始 gjson view 的对象。

实现顺序：

1. no-op：字节级原样透传，不 Marshal。
2. 单字段 set/delete：继续使用现有 sjson raw patch 路径。
3. 复杂 OAuth/Codex/image/clean-relay 变换：允许局部 map + Marshal，但 map 必须在 helper 返回前变为不可达。
4. invalid_encrypted_content：改用现有 trimOpenAIEncryptedReasoningItemsInRawBody，删除为该重试长期保留 openAIReqBody map 的需求。
5. rejected-field retry：继续使用 raw-body helper，删除 retry 后的完整重新 Unmarshal。
6. WSv2 当前仍需要结构化 map，保留独立分支，不为了 HTTP hotfix 大改 WS 协议实现。

必须验证未知字段、超过 2^53 的 JSON 数字、Unicode、转义字符串、tools、input 和 base64 在允许变更字段之外完全保持原语义。

### 4.5 retry/failover 与释放边界

不得提前释放的阶段：

- 上游尚未完整消费请求体。
- invalid_encrypted_content retry 仍可能发生。
- rejected-field retry 仍可能发生。
- 同账号重试、换账号或换 route 仍可能发生。
- 流式首个语义输出尚未成功提交，现有逻辑仍允许安全 failover。
- 非流式响应尚未完整读取和验证，现有逻辑仍可能返回可 failover 错误。

可以释放的阶段：

- HTTP reqBody map：最终序列化完成，raw retry helper 已建立后立即释放；不等网络响应。
- compact 前旧 body：session hash、clean-relay signal 和 request hash 物化完成后立即释放。
- resp.Request：所有基于状态码的 retry/failover 决策完成，进入成功响应处理前，流式和非流式统一清理；先确认当前文件没有后续读取者。
- handler canonical body：由状态机在“业务上不可再 failover”和“Transport 不再读取请求体”两个条件同时满足后释放。
- detached usage drain：只持有计费所需小对象；不得捕获 payload、gin.Context、http.Request、analysis 或 map。

### 4.6 客户端断开状态机

该部分放在第二发布单元，不与稳定性 hotfix 同时上线：

- 响应头前客户端断开：取消上游请求，不允许无界继续等待。
- 已输出首个语义事件后客户端断开：只保留现有有界 usage drain；大 body 和请求分析必须已释放。
- 流式 drain 最长 30 秒、非流式读取最长 180 秒的现有契约保持不变。
- lease loss 始终优先取消。
- 错误响应不进入 usage drain。
- 关闭 drain 配置时立即停止，健康客户端不受断连 drain 上限影响。

## 5. 文件级实施清单

### Sprint 0：冻结基线和行为契约

S0-1 记录当前工作树和 hot path 精确 diff

- 文件：全仓库只读检查。
- 动作：记录 git status、当前 commit、Go 版本和相关文件 diff；不要 reset、clean 或覆盖用户修改。
- 产物：实现使用独立 clean worktree，补丁只包含本计划范围。
- 验收：能把每一行 hotfix 映射回当前业务修改；无无关文件进入补丁。

S0-2 固化内存基线

- 文件：backend/internal/service 与 backend/internal/handler 的测试基础设施；临时压测输出放 test 目录。
- 动作：对现版本跑 16 x 4 MiB 的四阶段 barrier 测试和至少三次基线；采集 heap、goroutine、槽位、body close 和 SHA-256。
- 依赖：无。
- 验收：阈值在看修复结果前冻结，修复后不得为了通过而放宽。

S0-3 固化请求语义

- 动作：保存 no-op、单 patch、复杂 rewrite、invalid encrypted、rejected field、failover、断连 usage 的 golden assertions。
- 验收：原始透传必须逐字节相同；其他路径只允许预期字段变化。

### Sprint 1：稳定性 hotfix

S1-1 增加配置和校验

- 文件：backend/internal/config/config.go、对应 config tests 和部署模板。
- 动作：增加 body budget 五个配置项、正值校验、容量校验和脱敏启动日志。
- 验收：enabled 配置缺失或矛盾时启动失败；disabled 时现有测试配置不被意外破坏。

S1-2 实现加权 gate 和 lease

- 文件：建议新建 backend/internal/service/openai_responses_body_budget.go，并增加同包单测。
- 动作：封装 semaphore.Weighted、超时、sync.Once release、指标快照和 panic-safe defer。
- 验收：取消、超时、解析失败、panic、正常完成均只释放一次；无负数、双释放或 permit 泄漏。

S1-3 在读 body 前接入

- 文件：backend/internal/handler/openai_gateway_handler.go。
- 动作：认证和依赖检查后、第一次 body.Read 前计算 reservation 并获取 lease；设置端点局部 read deadline；读取完成后清除 deadline。
- 验收：gate 拒绝时自定义 Body reader 的 Read 调用次数严格为 0；现有 user/account slot 位置不变。

S1-4 缩短 HTTP map 生命周期

- 文件：backend/internal/service/openai_gateway_service.go、backend/internal/service/openai_clean_relay.go。
- 动作：将 HTTP map 约束在 prepare helper；invalid encrypted 改 raw helper；rejected-field retry 不再完整 Unmarshal；成功路径统一清理 resp.Request。
- 验收：正常 HTTP 路径在发起网络等待前 map 已不可达；WSv2 行为不变。

S1-5 增加可观测性

- 指标：capacity_bytes、in_use_bytes、waiters、admission_rejected_total、wait_seconds、reserved_bytes、active_responses、body_read_timeout_total。
- 日志：只记录低基数原因和数值，禁止正文与凭据。
- 验收：可用只读指标判断“因保护而拒绝”与“上游/用户并发拒绝”，两者不能混为一个 429 指标。

Sprint 1 依赖关系：S1-1 -> S1-2 -> S1-3；S0-3 -> S1-4；S1-3 和 S1-4 完成后做 S1-5 联调。

### Sprint 2：生命周期修复

S2-1 物化 owned metadata

- 文件：backend/internal/service/openai_responses_request_analysis.go 及测试。
- 动作：拆分 raw analysis 与 owned metadata；提前计算 session/clean-relay/hash；移除 WithBodyAndModel 的 raw alias 浅复制。
- 验收：释放 raw analysis 后所有后续逻辑仍可运行；释放后访问 raw-only API 必须 fail-fast。

S2-2 引入请求级 payload 所有者

- 文件：backend/internal/handler/openai_gateway_handler.go、backend/internal/service/openai_gateway_service.go，必要时新增同包文件。
- 动作：统一 canonical body、attempt body、lease 和 release 状态；删除跨长调用的散落 slice 所有权。
- 验收：同一 backing array 无未经状态机管理的长期引用；所有退出路径 Finalize 一次。

S2-3 绑定 Transport 完成边界

- 文件：上游 request builder 与 HTTP 测试。
- 动作：为 request Body 增加 EOF/Close 通知；只有 Transport 结束读取且业务不可重试时释放 canonical body；不修改或复用正在被 Transport 读取的字节。
- 验收：慢 HTTP/2 上游收到的长度和 SHA-256 完全正确，race 测试无数据竞争。

S2-4 拆分 client lifecycle 与 billing drain

- 文件：backend/internal/service/openai_gateway_service.go 现有 detached context 辅助逻辑及测试。
- 动作：实现显式 openAIUpstreamLifetime 状态机，保持 usage exactly-once 和现有有界 drain。
- 验收：响应头前断连取消；首个语义输出后仍能得到终态 usage；deadline 到期强制关闭；大 body 不随 drain 存活。

Sprint 2 依赖关系：S2-1 -> S2-2 -> S2-3 -> S2-4。

### Sprint 3：回归、性能和内存证明

S3-1 功能矩阵

- passthrough no-op、非 passthrough no-op、单字段 patch、复杂 rewrite。
- 1 KiB、1 MiB、4 MiB、8 MiB，大 tools、大 base64、混合 input。
- empty、非法 JSON、刚好上限、上限 + 1、gzip、zstd、deflate、chunked。
- invalid encrypted、rejected field、同账号、换账号、换 route、WS 到 HTTP 的既有边界。
- user/account/session slot 满、等待、超时、Redis 错误、父 context 取消。
- 流式首输出前/后断连、只输出 keepalive 后断连、非流式慢 body。

S3-2 HTTP/2 慢上传

- 使用 httptest.NewUnstartedServer 并启用 HTTP/2。
- 上游慢速读取并流式计算 SHA-256，不保存完整 body。
- 每个并发请求使用不同 sentinel。
- 验收：无串包、截断、提前复用、双 close 或 goroutine 泄漏。

S3-3 live-heap barrier

定义：

- B：预热后连续两次 runtime.GC 的 HeapAlloc 基线。
- G：同一时点 goroutine 基线。
- P：服务器本轮实际接收的请求体总字节数。

PR 级场景使用 16 x 4 MiB，P=64 MiB：

| Barrier | 场景 | 建议起始门 |
|---|---|---|
| S1 | 请求体正在向慢上游发送 | HeapAlloc - B <= 2.0 x P + 32 MiB |
| S2 | 上游读完请求并已提交首个语义输出，响应继续阻塞 | HeapAlloc - B <= 0.5 x P + 32 MiB |
| S3 | 客户端断开，usage drain 仍继续 | HeapAlloc - B <= 0.5 x P + 32 MiB |
| S4 | 全部完成并两次 GC | HeapAlloc <= B + 32 MiB；goroutine <= G + 5；slots 和 permits 为 0 |

重复至少 20 轮：最后 5 轮 post-GC trough 中位数减前 5 轮中位数，不得超过 max(16 MiB, 0.05 x P)。最后一次 trough 不得超过 B + 32 MiB。

S3-4 benchmark

覆盖 noop、single_sjson_patch、forced_rewrite、large_tools、large_base64、invalid_encrypted_retry；每组 1 MiB 和 8 MiB，运行 benchmem count=5。

起始目标：

- no-op B/op 不超过输入大小的 0.25 倍，且相对当前基线下降至少 75%。
- 单字段 patch B/op 不超过输入大小的 1.5 倍。
- 强制 rewrite 或一次 retry B/op 不超过输入大小的 2.5 倍，且相对当前基线下降至少 50%。
- benchmark 不能替代 live-heap barrier。

S3-5 30 分钟 soak

- 32 x 8 MiB，混合流式、非流式、HTTP/2、断连 drain。
- 默认 GC 配置跑一轮；GOMEMLIMIT=1GiB、GOGC=50 再跑一轮。
- 至少覆盖 10 个 180 秒 retention 周期和 20 次完整 GC。
- 请求数、成功率和吞吐必须与基线等价，禁止通过降低并发、缩小 body 或大量拒绝请求过门。

### Sprint 4：生产低风险切换

S4-1 新授权与只读预检

- 需要新的 MAIN_DEPLOY=yes。
- DB 仅允许读取 schema_migrations 的 filename/checksum；预期没有 migration。发现 pending migration 立即阻断。
- scheduler 只读范围需在部署前再次确认；此前诊断授权不自动扩展为部署或写入授权。
- nginx、systemd、真实业务 smoke、Git commit/push/tag 均是独立授权。
- 核对运行 binary Version/Commit/BuildType/SHA256、current symlink、previous release、服务 PID/NRestarts、memory.events 和主机恢复余量。

S4-2 构建和 staging

- 从精确生产源建立独立 clean worktree，仅移入本修复补丁。
- 本地构建 linux/amd64 binary 和同源码 resources；记录大小、SHA-256 和版本。
- 远端进入 quarantine 后复核 hash、ELF、owner/mode；不在生产机编译，不启动第二个完整 scheduler 实例。
- 创建独立 release 目录，原子 current symlink 切换，保留 previous release。

S4-3 时间灰度

1. 选择低峰窗口并冻结 pre baseline。
2. 原子切换 current，执行一次受控 restart。
3. 60 秒内验证 stable PID、active/running、NRestarts delta=0、三个 health endpoint 为 200、release/hash 正确。
4. 最多 130 秒完成 scheduler 初始闭合和两组 outbox 样本。
5. 锁内至少观察现场 full_rebuild_interval + 130 秒。
6. 释放维护锁后继续观察至少 2 至 3 小时，必须超过旧版约 1 小时 53 分的失败周期；24 小时稳定后才关闭事故。

S4-4 生产成功门

- 相同请求体分布、active Responses 和上游时长下，inuse_space / active_responses 落入本地修复版 soak envelope。
- warm-up 后 HeapInuse 和 MemoryCurrent 的归一化 trough 不再持续正斜率。
- ReadRequestBodyWithPrealloc、json.Marshal、request/response body 主栈的 live bytes 较问题基线下降至少 70%，且不再随长请求累计。
- 低流量并完成 GC 后，heap 回到发布前稳态基线的 115% 内。
- goroutine、用户/账号/session slot、HTTP in-flight、body budget permits 均能回落。
- TTFT、5xx、stream incomplete、usage 完整性和 failover 成功率不越过发布前冻结的 SLO/统计波动门。

S4-5 自动回滚门

任一项出现立即切回 previous release 并只重启一次：

- readiness 超时、panic、OOM/oom_kill、自动 restart。
- current/binary 身份不一致。
- scheduler 或 outbox 门失败。
- PostgreSQL、Redis、nginx 依赖异常。
- MemoryCurrent 达到预先冻结的 hard ceiling，或 MemAvailable 低于 recovery reserve。
- controlled smoke 出现请求体 hash 错误、未知字段丢失、usage 丢失/重复、retry/failover 语义变化。
- 连续三个可比 5 分钟窗口的归一化 heap trough 仍单调增长。

本次应无数据库 migration；回滚只切 binary/resources，不回退 schema。

## 6. 本地验证命令

定向语义回归：

    go test -C backend -tags unit ./internal/pkg/httputil ./internal/handler ./internal/service -run 'Test(OpenAI|RequestBody|WaitForSlot|WrapRelease)' -count=1 -timeout=20m

全量门：

    go test -C backend -tags unit ./... -count=1 -timeout=30m
    go vet -C backend -tags unit ./...

非 tag 套件与 lint：

    Push-Location backend
    try {
        go test ./... -count=1 -timeout=30m
        golangci-lint run ./...
    } finally {
        Pop-Location
    }

benchmark：

    go test -C backend ./internal/service -run '^$' -bench '^BenchmarkOpenAIResponses(Prepare|ForwardBody)' -benchmem -count=5

race 仅在 Windows 已配置受支持的 CGO/C 编译器时本地运行，否则必须由 Linux CI 执行，不能把跳过计作通过。

## 7. 风险与防误修

| 风险 | 错误做法的影响 | 本方案控制 |
|---|---|---|
| 慢上传占槽 | 把现有阻塞用户槽前移会长期占业务并发 | 独立字节 gate + 端点局部读取 deadline |
| JSON 精度/字段损坏 | 全量 map round-trip 可改变大数字或未知字段 | no-op 原样、单 patch raw、复杂路径 golden test |
| HTTP/2 数据竞争 | 提前清零或复用 body 会串包、截断 | EOF/Close 通知 + SHA-256 + race 门 |
| retry/failover 退化 | 过早释放 canonical body 导致不可重放 | 明确业务 commitment 状态机 |
| usage 丢失 | 为降内存关闭 drain 会少计费 | 保持 30/180 秒和 exactly-once 测试 |
| 假修复 | 只看 RSS 或缩小并发会把 OOM 延后 | 固定 workload、live heap barrier、20 轮 trough、30 分钟 soak |
| 大 buffer 常驻 | sync.Pool 会长期保留峰值对象 | 大 body/map 不进 pool |
| 脏工作树污染发布 | 用户已有大量未提交内容 | 独立 clean worktree 和最小补丁清单 |

## 8. 旁路安全阻断项

当前工作区已有未跟踪的运维文档包含明文生产凭据。它不是本次内存根因，但会阻断任何 Git 发布或对外共享。进入实现前应单独完成：

1. 不回显凭据地移除文档中的明文。
2. 对相关 SSH、Redis 等凭据执行轮换。
3. 扫描工作树、暂存区和 Git 历史，确认没有密钥残留。
4. 轮换和删除属于独立高风险动作，必须获得明确授权；本方案没有执行。

## 9. 参考依据

- Go x/sync/semaphore Weighted：Acquire 支持 context 取消，TryAcquire 为非阻塞尝试。官方文档：https://pkg.go.dev/golang.org/x/sync/semaphore
- Go http.ResponseController.SetReadDeadline：可设置覆盖整个请求（含 body）的读取 deadline，零值可清除。官方文档：https://pkg.go.dev/net/http#ResponseController.SetReadDeadline

## 10. 建议执行顺序与预计窗口

1. Sprint 0：半天，冻结行为和内存基线。
2. Sprint 1：半天到一天，完成稳定性 hotfix 与定向测试。
3. Sprint 2：一天左右，完成所有权和断连状态机。
4. Sprint 3：半天到一天，跑全量、race/CI、benchmark 和 soak。
5. Sprint 4：获得独立授权后，低峰切换；现场强观察 2 至 3 小时，24 小时后闭环。

工期取决于当前脏工作树与 clean worktree 的补丁冲突、Windows race 环境，以及 baseline 中真实请求变换分支覆盖率。任何一项内存门、语义门或生产预检失败，都停止进入下一阶段。
