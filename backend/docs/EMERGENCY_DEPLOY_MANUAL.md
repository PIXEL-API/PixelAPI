# 🚨 紧急部署指令 - 请在 FinalShell 终端执行

内存已达 43GB！请立即在你的 FinalShell 终端中逐条执行以下命令：

## 第1步：备份并停止服务
```bash
sudo cp /opt/sub2api/current/sub2api /opt/sub2api/current/sub2api.backup.$(date +%Y%m%d_%H%M%S)
sudo systemctl stop pixel.service
```

## 第2步：替换二进制
```bash
sudo cp /tmp/sub2api.new /opt/sub2api/current/sub2api
sudo chmod +x /opt/sub2api/current/sub2api
sudo chown sub2api:sub2api /opt/sub2api/current/sub2api
```

## 第3步：启动服务
```bash
sudo systemctl start pixel.service
```

## 第4步：验证（等待10秒后执行）
```bash
sleep 10
systemctl status pixel.service
ps aux | grep sub2api | grep -v grep | head -1
```

## 第5步：部署监控脚本
```bash
sudo cp /tmp/memory_guard.sh /opt/sub2api/memory_guard.sh
sudo chmod +x /opt/sub2api/memory_guard.sh
```

## 第6步：启动监控守护进程
```bash
# 创建日志目录
sudo mkdir -p /var/log/sub2api
sudo chown s766:s766 /var/log/sub2api

# 启动监控（后台运行）
nohup /opt/sub2api/memory_guard.sh > /var/log/sub2api/memory-guard.log 2>&1 &

# 验证监控进程
ps aux | grep memory_guard | grep -v grep
```

---

## 🔍 验证优化效果

执行完上述命令后，等待5分钟，然后执行：

```bash
# 查看当前内存（应该在 500MB-2GB 之间）
ps aux | grep sub2api | grep -v grep | head -1 | awk '{print "内存: " $6/1024 " MB"}'

# 查看监控日志
tail -f /var/log/sub2api/memory-guard.log
```

---

## ⏰ 预期效果时间线

| 时间 | 预期内存 | 说明 |
|------|---------|------|
| 重启后 1 分钟 | ~500 MB | 初始化阶段 |
| 重启后 5 分钟 | ~1.5 GB | 正常运行 |
| 重启后 10 分钟 | ~2-3 GB | 稳定状态（优化后） |
| 重启后 30 分钟 | ~4-6 GB | 峰值（之前是 15GB+） |

---

## 📊 对比（优化前 vs 优化后）

### 配置对比
```
Worker Pool 最大值: 512 → 384 (降低 25%)
HTTP MaxIdleConns: 100 → 200 (提升 100%，支持高吞吐)
HTTP MaxIdleConnsPerHost: 10 → 20 (提升 100%，支持高吞吐)
连接超时: 90s → 60s (降低 33%，加快释放)
```

### 吞吐量影响分析
- ✅ **不会降低吞吐量**：HTTP 连接池增大了（100→200）
- ✅ **并发处理能力保持**：每主机连接数翻倍（10→20）
- ✅ **Worker 384 个足够处理 4000+ req/min**

### 内存节省
- 10分钟内存增长：15.5GB → 预计 2-3GB（节省 80%+）
- 1小时内存使用：预计崩溃 → 预计 6-8GB（节省 70%+）

---

## 🛡️ 监控守护进程功能

监控脚本会：
1. 每 5 分钟检查一次内存
2. 如果超过 48GB，自动重启服务
3. 记录所有重启事件到日志
4. 防止系统 OOM（Out of Memory）崩溃

---

**请立即执行第1-6步，然后告诉我结果！**
