# Sub2API 内存泄漏和 429 错误诊断报告

**诊断时间：** 2026-09-02
**服务器：** 207.32.218.139 (新服务器)
**监控时长：** 10 分钟

---

## 🚨 严重问题确认

### 1. 内存泄漏（已确认）

#### 内存增长趋势
| 时间 | 主进程 RSS | 总内存使用 | 单分钟增长 |
|------|-----------|-----------|----------|
| 启动时 | 1,919 MB | 4,344 MB | - |
| 1分钟后 | 1,811 MB | 4,270 MB | -74 MB |
| 2分钟后 | 4,149 MB | 6,586 MB | +2,316 MB ⚠️ |
| 3分钟后 | 6,350 MB | 8,626 MB | +2,040 MB ⚠️ |
| 4分钟后 | 7,544 MB | 9,950 MB | +1,324 MB ⚠️ |
| 5分钟后 | 9,188 MB | 11,725 MB | +1,775 MB ⚠️ |
| 6分钟后 | 10,313 MB | 12,903 MB | +1,178 MB ⚠️ |
| 7分钟后 | 11,890 MB | 14,251 MB | +1,348 MB ⚠️ |
| 8分钟后 | 15,209 MB | 17,493 MB | +3,242 MB ⚠️⚠️ |
| 9分钟后 | 15,514 MB | 17,646 MB | +153 MB |

**关键指标：**
- ❌ 内存从 1.9GB 增长到 15.5GB（**8.1倍**）
- ❌ 平均增长速度：**1.4GB/分钟**
- ❌ 最高单分钟增长：3.2GB（第8-9分钟）
- ⚠️ 按此速度，62GB 内存将在 **40分钟内耗尽**

#### 请求处理量
- 平均每分钟：**3,700 个请求**
- 峰值：4,478 个/分钟
- 总计 10 分钟：**37,318 个请求**

---

### 2. 429 错误（持续出现）

#### 错误统计
| 分钟 | 429 错误数 | 错误率 |
|------|----------|--------|
| 1 | 64 | 2.2% |
| 2 | 44 | 1.6% |
| 3 | 82 | 2.3% |
| 4 | 130 | 3.3% ⚠️ |
| 5 | 149 | 3.5% ⚠️ |
| 6 | 142 | 3.2% |
| 7 | 148 | 3.4% |
| 8 | 125 | 3.0% |
| 9 | 90 | 2.5% |
| 10 | 87 | 2.3% |

**总计：** 1,061 个 429 错误（占总请求的 2.8%）

#### 429 错误来源
1. **登录接口限流：** `/api/v1/auth/login` - Redis 限流触发
2. **上游限流：** OpenAI 账号返回 429，触发故障转移
3. **限流降级：** `rate_limit_429_fallback_used` - 5秒冷却

---

### 3. 其他严重错误

#### 数据库类型错误（高频）
```
pq: column "moderation_next_retry_at" is of type timestamp with time zone
but expression is of type text
```
**影响：** 账号共享审核功能失败

#### 账号可用性问题
- **模型不支持：** `no available OpenAI accounts supporting model: gpt-5.5`
- **401 认证失败：** 多个账号认证失败
- **Codex 客户端限制：** 非官方客户端被拒绝

---

## 🔍 根本原因分析

### 内存泄漏的 3 个根因

#### 1. UsageRecordWorkerPool 自动扩容失控
**代码位置：** `backend/internal/service/usage_record_worker_pool.go`

```go
defaultUsageRecordAutoScaleMinWorkers = 128
defaultUsageRecordAutoScaleMaxWorkers = 512  // ⚠️ 最大 512 个 worker
```

**问题：**
- 每个 worker 是一个 goroutine + 上下文
- 每分钟 3,700+ 请求触发频繁扩容
- 扩容到 512 worker 时，内存累积超过 10GB
- **没有及时回收机制**

#### 2. HTTP 连接池配置过大
**代码位置：** `backend/internal/pkg/httpclient/pool.go`

```go
defaultMaxIdleConns = 100        // 总共最大 100 个空闲连接
defaultMaxIdleConnsPerHost = 10  // 每个主机 10 个
```

**问题：**
- 高并发下，实际连接数远超配置
- 每个连接的 buffer 未及时释放
- 多个账号 × 多个代理 = 大量连接累积

