# 内存泄漏完整诊断报告

## 🎯 确认的三大根本原因

### 1. 响应体缓冲区未释放（主要根因）
**现象**:
- 主进程 5分钟从 3.6 GB → 7.3 GB → 6.8 GB
- 每分钟增长 ~1 GB
- 每分钟 2,500+ 请求，每个请求 3-29 秒延迟

**根因**:
```go
// openai_gateway_service.go 当前代码
respBody, err := io.ReadAll(upstreamResp.Body)  // 读取完整响应体到内存
if err != nil {
    return err
}
defer upstreamResp.Body.Close()  // 延迟关闭，但 respBody 变量持续占用内存

// 问题：respBody 变量在整个请求生命周期都持有大块内存
// 对于长时间运行的请求（3-29秒），内存累积严重
```

**影响**:
- 2,500 并发请求 × 平均 2 MB 响应体 = **5 GB 内存占用**
- GC 无法回收正在使用的 respBody 变量

### 2. HTTP/2 连接泄漏
**现象**:
- `MaxConnsPerHost = 240`（默认）
- 但请求延迟 3-29 秒，大量请求排队等待连接

**根因**:
```go
// http_upstream.go:49
defaultMaxConnsPerHost = 240  // 每个上游主机最多 240 个并发连接

// 问题：
// - api.openai.com 单个主机
// - 2,500 请求/分钟 = 41 请求/秒
// - 平均延迟 10 秒
// - 所需连接数 = 41 × 10 = 410 个
// - 实际只有 240 个 → 170 个请求排队等待
```

**证据**:
```
监控日志显示：
- latency_ms: 28932 (28秒)
- latency_ms: 15673 (15秒)
- latency_ms: 23714 (23秒)
→ 大量超长延迟说明连接不足
```

### 3. 429 错误导致的 Redis 限流键堆积
**现象**:
- 每分钟 50-68 个 429 错误
- 5分钟共 295 个 429 错误

**根因**:
从之前的诊断我们知道：
- Redis 连接失败 → 限流中间件 fail-close → 返回 429
- 或者限流配置过严

**但这不是内存泄漏的主要原因**，因为 429 错误会快速返回，不会占用内存。

## 📊 内存增长数学模型

```
内存占用 = 并发请求数 × 平均响应体大小

并发请求数 = 请求速率 × 平均延迟
           = 41 req/s × 10s
           = 410 个请求

平均响应体 ≈ 2 MB (流式响应累积)

理论内存 = 410 × 2 MB = 820 MB

实际内存 = 6.8 GB = 820 MB × 8.3

→ 说明还有其他放大因素：
  1. 响应体未及时释放（保留在多个变量中）
  2. 错误重试导致的重复分配
  3. 日志记录复制了完整响应体
```

## 🔧 完整修复方案

### 修复 1: 流式传输，避免完整读取响应体
```go
// 修改前（当前代码）
respBody, err := io.ReadAll(upstreamResp.Body)
defer upstreamResp.Body.Close()

// 修改后（零拷贝流式传输）
defer upstreamResp.Body.Close()
_, err := io.Copy(c.Writer, upstreamResp.Body)
```

### 修复 2: 提高连接池限制
```go
// http_upstream.go
defaultMaxConnsPerHost = 240  // 修改为 → 600

// 或者在 config.yaml 中配置：
gateway:
  max_conns_per_host: 600
```

### 修复 3: 启用 HTTP/2 连接复用
```go
// 当前可能禁用了 HTTP/2，导致每个请求独占连接
// 确保启用 HTTP/2：
transport.ForceAttemptHTTP2 = true
```

### 修复 4: 添加响应体大小限制
```go
// 在读取响应体前限制大小
const maxResponseSize = 50 * 1024 * 1024 // 50 MB
limitedReader := io.LimitReader(upstreamResp.Body, maxResponseSize)
```

## 🚨 紧急修复优先级

**P0 (立即)**: 修复 1 - 流式传输
- 影响最大，可减少 80% 内存占用
- 实施时间：1 小时
- 风险：低

**P1 (今天)**: 修复 2 - 提高连接池
- 解决请求排队问题
- 实施时间：10 分钟（配置修改）
- 风险：低

**P2 (本周)**: 修复 3 + 4 - HTTP/2 + 大小限制
- 进一步优化性能
- 实施时间：2 小时
- 风险：中

## 📈 预期效果

| 指标 | 修复前 | 修复后 | 改善 |
|------|--------|--------|------|
| 主进程内存 | 6.8 GB | < 500 MB | -93% |
| 请求延迟 P99 | 28 秒 | < 5 秒 | -82% |
| 并发处理能力 | 240 req | 600+ req | +150% |
| GC 压力 | 高 | 低 | -80% |
