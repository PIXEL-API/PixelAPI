# 内存泄漏修复 - 快速部署指南

## 问题总结

**症状**：内存异常增长 ~946MB/分钟，最终导致 OOM
**根因**：异步计费任务持有完整请求 Context 链（包括 3MB body），队列堆积导致 GC 无法回收
**修复**：从上游移植 `usageRecordContext` 机制，异步任务只持有必要字段（2 个字符串）而非完整请求链

## 修复文件

✅ **已编译**：`test/sub2api-fix.gz`（57MB）
✅ **代码修改**：`backend/internal/handler/openai_gateway_handler.go`
  - 添加 `usageRecordContext` 函数（217-232 行）
  - 添加 `wrapUsageRecordTaskContext` 函数（234-241 行）
  - 修改 `submitUsageRecordTask` 函数（3093 行）
  - 添加 `ctxkey` 包导入

## 部署步骤

### 准备工作
```bash
# 本地项目路径
cd C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\test

# 服务器信息
# IP: 207.32.218.139
# 用户: s766
# 密码: Qpq1Bv%2LhpfVPAr
```

### 第 1 步：上传文件
在 Git Bash 或 PowerShell 中执行：
```bash
scp sub2api-fix.gz s766@207.32.218.139:/tmp/
# 输入密码：Qpq1Bv%2LhpfVPAr
```

### 第 2 步：SSH 连接
```bash
ssh s766@207.32.218.139
# 输入密码：Qpq1Bv%2LhpfVPAr
```

### 第 3 步：部署（服务器上执行）
```bash
# 解压
cd /tmp
gunzip -f sub2api-fix.gz
ls -lh sub2api-fix  # 应该显示 ~137MB

# 停止服务
echo 'Qpq1Bv%2LhpfVPAr' | sudo -S systemctl stop pixel.service

# 替换二进制
sudo cp sub2api-fix /opt/sub2api/current/sub2api
sudo chmod +x /opt/sub2api/current/sub2api

# 启动服务
sudo systemctl start pixel.service

# 等待 5 秒
sleep 5

# 验证
systemctl status pixel.service
ps aux | grep sub2api | grep -v grep
```

### 第 4 步：监控内存（服务器上执行）
```bash
# 持续监控（每 30 秒）
watch -n 30 'date; echo ""; ps aux | grep sub2api | grep -v grep; echo ""; free -h | grep Mem'

# 按 Ctrl+C 退出监控
```

## 预期结果

✅ **立即验证**（5 分钟内）
- 服务正常启动
- 初始内存 ~2-3GB
- API 正常响应

✅ **持续验证**（1 小时后）
- 内存稳定，不持续增长
- 即使有大量 403 拒绝请求，内存也不增长
- 系统整体内存健康

## 快速检查命令

```bash
# 查看服务状态
systemctl status pixel.service

# 查看当前内存
ps aux | grep sub2api | grep -v grep | awk '{print "内存使用: " $6/1024 " MB"}'

# 查看系统内存
free -h

# 查看最近日志
journalctl -u pixel.service -n 50 --no-pager

# 查看 403 拒绝（应该正常，但不占用内存）
journalctl -u pixel.service --since "5 minutes ago" | grep "codex_cli_only"
```

## 回滚方法（如果需要）

```bash
# 如果修复后有问题，可以回滚到之前的版本
cd /opt/sub2api
ls -la  # 查看是否有备份

# 如果有备份（通常是 sub2api.backup）
sudo systemctl stop pixel.service
sudo cp sub2api.backup current/sub2api
sudo systemctl start pixel.service
```

## 技术细节

详细的根因分析和修复原理请查看：
- `docs/MEMORY_LEAK_FIX_COMPLETE.md` - 完整修复报告
- `docs/memory-leak-rootcause-and-fix.md` - 根因分析

---

**创建时间**：2026-09-03 01:06
**二进制位置**：`test/sub2api-fix.gz`
**预计部署时间**：< 2 分钟
