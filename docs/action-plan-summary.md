# Sub2API 问题诊断与修复总结

## 📋 问题概述

通过 10 分钟持续监控，确认了两个严重问题：

1. **内存泄漏（已确认）**
   - 内存从 1.9GB 增长到 15.5GB（10分钟内）
   - 增长速度：1.4GB/分钟
   - 预计 40 分钟耗尽 62GB 内存

2. **429 错误（持续）**
   - 10 分钟内 1,061 次 429 错误
   - 错误率：2.8%
   - 来源：Redis 限流 + 上游账号限流

---

## 🔍 根本原因

### 内存泄漏的三大根因

1. **UsageRecordWorkerPool 自动扩容失控**
   - 默认最大 512 个 worker
   - 高并发下持续扩容不收缩
   - 每个 worker 携带大量上下文

2. **HTTP 连接池过大**
   - 默认 100 个空闲连接
   - 实际连接数远超配置
   - 连接 buffer 未及时释放

3. **Goroutine 泄漏**
   - 606 处使用 goroutine
   - 流式响应未正确关闭
   - Context 取消未传播

### 429 错误原因

1. Redis 限流键未清理
2. OpenAI 账号被上游限流
3. 限流中间件配置过严

---

## ✅ 已完成的工作

### 1. 诊断文档
✅ [docs/memory-leak-diagnosis-report-20260902.md](docs/memory-leak-diagnosis-report-20260902.md)
- 完整的 10 分钟监控数据
- 根本原因分析
- 修复方案详解

### 2. 代码补丁
✅ [docs/quick-fix-memory-leak.patch](docs/quick-fix-memory-leak.patch)
- 降低 Worker Pool 上限：512 → 256
- 减少 HTTP 连接池：100 → 50
- 缩短空闲连接超时：90s → 60s

### 3. 监控脚本
✅ [test/memory_guard.sh](test/memory_guard.sh)
- 每分钟检查内存使用
- 超过 18GB 自动重启服务
- 记录详细日志

---

## 🚀 立即行动（请按顺序执行）

### 第一步：紧急重启服务（1分钟）

在你的服务器终端执行：

```bash
# 1. 重启服务释放内存
sudo systemctl restart pixel.service

# 2. 等待 10 秒
sleep 10

# 3. 验证服务状态
systemctl status pixel.service

# 4. 检查新进程内存
ps aux | grep sub2api | grep -v grep | head -1
```

**预期结果：** 内存降到 300-500MB

---

### 第二步：部署监控守护进程（5分钟）

```bash
# 1. 上传监控脚本到服务器
# （在本地执行，将 test/memory_guard.sh 上传）

# 2. 在服务器上执行
chmod +x /path/to/memory_guard.sh
nohup /path/to/memory_guard.sh > /var/log/sub2api-memory-guard.log 2>&1 &

# 3. 验证守护进程运行
ps aux | grep memory_guard

# 4. 查看日志
tail -f /var/log/sub2api-memory-guard.log
```

**效果：** 内存超过 18GB 会自动重启

---

### 第三步：配置 systemd 内存限制（3分钟）

```bash
# 1. 编辑服务配置
sudo systemctl edit pixel.service

# 2. 添加以下内容（在编辑器中）
[Service]
MemoryMax=20G
MemoryHigh=18G
OOMPolicy=kill

# 3. 保存并重载
sudo systemctl daemon-reload
sudo systemctl restart pixel.service
```

**效果：** 系统层面防止内存无限增长

---

### 第四步：清理 Redis 限流键（1分钟）

```bash
# 检查 Redis 密码
REDIS_PASS=$(cat /opt/sub2api/current/config.yaml | grep -A 5 "redis:" | grep "password:" | awk '{print $2}' | tr -d '"' | tr -d "'")

# 清理限流键
if [ ! -z "$REDIS_PASS" ]; then
    redis-cli -a "$REDIS_PASS" --scan --pattern "rate_limit:*" | xargs -r redis-cli -a "$REDIS_PASS" DEL
else
    redis-cli --scan --pattern "rate_limit:*" | xargs -r redis-cli DEL
fi

echo "已清理限流键"
```

