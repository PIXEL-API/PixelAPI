# 全站 429 错误与内存泄漏根因分析报告

**日期**: 2026-09-02
**服务器**: 207.32.218.139 (新部署环境)
**症状**: 全站 429 错误 + 内存从 3GB 暴涨至 50GB

---

## 🎯 问题定位过程

### 1. 初步假设（已排除）

❌ **Redis 连接失败** - 经验证 Redis 服务正常运行
❌ **限流配置过严** - 代码中未发现异常的限流配置
❌ **流式处理泄漏** - 代码使用 `bufio.Scanner` 逐行读取，本身无问题
❌ **goroutine 泄漏** - 流处理有完善的清理机制

### 2. 关键线索

通过监控数据发现：
```
请求速率: 2,500+ req/min (每秒 41.7 个)
平均延迟: 10-30 秒
进程内存: 5分钟内从 3MB → 6.3GB
```

### 3. 根因定位

**代码位置**: `backend/internal/repository/http_upstream.go:39-51`

```go
const (
    defaultMaxConnsPerHost = 240      // ⚠️ 瓶颈点
    defaultMaxIdleConnsPerHost = 120
    defaultMaxIdleConns = 240
)
```

**数学计算**:
```
并发需求 = 请求速率 × 平均延迟
         = (2500 / 60) × 10s
         = 41.7 × 10
         = 417 个并发连接

可用连接 = 240
排队请求 = 417 - 240 = 177 个
```

### 4. 内存泄漏链路

```
高并发请求 (417个)
    ↓
HTTP连接池上限 (240个)
    ↓
177个请求被阻塞排队
    ↓
每个请求持有完整请求体在内存 (50KB-3MB)
    ↓
持续排队 × 大量请求体
    ↓
内存累积: 177 × 500KB × 时间 = 6GB+
```

---

## 🔧 修复方案

### 代码修改

**文件**: `backend/internal/repository/http_upstream.go`

```diff
const (
-   defaultMaxConnsPerHost = 240
+   defaultMaxConnsPerHost = 1000      // 提升至 2.4x 并发需求

-   defaultMaxIdleConnsPerHost = 120
+   defaultMaxIdleConnsPerHost = 200   // 配比 1:5

-   defaultMaxIdleConns = 240
+   defaultMaxIdleConns = 400          // 匹配新规模
)
```

### 修复原理

1. **消除排队瓶颈**: 1000 > 417，所有请求立即获得连接
2. **请求体即时释放**: 发送后立即释放内存，不再持有
3. **流式响应增量读取**: 只保留当前块的数据
4. **内存占用正常化**: 回归至 < 500MB

### 容量规划

```
设计并发 = 1000 连接
当前峰值 = 417 连接
安全冗余 = 1000 / 417 = 2.4x
```

即使请求速率翻倍至 5000 req/min，仍有 1.2x 冗余。

---

## 📊 修复效果预期

| 指标 | 修复前 | 修复后 | 改善 |
|------|--------|--------|------|
| **MaxConnsPerHost** | 240 | 1000 | +316% |
| **内存占用 (5min)** | 6-50 GB | < 500 MB | -92% |
| **429 错误率** | 高 | 接近零 | -99%+ |
| **请求排队** | 177 个 | 0 个 | -100% |
| **P99 延迟** | > 30s | < 15s | -50% |

---

## 🔍 为什么之前没发现？

### 老服务器 vs 新服务器

| 对比项 | 老服务器 | 新服务器 (207.32.218.139) |
|--------|----------|---------------------------|
| 流量规模 | 低 | **高 2-3倍** |
| 并发需求 | < 240 | **417+** |
| 触发条件 | 未达到 | **已触发** |
| 内存表现 | 正常 | **泄漏** |

新服务器流量更高，首次触发了连接池上限的瓶颈。

---

## ⚠️ 其他潜在问题（已验证正常）

### 1. Redis 配置
```bash
✅ Redis 服务: 运行中
✅ 连接测试: PONG
✅ 内存使用: < 100MB
```

### 2. 限流中间件
```go
// backend/internal/server/middleware/rate_limiter.go
✅ 限流策略: 基于 IP/用户，未全局阻断
✅ Fail-open 模式: Redis 故障时放行，不返回 429
```

### 3. 流式处理
```go
// backend/internal/service/openai_gateway_service.go:6369-6946
✅ 使用 bufio.Scanner: 逐行读取，无缓冲累积
✅ 清理机制: defer 确保 Body 关闭
✅ 超时控制: 5 秒空闲超时
```

---

## 📝 部署清单

### 前置条件
- [x] 编译 linux/amd64 二进制
- [x] 压缩文件 (135MB → 57MB)
- [x] 创建部署脚本
- [x] 准备回滚方案

### 部署步骤
1. 上传 `sub2api-emergency-fix.gz` 到 `/tmp/`
2. 执行部署脚本 `deploy_emergency_fix.sh`
3. 验证服务状态
4. 监控内存 5-10 分钟

### 验证指标
```bash
# 1. 服务状态
systemctl status pixel.service

# 2. 内存占用
ps aux | grep sub2api | awk '{print $6/1024 " MB"}'

# 3. 429 错误
journalctl -u pixel.service --since "5 min ago" | grep 429 | wc -l

# 4. 连接数
ss -s
```

---

## 🎓 经验总结

### 关键教训

1. **连接池sizing要基于实际并发**: 不是固定值240
2. **高延迟场景需更大池**: 延迟 × 速率 = 并发需求
3. **内存监控要持续观察**: 5分钟窗口才能发现累积
4. **新环境要做容量测试**: 不能假设旧配置够用

### 最佳实践

```go
// 连接池配置公式
MaxConnsPerHost = (请求速率/秒) × P99延迟(秒) × 冗余系数(1.5-3.0)

// 本案例
= (2500/60) × 10 × 2.4
= 1000
```

---

## 📚 相关文档

- 部署指南: `docs/emergency-fix-deployment-guide.md`
- 部署脚本: `test/deploy_emergency_fix.sh`
- 代码修改: `backend/internal/repository/http_upstream.go`

---

## 🔗 追踪信息

**Git Commit**: (待部署后记录)
**Release**: emergency-20260902-235x
**部署者**: (待填写)
**部署时间**: (待填写)
**验证结果**: (待填写)
