# 新服务器故障诊断总结

## 问题描述
服务器信息：
- IP: 207.32.218.139
- 端口: 22
- 用户: s766

故障现象：
1. **全站 429 错误**：所有请求返回 `Too Many Requests`
2. **内存异常增高**：应用进程内存持续增长

## 根本原因分析

### 问题 1：全站 429 错误

**根因**：认证路由配置了 `RateLimitFailClose` 模式，当 Redis 连接失败时，限流中间件会直接返回 429，导致全站不可用。

**触发条件**（五选一即可触发）：
1. Redis 服务未启动
2. Redis 连接配置错误（IP/端口/密码不匹配）
3. Redis 连接超时（网络问题或连接池耗尽）
4. Redis 内存满（达到 maxmemory 上限）
5. Redis 性能问题（慢查询、阻塞命令）

**影响范围**：
- `/api/v1/auth/register`（注册）
- `/api/v1/auth/login`（登录）
- `/api/v1/auth/refresh`（Token 刷新）
- 所有 OAuth 回调接口

**代码位置**：
- `backend/internal/middleware/rate_limiter.go:100-108`
- `backend/internal/server/routes/auth.go:31-180`

### 问题 2：内存异常增高

**可能原因**（需要实际诊断确认）：

#### 原因 A：Redis 连接池配置不当
- 症状：`pool_size` 或 `min_idle_conns` 配置过大
- 影响：每个空闲连接占用约 4-8KB 内存，1000 个空闲连接 = 4-8MB
- 诊断：检查 `config.yaml` 中的 `redis.pool_size` 和 `redis.min_idle_conns`
- 修复：降低到推荐值（pool_size: 128, min_idle_conns: 10）

#### 原因 B：上游 HTTP 连接池泄漏
- 症状：按账号+代理隔离，账号数量多时每个组合都创建独立连接池
- 影响：1000 个账号 × 10 个连接/账号 = 10000 个连接
- 诊断：检查 `gateway.connection_pool_isolation` 配置
- 修复：考虑改为 `proxy` 模式（按代理隔离）

#### 原因 C：限流键未正确过期
- 症状：Redis 中积累大量 `rate_limit:*` 键
- 影响：间接导致 Redis 内存增长，影响应用性能
- 诊断：`redis-cli --scan --pattern "rate_limit:*" | wc -l`
- 修复：清空限流键 + 检查 TTL 修复逻辑

#### 原因 D：goroutine 泄漏
- 症状：Redis 慢查询导致请求处理 goroutine 堆积
- 影响：每个 goroutine 占用约 2KB 栈空间
- 诊断：使用 pprof 分析 goroutine 数量和堆栈
- 修复：优化 Redis 超时配置

## 快速诊断步骤

### 步骤 1：登录服务器
```bash
ssh -p 22 s766@207.32.218.139
# 密码: Qpq1Bv%2LhpfVPAr
```

### 步骤 2：运行自动诊断脚本
我已经为你准备了两个诊断脚本：

```bash
# 方案 A：复制脚本到服务器后执行（推荐）
# 1. 将 test/redis_diagnostics.sh 上传到服务器
# 2. 在服务器上执行：
chmod +x redis_diagnostics.sh
./redis_diagnostics.sh

# 方案 B：手动执行命令（如果脚本无法运行）
# 参考 test/manual_check_commands.md 中的命令清单
```

### 步骤 3：根据诊断结果执行修复

#### 修复 A：Redis 服务未启动
```bash
systemctl status redis
systemctl start redis
systemctl restart pixel.service
```

#### 修复 B：清空限流键（临时方案）
```bash
redis-cli --scan --pattern "rate_limit:*" | xargs redis-cli DEL
systemctl restart pixel.service
```

#### 修复 C：修复 Redis 连接配置
编辑 `/opt/sub2api/current/config.yaml`，参考 `docs/redis-config-recommendations.yaml` 修改配置。

推荐配置：
```yaml
redis:
  host: "127.0.0.1"
  port: 6379
  password: ""
  db: 0
  pool_size: 128              # 不要超过 1000
  min_idle_conns: 10          # 不要超过 50
  dial_timeout_seconds: 5
  read_timeout_seconds: 3
  write_timeout_seconds: 3
  enable_tls: false
```

