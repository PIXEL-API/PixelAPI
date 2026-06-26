# 全项目代码审查报告总览 2026-06-14

## 审查边界

- 审查对象：当前工作区 `C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119`，包含已修改、已删除和未跟踪文件。
- 审查方式：只读静态审查为主，结合多方向并行审查结果后再本地交叉复核关键证据。
- 未执行事项：未访问生产数据库，未做任何数据库写入，未修改业务代码。
- 工作区状态：`git status --porcelain` 当前 221 条变更，其中 27 个 tracked 删除、25 个未跟踪文件。报告结论只代表当前脏工作区，不等价于已提交分支或生产发布包。

## 严重级别

- P0：已确认会造成严重生产事故、数据破坏或可直接利用的关键漏洞。本轮未确认 P0。
- P1：高优先级安全、数据完整性、认证、供应链或迁移风险，应优先修复。
- P2：明确影响用户体验、业务正确性、运维可靠性或测试可信度的问题。
- P3：维护性、可读性、测试基础设施、边界清理问题。

## 报告文件

- `docs/CODE_REVIEW_2026-06-14_BACKEND.md`：后端认证、网关、账号模式、计费和业务逻辑。
- `docs/CODE_REVIEW_2026-06-14_FRONTEND.md`：前端状态刷新、公告弹窗、管理端操作和首页安全体验。
- `docs/CODE_REVIEW_2026-06-14_OPS_DATA.md`：迁移、配置、部署脚本、供应链和数据风险。
- `docs/CODE_REVIEW_2026-06-14_TESTING.md`：测试覆盖、CI/Makefile、嵌入构建、工作区可复现性。

## 优先修复清单

1. P1：修复 Gemini/Antigravity v1beta API key 校验绕过问题。
2. P1：收紧迁移 checksum 兼容规则，避免历史版本双向放行。
3. P1：安装脚本 checksum 缺失时默认失败，不继续 root 安装。
4. P1：生产 URL allowlist 默认启用，禁止私网和明文 HTTP，至少不要在 allowlist 关闭时完全失去 SSRF 防线。
5. P2：refresh token 存储失败时不要返回成功登录。
6. P2：账号模式 seat billing worker 纳入服务统一 shutdown。
7. P2：修复平台支付抽奖后余额/积分不刷新、公告滚动锁和批量删除确认不足。
8. P2：把完整前端 Vitest、带 tag 的 Go 测试和嵌入构建链路纳入明确验证口径。

## 已确认问题统计

- P1：4 项。
- P2：16 项。
- P3：4 项。
- 待确认风险：9 项。

## 误判撤销

- 未确认 Usage Billing 重复计费：`usage_billing_repo.go` 有事务和 `(request_id, api_key_id)` 去重路径。
- 未确认商城发卡并发超卖：`shop.go` 使用订单行锁和 `FOR UPDATE SKIP LOCKED`。
- 未确认支付 webhook 先处理后验签：webhook handler 先验签再履约。
- 未确认 CORS 通配符加 credentials 风险：当前 middleware 在 wildcard 时会关闭 credentials，并对非允许 origin 的 preflight 返回 forbidden。
- 未确认公告/法律文档 Markdown XSS：公告和法律文档渲染使用 DOMPurify；首页自定义 HTML 是单独的设计风险。