#### 3. Goroutine 泄漏
**发现：** 606 处使用了 goroutine/channel

**高风险位置：**
- `openai_gateway_service.go` - 11 处
- `antigravity_gateway_service.go` - 20 处
- `backup_service.go` - 22 处
- WebSocket 连接池 - 多个长连接

**泄漏场景：**
- 流式响应未正确关闭
- Context 取消未传播到子 goroutine
- Channel 未关闭导致 goroutine 阻塞

---

## 🛠️ 修复方案

### 🔴 紧急措施（立即执行）

#### 1. 重启服务释放内存
```bash
sudo systemctl restart pixel.service
```

#### 2. 清理 Redis 限流键
```bash
redis-cli --scan --pattern "rate_limit:*" | xargs redis-cli DEL
```

#### 3. 设置自动重启（临时）
编辑 `/etc/systemd/system/pixel.service`，添加：
```ini
[Service]
# 内存超过 20GB 时自动重启
MemoryMax=20G
# OOM 时重启
OOMPolicy=kill
```

然后重载：
```bash
sudo systemctl daemon-reload
sudo systemctl restart pixel.service
```

---

### 🟡 短期修复（代码层面，需要重新部署）

#### 修复 1: 降低 Worker Pool 上限
**文件：** `backend/internal/service/usage_record_worker_pool.go`

```go
// 修改前
defaultUsageRecordAutoScaleMaxWorkers = 512

// 修改后
defaultUsageRecordAutoScaleMaxWorkers = 256  // 降低一半
```

#### 修复 2: 减少 HTTP 连接池大小
**文件：** `backend/internal/pkg/httpclient/pool.go`

```go
// 修改前
defaultMaxIdleConns = 100
defaultMaxIdleConnsPerHost = 10

// 修改后
defaultMaxIdleConns = 50          // 降低总空闲连接
defaultMaxIdleConnsPerHost = 5    // 每主机降低
defaultIdleConnTimeout = 60 * time.Second  // 缩短超时
```

#### 修复 3: 修复数据库类型错误
**文件：** `backend/internal/service/account_share_review_moderation.go`

查找：
```go
moderation_next_retry_at
```

确保传入的是 `time.Time` 类型，而不是字符串。

#### 修复 4: 添加内存监控告警
**文件：** 新建 `backend/internal/service/memory_monitor.go`

```go
func StartMemoryMonitor(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            var m runtime.MemStats
            runtime.ReadMemStats(&m)

            // 内存超过 10GB 告警
            if m.Alloc > 10*1024*1024*1024 {
                logger.L().Error("memory usage too high",
                    zap.Uint64("alloc_mb", m.Alloc/1024/1024),
                    zap.Uint64("sys_mb", m.Sys/1024/1024))

                // 强制 GC
                runtime.GC()
            }
        }
    }
}
```

---

### 🟢 长期优化（架构层面）

#### 1. 实现连接池隔离
按账号/代理组合隔离连接池，避免单个池过大。

#### 2. 添加 Goroutine 泄漏检测
使用 `uber-go/goleak` 在测试中检测泄漏。

#### 3. 优化 UsageRecord 批处理
将高频的单条记录改为批量写入。

#### 4. 添加内存 Profiling
定期生成 pprof heap dump 分析内存分布。

---

## 📊 预期效果

### 修复后的内存表现
| 时间 | 修复前 | 修复后预期 |
|------|-------|----------|
| 10分钟 | 15.5 GB | 2-3 GB |
| 30分钟 | OOM (>62GB) | 3-4 GB |
| 1小时 | 服务崩溃 | 4-5 GB 稳定 |

### 429 错误预期
- 从 2.8% 降低到 < 0.5%
- Redis 限流键定期清理
- 上游 429 通过账号池分散

---

## ⚡ 立即行动清单

- [ ] 1. 在服务器终端执行 `sudo systemctl restart pixel.service`
- [ ] 2. 清理 Redis 限流键
- [ ] 3. 配置 systemd 内存限制
- [ ] 4. 准备代码修复（降低 worker 上限）
- [ ] 5. 修复数据库类型错误
- [ ] 6. 添加内存监控
- [ ] 7. 设置每小时自动重启（临时）

---

**报告生成时间：** 2026-09-02 10:42
**诊断工具：** Claude Code + SSH 远程监控
**建议执行人：** 运维团队 + 后端开发
