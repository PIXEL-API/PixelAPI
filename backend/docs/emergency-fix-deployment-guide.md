# 紧急修复部署指南

## 📋 修复说明

**问题根因：** HTTP 连接池上限过低（240）导致高并发场景下请求排队，每个等待的请求都持有完整请求体在内存中，造成内存从 3GB 暴涨至 50GB。

**修复方案：**
- `MaxConnsPerHost`: 240 → 1000
- `MaxIdleConnsPerHost`: 120 → 200
- `MaxIdleConns`: 240 → 400

**计算依据：** 2500 req/min ÷ 60s × 10s 平均延迟 ≈ 420 并发，留 2.4x 冗余

---

## 🚀 快速部署步骤

### 方法一：自动脚本（推荐）

在你的服务器终端中执行：

```bash
# 1. 手动上传文件（从本地 Windows）
# 在 Windows PowerShell 或你的 SSH 工具中上传：
# scp sub2api-emergency-fix.gz s766@207.32.218.139:/tmp/

# 2. 上传部署脚本
# scp test/deploy_emergency_fix.sh s766@207.32.218.139:/tmp/

# 3. 在服务器上执行
ssh s766@207.32.218.139
sudo bash /tmp/deploy_emergency_fix.sh
```

---

### 方法二：手动部署（详细步骤）

#### 步骤 1: 上传文件

在你的 **Windows 终端**（FinalShell）中，使用内置的上传功能：
- 文件位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\sub2api-emergency-fix.gz`
- 上传到服务器：`/tmp/sub2api-emergency-fix.gz`

#### 步骤 2: 在服务器上执行

```bash
# 2.1 解压
cd /tmp
gunzip -f sub2api-emergency-fix.gz
chmod +x sub2api-emergency-fix

# 2.2 创建 release 目录
RELEASE_DIR="/opt/sub2api/releases/emergency-$(date +%Y%m%d-%H%M%S)"
sudo mkdir -p "$RELEASE_DIR"

# 2.3 复制配置和数据
sudo cp /opt/sub2api/current/config.yaml "$RELEASE_DIR/"
sudo cp -r /opt/sub2api/current/data "$RELEASE_DIR/"
sudo cp /tmp/sub2api-emergency-fix "$RELEASE_DIR/sub2api"

# 2.4 切换软链
sudo ln -snf "$RELEASE_DIR" /opt/sub2api/current

# 2.5 重启服务
sudo systemctl restart pixel.service

# 2.6 验证
sleep 3
systemctl status pixel.service
ps aux | grep sub2api | grep -v grep
```

---

## ✅ 验证修复效果

执行以下命令持续监控内存：

```bash
# 监控进程内存（每5秒刷新）
watch -n 5 'ps aux | grep sub2api | grep -v grep | awk "{printf \"内存: %.1f MB\n\", \$6/1024}"'
```

**预期效果：**
- 初始内存：< 100 MB
- 5分钟后：< 500 MB（之前会涨到 6GB+）
- 429 错误消失

---

## 🔍 问题诊断命令

### 检查 429 错误
```bash
journalctl -u pixel.service --since "5 minutes ago" | grep "429" | wc -l
```

### 检查连接数
```bash
ss -s
netstat -an | grep ESTABLISHED | wc -l
```

### 检查 Redis
```bash
redis-cli ping
redis-cli info memory | grep used_memory_human
```

---

## 📊 修复前后对比

| 指标 | 修复前 | 修复后 |
|------|--------|--------|
| MaxConnsPerHost | 240 | **1000** |
| MaxIdleConnsPerHost | 120 | **200** |
| 内存占用（5分钟） | 6-50 GB | < 500 MB |
| 429 错误率 | 高 | 接近零 |
| 并发处理能力 | 240 req | **1000 req** |

---

## ⚠️ 回滚方案

如果出现问题，立即回滚：

```bash
# 查看上一个 release
ls -lt /opt/sub2api/releases/ | head -5

# 回滚（替换为实际的目录名）
sudo ln -snf /opt/sub2api/releases/20260901-115907 /opt/sub2api/current
sudo systemctl restart pixel.service
```

---

## 📝 技术细节

### 代码修改位置
- 文件：`backend/internal/repository/http_upstream.go`
- 行号：39-51

### 为什么会内存泄漏？

1. **请求速率**: 2500 req/min = 每秒 41.7 个请求
2. **平均延迟**: 10-30 秒（上游 API 慢）
3. **并发需求**: 41.7 × 10s = **417 个并发连接**
4. **原有上限**: 240 个连接
5. **排队效应**: 177 个请求被阻塞，每个持有 50KB-3MB 请求体
6. **内存累积**: 177 × 500KB × 持续排队 = **6GB+ 内存**

### 修复原理

提升连接池上限至 1000，消除排队瓶颈：
- 所有请求立即获得连接
- 请求体发送后立即释放
- 只保留流式响应的增量数据
- 内存占用回归正常

---

## 🆘 需要帮助？

如果遇到问题，收集以下信息：

```bash
# 完整诊断
systemctl status pixel.service
journalctl -u pixel.service -n 50
ps aux | grep sub2api
free -h
ss -s
```

然后将输出发送给开发团队。