修改后重启：
```bash
systemctl restart pixel.service
```

#### 修复 D：代码补丁（降低安全风险，改为 fail-open）
⚠️ **仅用于紧急情况，会降低安全性**

应用补丁：
```bash
cd /path/to/project
git apply docs/quick-fix-redis-failopen.patch
cd backend && go build -o sub2api cmd/api/main.go
# 然后参考 pixeldeploy skill 重新部署
```

## 需要你提供的诊断信息

请登录服务器后执行以下命令，将输出发给我：

```bash
# 1. Redis 状态
redis-cli ping
redis-cli info memory
redis-cli info clients
redis-cli --scan --pattern "rate_limit:*" | wc -l

# 2. 应用配置（脱敏后）
cat /opt/sub2api/current/config.yaml | grep -A 15 "redis:"

# 3. 应用日志
journalctl -u pixel.service -n 200 --no-pager

# 4. 内存占用
ps aux | grep sub2api | grep -v grep
free -h

# 5. 系统信息
cat /etc/os-release
redis-cli --version
```

## 预期结果

完成修复后：
- ✅ `redis-cli ping` 返回 `PONG`
- ✅ 应用日志中无 `redis error`
- ✅ 所有接口返回 200（不再返回 429）
- ✅ 应用内存稳定（不再持续增长）
- ✅ Redis 限流键数量 < 1000

## 文档索引

我已创建以下文档供你参考：

1. **完整分析**：[docs/troubleshooting-429-memory-analysis.md](docs/troubleshooting-429-memory-analysis.md)
   - 详细的根因分析、代码追踪、修复方案

2. **手动诊断命令**：[test/manual_check_commands.md](test/manual_check_commands.md)
   - 逐步诊断命令清单（如果自动脚本无法运行）

3. **自动诊断脚本**：[test/redis_diagnostics.sh](test/redis_diagnostics.sh)
   - 一键诊断 Redis 和应用状态

4. **配置推荐**：[docs/redis-config-recommendations.yaml](docs/redis-config-recommendations.yaml)
   - 不同规模部署的 Redis 配置模板
   - 常见问题排查指南

5. **代码补丁**：[docs/quick-fix-redis-failopen.patch](docs/quick-fix-redis-failopen.patch)
   - 紧急情况下的 fail-open 补丁（降低安全性）

## 下一步行动

**立即执行**（解决 429 错误）：
1. 登录服务器
2. 执行 `systemctl status redis` 检查 Redis 状态
3. 如果 Redis 未运行，执行 `systemctl start redis`
4. 清空限流键：`redis-cli --scan --pattern "rate_limit:*" | xargs redis-cli DEL`
5. 重启应用：`systemctl restart pixel.service`
6. 测试接口是否恢复

**短期优化**（解决内存问题）：
1. 检查 `config.yaml` 中的 Redis 连接池配置
2. 如果 `pool_size` > 1000 或 `min_idle_conns` > 50，按推荐值修改
3. 重启应用观察内存趋势（每 10 分钟记录一次）
4. 如果内存仍然增长，使用 pprof 分析堆内存和 goroutine

**长期改进**（提升可靠性）：
1. 添加 Redis 健康检查（启动时检查连接）
2. 添加监控指标（Redis 连接池、限流键数量、内存趋势）
3. 考虑将非关键接口改为 fail-open 模式
4. 配置 Redis 内存淘汰策略（allkeys-lru）

## 注意事项

1. **数据安全**：诊断过程中不会修改数据库，只会清理 Redis 缓存
2. **服务中断**：重启应用会导致短暂服务中断（约 3-5 秒）
3. **配置备份**：修改配置前建议备份：`cp config.yaml config.yaml.bak`
4. **监控观察**：修复后需要持续观察至少 1 小时，确认问题不再复现

## 联系方式

如果遇到以下情况，请提供诊断信息：
- Redis 连接正常但仍然返回 429
- 修复后内存仍然持续增长
- 应用日志中出现新的错误
- 需要更详细的性能分析

请将诊断信息（上述 5 个命令的输出）发给我，我会进一步分析。
