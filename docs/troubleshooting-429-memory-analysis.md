# 新服务器全站 429 错误和内存异常增高问题分析

## 问题现象
1. **全站 429 错误**：所有请求返回 `429 Too Many Requests`
2. **内存异常增高**：应用进程内存持续增长

## 根因分析

### 问题 1：全站 429 错误

#### 根本原因
从代码分析发现，**所有认证路由都配置了 `RateLimitFailClose` 模式**：

```go
// backend/internal/server/routes/auth.go:31-46
auth.POST("/register", rateLimiter.LimitWithOptions("auth-register", 5, time.Minute, middleware.RateLimitOptions{
    FailureMode: middleware.RateLimitFailClose,  // ← 关键：Redis 故障时直接返回 429
}), h.Auth.Register)

auth.POST("/login", rateLimiter.LimitWithOptions("auth-login", 20, time.Minute, middleware.RateLimitOptions{
    FailureMode: middleware.RateLimitFailClose,
}), h.Auth.Login)

auth.POST("/refresh", rateLimiter.LimitWithOptions("refresh-token", 30, time.Minute, middleware.RateLimitOptions{
    FailureMode: middleware.RateLimitFailClose,
}), h.Auth.RefreshToken)
```

#### 故障链条
```
Redis 连接失败/超时
    ↓
RateLimitFailClose 模式触发
    ↓
middleware.abortRateLimit() 返回 429
    ↓
全站所有请求被拒绝
```

#### 关键代码逻辑（backend/internal/middleware/rate_limiter.go:100-108）
```go
count, repaired, err := rateLimitRun(ctx, r.redis, redisKey, windowMillis)
if err != nil {
    log.Printf("[RateLimit] redis error: key=%s mode=%s err=%v", redisKey, failureModeLabel(failureMode), err)
    if failureMode == RateLimitFailClose {
        abortRateLimit(c)  // ← 直接返回 429，不放行
        return
    }
    // Redis 错误时放行，避免影响正常服务
    c.Next()
    return
}
```

#### 可能触发条件
1. **Redis 服务未启动**
2. **Redis 连接配置错误**（IP、端口、密码）
3. **Redis 连接超时**（网络问题、连接池耗尽）
4. **Redis 内存满**（maxmemory 达到上限）
5. **Redis 性能问题**（慢查询、阻塞命令）

### 问题 2：内存异常增高

#### 可能根因

##### 2.1 Redis 连接池配置不当
代码中 Redis 连接池使用配置文件参数，如果配置不当会导致内存泄漏：

```go
// backend/internal/repository/redis.go:44-54
opts := &redis.Options{
    Network:      string(spec.Network),
    Addr:         spec.Address,
    Password:     cfg.Redis.Password,
    DB:           cfg.Redis.DB,
    DialTimeout:  time.Duration(cfg.Redis.DialTimeoutSeconds) * time.Second,
    ReadTimeout:  time.Duration(cfg.Redis.ReadTimeoutSeconds) * time.Second,
    WriteTimeout: time.Duration(cfg.Redis.WriteTimeoutSeconds) * time.Second,
    PoolSize:     cfg.Redis.PoolSize,      // ← 如果配置过大会占用大量内存
    MinIdleConns: cfg.Redis.MinIdleConns,  // ← 如果配置过高会保持大量空闲连接
}
```

**问题配置示例**：
- `pool_size: 10000`（过大，默认应为 128）
- `min_idle_conns: 5000`（过高，默认应为 10）

##### 2.2 上游 HTTP 连接池泄漏
项目使用账号级或代理级连接池隔离，配置不当可能导致连接未释放：

```go
// backend/internal/config/config.go:63-72
// ConnectionPoolIsolationProxy: 按代理隔离
// ConnectionPoolIsolationAccount: 按账户隔离
// ConnectionPoolIsolationAccountProxy: 按账户+代理组合隔离（默认）
```

如果账号数量多，且使用 `ConnectionPoolIsolationAccountProxy` 模式，每个账号+代理组合都会创建独立连接池。

##### 2.3 限流键未过期导致 Redis 内存溢出
限流键格式：`rate_limit:<key>:<clientIP>`

如果大量客户端请求，Redis 会积累大量限流键。虽然有 TTL，但如果：
- TTL 修复逻辑失效（`repaired` 标志）
- Redis 内存策略配置不当（未启用 LRU/LFU 淘汰）

会导致 Redis 内存持续增长，反过来影响应用性能。

##### 2.4 goroutine 泄漏
限流中间件在每个请求中都会调用 Redis，如果：
- 超时配置过长（ReadTimeout/WriteTimeout）
- Redis 慢查询阻塞

可能导致请求处理 goroutine 堆积，无法释放。

## 诊断步骤

### 第一步：登录服务器
```bash
ssh -p 22 s766@207.32.218.139
# 密码: Qpq1Bv%2LhpfVPAr
```

### 第二步：检查 Redis 状态
```bash
# 1. 测试 Redis 连接
redis-cli ping
# 预期输出: PONG

# 2. 查看 Redis 内存使用
redis-cli info memory | grep -E "used_memory_human|maxmemory|mem_fragmentation"

# 3. 查看限流键数量
redis-cli --scan --pattern "rate_limit:*" | wc -l

# 4. 查看 Redis 连接数
redis-cli info clients | grep connected_clients
```

