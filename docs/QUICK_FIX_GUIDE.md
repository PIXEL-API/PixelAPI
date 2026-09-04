# 新服务器故障快速修复指引

## 🚨 当前问题
1. **全站 429 错误** - 所有请求返回 Too Many Requests
2. **内存异常增高** - 应用进程内存持续增长

## 📋 立即执行（5 分钟内解决 429）

### 1. 登录服务器
```bash
ssh -p 22 s766@207.32.218.139
# 密码: Qpq1Bv%2LhpfVPAr
```

### 2. 检查 Redis 状态
```bash
# 测试连接
redis-cli ping
# 应该返回: PONG

# 如果返回错误或无响应，启动 Redis
sudo systemctl start redis
```

### 3. 清空限流缓存
```bash
# 清空所有限流键（安全操作，只清缓存不影响数据）
redis-cli --scan --pattern "rate_limit:*" | xargs redis-cli DEL
```

### 4. 重启应用
```bash
sudo systemctl restart pixel.service
```

### 5. 验证修复
```bash
# 查看应用状态
systemctl status pixel.service

# 查看日志（确认没有 redis error）
journalctl -u pixel.service -n 50
```

✅ **如果现在可以正常访问，429 问题已解决。继续下一步排查内存问题。**

---

## 🔍 内存问题诊断（10 分钟）

### 1. 检查当前内存占用
```bash
# 查看应用进程内存（关注 RSS 列，单位 KB）
ps aux | grep -E "sub2api|pixel" | grep -v grep

# 查看系统内存
free -h
```

### 2. 检查 Redis 配置
```bash
# 查看应用的 Redis 配置
cat /opt/sub2api/current/config.yaml | grep -A 15 "redis:"
```

**检查点**：
- `pool_size` 应该在 128-512 之间（如果 > 1000 则异常）
- `min_idle_conns` 应该在 10-50 之间（如果 > 100 则异常）

### 3. 如果配置异常，修改配置
```bash
# 备份配置
sudo cp /opt/sub2api/current/config.yaml /opt/sub2api/current/config.yaml.bak

# 编辑配置
sudo nano /opt/sub2api/current/config.yaml
```

找到 `redis:` 配置段，修改为：
```yaml
redis:
  host: "127.0.0.1"
  port: 6379
  password: ""
  db: 0
  pool_size: 128              # 改为 128
  min_idle_conns: 10          # 改为 10
  dial_timeout_seconds: 5
  read_timeout_seconds: 3
  write_timeout_seconds: 3
  enable_tls: false
```

保存后重启：
```bash
sudo systemctl restart pixel.service
```

### 4. 监控内存趋势（观察 10 分钟）
```bash
# 每 30 秒记录一次内存
watch -n 30 'ps aux | grep sub2api | grep -v grep | awk "{print \$6/1024\" MB\"}"'

# 按 Ctrl+C 退出
```

**正常情况**：内存应该稳定在某个值（如 500-800 MB），不会持续线性增长。

**异常情况**：内存每分钟增长 > 10 MB，说明存在泄漏。

---

## 📊 收集诊断信息（如果问题未解决）

执行以下命令，将输出发给我：

```bash
# 一键收集所有诊断信息
echo "=== Redis 状态 ===" && \
redis-cli ping && \
redis-cli info memory | grep -E "used_memory_human|maxmemory" && \
redis-cli --scan --pattern "rate_limit:*" | wc -l && \
echo -e "\n=== 应用配置 ===" && \
cat /opt/sub2api/current/config.yaml | grep -A 15 "redis:" && \
echo -e "\n=== 应用内存 ===" && \
ps aux | grep sub2api | grep -v grep && \
echo -e "\n=== 最近日志 ===" && \
journalctl -u pixel.service -n 50 --no-pager
```

---

## 🛠️ 可用工具

我已经准备了以下工具文件：

### 诊断脚本
- `test/redis_diagnostics.sh` - 自动诊断 Redis 和应用状态
- `test/manual_check_commands.md` - 手动诊断命令清单

### 配置参考
- `docs/redis-config-recommendations.yaml` - Redis 配置推荐值
- `docs/quick-fix-redis-failopen.patch` - 紧急代码补丁（降低安全性）

### 分析文档
- `docs/server-troubleshooting-summary.md` - 完整故障总结
- `docs/troubleshooting-429-memory-analysis.md` - 详细根因分析

---

## ⚠️ 注意事项

1. **清空限流键是安全的** - 只清理缓存，不影响用户数据
2. **重启应用会中断服务** - 约 3-5 秒，建议在低峰期执行
3. **修改配置前务必备份** - 使用 `cp config.yaml config.yaml.bak`
4. **持续监控至少 1 小时** - 确认问题不再复现

---

## 🎯 预期结果

修复后应该达到：
- ✅ 所有接口正常响应（不再 429）
- ✅ 应用日志无 `redis error`
- ✅ 内存稳定不再增长
- ✅ Redis 连接正常（`redis-cli ping` 返回 PONG）

---

## 📞 后续支持

如果以上步骤未能解决问题，请提供：
1. 上述"收集诊断信息"命令的完整输出
2. 应用启动后内存增长的速度（MB/分钟）
3. 是否有其他异常日志

我会根据诊断信息提供更深入的分析和修复方案。
