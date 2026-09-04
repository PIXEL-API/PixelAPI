# 内存泄漏修复 - 手动部署命令（复制粘贴执行）

## 准备工作完成
✅ 二进制已编译：`test/sub2api-fix.gz` (57MB)
✅ 代码已修复：异步任务不再持有完整请求链
✅ 预期效果：内存稳定在 2-3GB

---

## 部署命令（请按顺序复制执行）

### 第 1 步：上传文件
在 **Git Bash** 或 **PowerShell** 中，切换到项目目录：
```bash
cd C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\test
```

然后执行上传（会提示输入密码）：
```bash
scp sub2api-fix.gz s766@207.32.218.139:/tmp/
```
**密码**: `Qpq1Bv%2LhpfVPAr`

---

### 第 2 步：SSH 连接服务器
```bash
ssh s766@207.32.218.139
```
**密码**: `Qpq1Bv%2LhpfVPAr`

---

### 第 3 步：部署（在服务器上执行以下所有命令）

#### 3.1 解压文件
```bash
cd /tmp
gunzip -f sub2api-fix.gz
ls -lh sub2api-fix
```
应该显示 ~137MB

#### 3.2 停止服务
```bash
echo 'Qpq1Bv%2LhpfVPAr' | sudo -S systemctl stop pixel.service
```

#### 3.3 备份当前版本（可选但推荐）
```bash
sudo cp /opt/sub2api/current/sub2api /opt/sub2api/current/sub2api.backup
```

#### 3.4 替换二进制
```bash
sudo cp /tmp/sub2api-fix /opt/sub2api/current/sub2api
sudo chmod +x /opt/sub2api/current/sub2api
```

#### 3.5 启动服务
```bash
sudo systemctl start pixel.service
```

#### 3.6 等待启动
```bash
sleep 5
```

---

### 第 4 步：验证部署

#### 4.1 检查服务状态
```bash
systemctl status pixel.service
```
应该显示 `active (running)`

#### 4.2 检查内存使用
```bash
ps aux | grep sub2api | grep -v grep
```
应该显示内存 ~2-3GB（VSZ 列 / 1024 / 1024）

#### 4.3 持续监控（推荐运行 30 分钟）
```bash
watch -n 30 'date; echo ""; ps aux | grep sub2api | grep -v grep; echo ""; free -h | grep Mem'
```
按 `Ctrl+C` 退出监控

---

## 验证清单

运行 30 分钟后，确认：
- [ ] 服务状态：`active (running)`
- [ ] 内存使用：稳定在 2-3GB，不持续增长
- [ ] API 响应：正常
- [ ] 系统内存：健康，无 OOM 告警

---

## 如果需要回滚

```bash
sudo systemctl stop pixel.service
sudo cp /opt/sub2api/current/sub2api.backup /opt/sub2api/current/sub2api
sudo systemctl start pixel.service
```

---

## 修复内容总结

### 修改的文件
`backend/internal/handler/openai_gateway_handler.go`

### 修改内容
1. **添加** `usageRecordContext` 函数（217-232 行）
   - 从完整 Context 中只提取必要字段（ClientRequestID, RequestID）

2. **添加** `wrapUsageRecordTaskContext` 函数（234-241 行）
   - 包装异步任务，确保只传递轻量级 Context

3. **修改** `submitUsageRecordTask` 函数（3093 行）
   - 提交前先用 `wrapUsageRecordTaskContext` 包装任务

### 修复原理
**修复前**：
- 异步任务持有完整请求链：闭包 → forwardCtx → gin.Context → Request → Body (3MB)
- 队列堆积时，GC 无法回收
- 16384 个任务 × 3MB = 49GB

**修复后**：
- 异步任务只持有 2 个字符串（~200 字节）
- 请求结束后，gin.Context 和 Body 立即可被 GC 回收
- 16384 个任务 × 200B = 3.2MB

---

**文档创建时间**: 2026-09-03 01:08
**二进制位置**: `C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\test\sub2api-fix.gz`