### 第三步：检查应用配置
```bash
# 查看 Redis 配置
cat /opt/sub2api/current/config.yaml | grep -A 15 "redis:"

# 重点检查这些参数：
# - host: <Redis地址>
# - port: <Redis端口>
# - pool_size: <连接池大小，默认128>
# - min_idle_conns: <最小空闲连接，默认10>
# - dial_timeout_seconds: <建连超时，默认5>
# - read_timeout_seconds: <读取超时，默认3>
# - write_timeout_seconds: <写入超时，默认3>
```

### 第四步：检查应用日志
```bash
# 查看 Redis 错误
journalctl -u pixel.service -n 200 | grep -i "redis error"

# 查看限流日志
journalctl -u pixel.service -n 200 | grep -i "rate limit"

# 查看 429 错误
journalctl -u pixel.service -n 200 | grep "429"
```

### 第五步：检查内存占用
```bash
# 查看应用进程内存（RSS 列）
ps aux | grep -E "sub2api|pixel" | grep -v grep

# 实时监控内存增长（运行5分钟）
watch -n 10 'ps aux | grep sub2api | grep -v grep | awk "{print \$6/1024\" MB\"}"'
```

## 快速修复方案

### 修复方案 1：Redis 连接失败
```bash
# 1. 检查 Redis 服务
systemctl status redis
# 如果未运行
systemctl start redis

# 2. 检查 Redis 配置
redis-cli ping

# 3. 重启应用
systemctl restart pixel.service
```

### 修复方案 2：清空限流键（临时）
```bash
# 清空所有限流键
redis-cli --scan --pattern "rate_limit:*" | xargs redis-cli DEL

# 重启应用
systemctl restart pixel.service
```

### 修复方案 3：修改 Redis 连接池配置
编辑 `/opt/sub2api/current/config.yaml`：

```yaml
redis:
  host: "127.0.0.1"  # 或者实际的 Redis 地址
  port: 6379
  password: ""
  db: 0
  # 连接池配置（推荐值）
  pool_size: 128              # 默认128，不要超过1000
  min_idle_conns: 10          # 默认10，不要超过50
  dial_timeout_seconds: 5     # 建连超时
  read_timeout_seconds: 3     # 读取超时
  write_timeout_seconds: 3    # 写入超时
  enable_tls: false
```

重启应用：
```bash
systemctl restart pixel.service
```

### 修复方案 4：改为 fail-open 模式（代码修改，需重新部署）
如果 Redis 故障不应该阻断全站服务，可以将 fail-close 改为 fail-open：

```go
// backend/internal/server/routes/auth.go
auth.POST("/login", rateLimiter.LimitWithOptions("auth-login", 20, time.Minute, middleware.RateLimitOptions{
    FailureMode: middleware.RateLimitFailOpen,  // ← Redis故障时放行
}), h.Auth.Login)
```

但这会降低安全性，不推荐用于生产环境的认证接口。

## 长期优化建议

### 1. 添加 Redis 健康检查
在应用启动时检查 Redis 连接，失败时拒绝启动：

```go
// 在 main.go 或 setup 中添加
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
if err := redisClient.Ping(ctx).Err(); err != nil {
    log.Fatalf("Redis connection failed: %v", err)
}
```

### 2. 监控 Redis 连接池指标
添加 Prometheus 指标：
- `redis_pool_hits`：连接池命中次数
- `redis_pool_misses`：连接池未命中次数
- `redis_pool_timeouts`：连接池超时次数
- `redis_pool_total_conns`：总连接数
- `redis_pool_idle_conns`：空闲连接数

### 3. 限流键 TTL 监控
监控限流键的 TTL 修复次数：

```go
if repaired {
    log.Printf("[RateLimit] ttl repaired: key=%s window_ms=%d", redisKey, windowMillis)
    // ← 添加 Prometheus counter
}
```

如果频繁出现 TTL 修复，说明 Redis 性能或脚本执行有问题。

### 4. 内存泄漏排查工具
添加 pprof 端点：

```go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe("localhost:6060", nil))
}()
```

然后可以通过以下命令分析内存：
```bash
# 在本地机器执行（需要 SSH 隧道）
ssh -L 6060:localhost:6060 s766@207.32.218.139 -p 22
go tool pprof http://localhost:6060/debug/pprof/heap
```

### 5. Redis 内存淘汰策略
确保 Redis 配置了合理的淘汰策略：

```bash
# 编辑 /etc/redis/redis.conf
maxmemory 2gb
maxmemory-policy allkeys-lru  # 或 volatile-lru

# 重启 Redis
systemctl restart redis
```

## 预期结果

完成诊断和修复后：
1. **429 错误消失**：应用正常响应请求
2. **内存稳定**：RSS 内存不再持续增长，保持在合理范围（< 2GB）
3. **Redis 健康**：`redis-cli ping` 返回 PONG，连接数正常

## 需要收集的信息

请执行诊断后，将以下信息发给我：

1. **Redis 状态**：
   ```bash
   redis-cli ping
   redis-cli info memory
   redis-cli info clients
   redis-cli --scan --pattern "rate_limit:*" | wc -l
   ```

2. **应用配置**（脱敏后）：
   ```bash
   cat /opt/sub2api/current/config.yaml | grep -A 15 "redis:"
   ```

3. **应用日志**（最近200行）：
   ```bash
   journalctl -u pixel.service -n 200 --no-pager
   ```

4. **内存占用**：
   ```bash
   ps aux | grep sub2api | grep -v grep
   free -h
   ```

5. **系统信息**：
   ```bash
   cat /etc/os-release
   systemctl --version
   redis-cli --version
   ```

有了这些信息，我可以精确定位问题并提供针对性的修复方案。