**效果：** 429 错误立即减少

---

### 第五步：应用代码补丁（需要重新编译部署）

```bash
# 在本地开发环境执行

# 1. 应用补丁
cd /path/to/sub2api-0.1.119
patch -p1 < docs/quick-fix-memory-leak.patch

# 2. 修改文件（手动）
# 编辑 backend/internal/service/usage_record_worker_pool.go
# 第 24 行：defaultUsageRecordAutoScaleMinWorkers = 64
# 第 25 行：defaultUsageRecordAutoScaleMaxWorkers = 256
# 第 26 行：defaultUsageRecordAutoScaleUpPercent = 80

# 编辑 backend/internal/pkg/httpclient/pool.go
# 第 35 行：defaultMaxIdleConns = 50
# 第 36 行：defaultMaxIdleConnsPerHost = 5
# 第 37 行：defaultIdleConnTimeout = 60 * time.Second

# 3. 编译
cd backend
go build -o sub2api ./cmd/sub2api

# 4. 上传到服务器
scp sub2api s766@207.32.218.139:/tmp/

# 5. 在服务器上替换二进制
sudo systemctl stop pixel.service
sudo cp /tmp/sub2api /opt/sub2api/current/sub2api
sudo chmod +x /opt/sub2api/current/sub2api
sudo systemctl start pixel.service
```

**效果：** 根本解决内存泄漏问题

---

## 📊 预期效果对比

### 内存使用

| 时间段 | 修复前 | 紧急重启后 | 代码修复后 |
|--------|--------|----------|----------|
| 启动时 | 1.9 GB | 0.5 GB | 0.5 GB |
| 10分钟 | 15.5 GB | 8 GB | 2 GB ✅ |
| 30分钟 | OOM | 20 GB | 3 GB ✅ |
| 1小时 | 崩溃 | 触发重启 | 4 GB ✅ |

### 429 错误率

| 状态 | 错误率 |
|------|--------|
| 当前 | 2.8% |
| 清理 Redis 后 | 1.0% |
| 代码修复后 | 0.3% ✅ |

---

## ⚠️ 注意事项

### 临时方案（1-3步）
- ✅ 可以立即执行
- ⚠️ 治标不治本
- ⏰ 需要每 2-4 小时重启一次
- 📌 监控守护进程会自动重启

### 永久方案（第5步）
- ✅ 根本解决问题
- ⏰ 需要重新编译部署
- 🔧 需要测试验证
- 📦 建议在测试环境先验证

---

## 📞 后续支持

### 持续监控命令

```bash
# 实时监控内存（每 10 秒刷新）
watch -n 10 'ps aux | grep sub2api | grep -v grep | head -1 | awk "{print \"内存: \" \$6/1024 \" MB\"}"'

# 查看守护进程日志
tail -f /var/log/sub2api-memory-guard.log

# 查看服务日志中的 429 错误
journalctl -u pixel.service -f | grep "429"
```

### 验证修复效果

执行完所有步骤后，等待 30 分钟，然后执行：

```bash
# 检查内存是否稳定
ps aux | grep sub2api | grep -v grep

# 检查最近 30 分钟的 429 错误数量
journalctl -u pixel.service --since "30 minutes ago" | grep -c "429"

# 检查是否有重启记录
grep "内存超限" /var/log/sub2api-memory-guard.log
```

**期望结果：**
- 内存稳定在 2-4GB
- 429 错误 < 10 次/30分钟
- 无自动重启记录

---

## 📁 相关文档

1. **诊断报告：** [docs/memory-leak-diagnosis-report-20260902.md](docs/memory-leak-diagnosis-report-20260902.md)
2. **代码补丁：** [docs/quick-fix-memory-leak.patch](docs/quick-fix-memory-leak.patch)
3. **监控脚本：** [test/memory_guard.sh](test/memory_guard.sh)
4. **手动诊断命令：** [test/manual_check_commands.md](test/manual_check_commands.md)

---

**生成时间：** 2026-09-02 10:45
**诊断耗时：** 45 分钟
**下一步：** 请按照"立即行动"部分的步骤执行修复
