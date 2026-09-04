# 内存泄漏根因分析与修复方案

## 问题现象

**服务器**: 207.32.218.139
**时间**: 2026-09-03
**症状**:
- 全站返回 429/403 错误
- 内存异常增长：~946MB/分钟
- 服务重启后从 52GB 降至 2GB，但很快又开始增长

## 根本原因

### 攻击者行为
- **来源 IP**: 156.246.179.21
- **User-Agent**: `Go-http-client/2.0`（非官方客户端）
- **请求大小**: 每个 3MB（3080854, 3180028, 3132498 字节）
- **频率**: 每秒数十个请求
- **结果**: 被 `codex_cli_only` 检查拒绝（403 Forbidden）

### 内存泄漏链条

1. **请求到达** (第 289 行)
   ```go
   body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
   ```
   → 读取 3MB body 到内存

2. **账号选择** (第 509-595 行)
   → 选择账号

3. **转发处理** (第 685 行)
   ```go
   result, err := h.gatewayService.ForwardWithAnalysis(forwardCtx, c, account, forwardBody, forwardAnalysis)
   ```

4. **codex_cli_only 检查** (第 2838-2848 行)
   ```go
   if restrictionResult.Enabled && !restrictionResult.Matched {
       c.JSON(http.StatusForbidden, gin.H{...})
       return nil, errors.New("codex_cli_only restriction...")
   }
   ```
   → 拒绝请求，返回 403

5. **异步计费任务提交** (第 689-701 行)
   ```go
   h.submitUsageRecordTask(forwardCtx, func(ctx context.Context) {
       usageCtx := service.WithAccountShareModeRequestFromContext(ctx, forwardCtx)
       // ...
   })
   ```
   → **问题所在**：闭包捕获了 `forwardCtx`

6. **任务排队**
   - Worker Pool 队列大小：16384
   - 每个任务持有 `forwardCtx` → gin.Context → Request → Body (3MB)
   - **16384 × 3MB = 49GB** 理论最大内存占用

### 为什么 GC 无法回收？

```
异步任务闭包 → forwardCtx (context.Context)
                    ↓
          gin.Context (c *gin.Context)
                    ↓
          c.Request (*http.Request)
                    ↓
          Request.Body (3MB 数据)
```

只要异步任务在 Worker Pool 队列中等待，整条引用链都无法被 GC 回收。

## 修复方案

### 上游解决方案

上游项目 (https://github.com/Wei-Shaw/sub2api) 已经有完整的修复：

**1. `usageRecordContext` 函数**
```go
func usageRecordContext(parent context.Context, base context.Context) context.Context {
    if base == nil {
        base = context.Background()
    }
    if parent == nil {
        return base
    }
    // 只提取必要的字段
    if clientRequestID, _ := parent.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
        base = context.WithValue(base, ctxkey.ClientRequestID, strings.TrimSpace(clientRequestID))
    }
    if requestID, _ := parent.Value(ctxkey.RequestID).(string); strings.TrimSpace(requestID) != "" {
        base = context.WithValue(base, ctxkey.RequestID, strings.TrimSpace(requestID))
    }
    return base
}
```

**2. `wrapUsageRecordTaskContext` 函数**
```go
func wrapUsageRecordTaskContext(parent context.Context, task service.UsageRecordTask) service.UsageRecordTask {
    if task == nil {
        return nil
    }
    return func(ctx context.Context) {
        task(usageRecordContext(parent, ctx))  // 只传递提取后的精简 Context
    }
}
```

**3. 修改 `submitUsageRecordTask`**
```go
func (h *OpenAIGatewayHandler) submitUsageRecordTask(requestCtx context.Context, task service.UsageRecordTask) {
    if task == nil {
        return
    }
    task = wrapUsageRecordTaskContext(requestCtx, task)  // ← 关键修改
    if h.usageRecordWorkerPool != nil {
        // ...
    }
    // ...
}
```

### 修复效果

**修复前**:
- 异步任务持有完整请求 Context 链
- 3MB body 无法被 GC 回收
- 内存持续增长至 OOM

**修复后**:
- 异步任务只持有必要字段（ClientRequestID, RequestID）
- 请求结束后，gin.Context 和 body 立即可被 GC 回收
- 内存稳定，不再泄漏

## 部署记录

**文件修改**:
- `backend/internal/handler/openai_gateway_handler.go`
  - 添加 `usageRecordContext` 函数
  - 添加 `wrapUsageRecordTaskContext` 函数
  - 修改 `submitUsageRecordTask` 函数
  - 添加 `ctxkey` 包导入

**编译**: ✅ 通过
**部署时间**: 2026-09-03
**部署方式**: 二进制热替换 + 服务重启

## 验证方法

### 1. 内存监控
```bash
# 监控应用进程内存（每 30 秒）
while true; do
    date '+%Y-%m-%d %H:%M:%S'
    ps aux | grep -E '^(USER|sub2api)' | grep -v grep
    echo ""
    sleep 30
done
```

**预期结果**: 内存稳定在 2-3GB，不再持续增长

### 2. Redis 计费任务队列
```bash
# 检查队列堆积情况
redis-cli -a 's8WxN2kP9mFqR7tL' INFO stats | grep instantaneous
```

**预期结果**: 队列处理速度正常

### 3. 日志监控
```bash
# 监控 403 拒绝日志
journalctl -u pixel.service -f | grep "codex_cli_only"
```

**预期结果**: 拒绝日志正常，但内存不再增长

## 技术要点

### 为什么不能拒绝非官方客户端？
用户可能使用各种客户端调用 API：
- curl
- Postman
- 自定义程序
- SDK

**拒绝非官方客户端会导致合法用户无法使用服务**。

### 为什么不是 body.go 的问题？
`ReadRequestBodyWithPrealloc` 本身没有问题：
- 它正确读取 body
- 它不持有额外引用
- 问题在于**谁持有了这个 body**

### 关键设计原则
**异步任务不应持有完整的请求 Context**，原因：
1. Context 链可能很长（gin.Context → http.Request → Body）
2. 大对象（如 3MB body）会占用大量内存
3. 异步任务可能排队很久，导致内存无法回收

**正确做法**：
- 提前提取需要的字段（如 RequestID）
- 创建新的轻量 Context
- 只传递新 Context 给异步任务

## 相关链接

- 上游仓库: https://github.com/Wei-Shaw/sub2api
- 相关函数位置: `backend/internal/handler/openai_gateway_handler.go`
  - `usageRecordContext`: ~第 220 行
  - `wrapUsageRecordTaskContext`: ~第 234 行
  - `submitUsageRecordTask`: ~第 3093 行
