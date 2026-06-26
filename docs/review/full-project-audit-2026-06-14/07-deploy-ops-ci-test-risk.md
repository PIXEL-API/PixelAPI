# 部署、运维、CI 与测试风险

## 本地验证结果

- `go test ./...`，工作目录 `backend`：通过。
- `pnpm --dir frontend run typecheck`：通过。
- `pnpm --dir frontend run lint:check`：通过。
- `pnpm --dir frontend run test:run`：失败，21 个前端测试失败。

本次未运行 `pnpm run lint`，因为该命令带 `--fix` 会修改文件。未运行 migration apply、Docker build、release workflow、install.sh。

## [P1] Release workflow 不依赖测试和 lint

- 状态：已确认问题
- 类型：CI / 发布门禁 / 运维
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\.github\workflows\release.yml:88`
- 证据 1：`release.yml:88` 的 release job 只 `needs: [update-version, build-frontend]`，没有依赖 backend/frontend 测试或 lint。
- 证据 2：关键测试在独立 `backend-ci.yml:26`、`:29` 和 `Makefile:33` 到 `:38`，release workflow 没有把这些 job 作为门禁；`release.yml:179` 使用 `goreleaser ... --skip=validate`。
- 触发场景：打 `v*` tag 或手动 `workflow_dispatch` 发布。
- 用户体验：用户可能拿到含测试失败或迁移缺陷的正式 release。
- 代码逻辑影响：发布 artifact 与 CI 质量门禁割裂。
- 风险后果：迁移、支付、repository 或前端关键链路失败仍可发布。
- 建议：把后端 unit/integration、前端 lint/typecheck/critical vitest 纳入 release workflow 必经 job，或要求 required status 后才能发布。
- 置信度：High

## [P2] install.sh 升级先停服务再下载，失败无自动健康回滚

- 状态：已确认问题
- 类型：运维 / 发布 / 回滚
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\deploy\install.sh:811`
- 证据 1：`deploy\install.sh:811` 到 `:820` 升级流程先 `systemctl stop sub2api`，之后才获取版本、下载和解压。
- 证据 2：`deploy\install.sh:815` 备份旧二进制，但 `deploy\install.sh:827` 直接启动服务，未见启动后健康检查失败自动回滚。
- 触发场景：GitHub API/下载/解压失败，或新版本启动后不健康。
- 用户体验：服务可能在升级脚本中断后保持停止，或坏版本上线。
- 代码逻辑影响：发布切换不是先准备后原子替换。
- 风险后果：生产不可用，需要人工恢复。
- 建议：先下载校验到临时目录，再停服原子切换；启动后轮询 `/health`，失败自动恢复备份并重启。
- 置信度：High

## [P2] Docker compose 默认 latest，版本不可追踪

- 状态：已确认问题
- 类型：运维 / 发布可追溯性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\deploy\docker-compose.local.yml:27`
- 证据 1：`deploy\docker-compose.local.yml:27` 使用 `image: weishaw/sub2api:latest`，`deploy\docker-compose.yml:19` 同类配置也使用 latest。
- 证据 2：`deploy\README.md:176` 到 `:178` 指导直接 `docker compose pull` 后 `up -d`。
- 触发场景：运维按文档升级或重拉镜像。
- 用户体验：回滚时无法确定用户当时运行的具体镜像。
- 代码逻辑影响：运行版本不能稳定映射到 git tag/artifact。
- 风险后果：故障复现、回滚、审计困难。
- 建议：使用 `${SUB2API_VERSION}` 或 digest pinning；文档明确升级/回滚 tag。
- 置信度：High

## [P2] test 目录存在读取生产连接凭据的远程探测脚本

- 状态：已确认问题
- 类型：安全 / 运维 / 测试卫生
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\test\remote_probe.py:10`
- 证据 1：`test\remote_probe.py:10` 从仓库根目录 `main_data.md` 读取凭据，`:61` 到 `:71` 使用 Paramiko 连接远程。
- 证据 2：`test\ssh_askpass.ps1:3` 到 `:9`、`test\ssh_askpass.go:17` 到 `:26` 也读取同一凭据并输出密码；`.gitignore:57` 忽略 `main_data.md`。
- 触发场景：本地脚本误运行、test 目录被共享、或调试文件被误打包。
- 用户体验：正常用户无感，但生产连接凭据暴露面扩大。
- 代码逻辑影响：测试目录混入生产运维凭据读取逻辑。
- 风险后果：非预期远程探测、凭据泄漏、审计困难。
- 建议：生产探测脚本移出仓库工作区，或只接受显式环境变量/密钥管理；清理 askpass 可执行物。
- 置信度：High

## [P2] 前端全量 Vitest 当前失败 21 个测试

- 状态：已确认问题
- 类型：测试 / 回归风险
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\frontend\src\components\account\__tests__\BulkEditAccountModal.spec.ts`
- 证据 1：`pnpm --dir frontend run test:run` 失败，输出 `Failed Tests 21`。
- 证据 2：失败集中在 `BulkEditAccountModal.spec.ts` 缺 active Pinia、`useModelWhitelist.spec.ts` 模型白名单预期、`AccountUsageCell.spec.ts`、`settings.authSourceDefaults.spec.ts`。
- 触发场景：全量前端测试或 CI 扩大测试范围。
- 用户体验：未必直接暴露，但代表账号批量编辑、模型白名单、设置保存 helper 等路径存在回归信号。
- 代码逻辑影响：测试夹具或实现与预期不一致。
- 风险后果：当前 Release workflow 不以全量前端测试为门禁，失败可能被带入发布。
- 建议：先修复失败测试或明确更新测试预期；把关键失败用例纳入 release 门禁。
- 置信度：High

## [P2] 前端 CI 只跑 critical subset，关键新模块未必纳入门禁

- 状态：已确认问题
- 类型：CI / 测试覆盖
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\Makefile:3`
- 证据 1：`Makefile:3` 到 `:9` 的 `FRONTEND_CRITICAL_VITEST` 只包含 LinuxDoCallback、WechatCallback、PaymentView、PaymentResult、ProfileInfoCard、SettingsView。
- 证据 2：`Makefile:33` 到 `:38` 的 `test-frontend` 只运行 lint:check、typecheck 和 critical vitest，不跑全量 `test:run`。
- 触发场景：账号共享、商城、管理批量操作、模型白名单、Toast/Dialog 等模块发生变更。
- 用户体验：相关回归可能在 CI 中漏过。
- 代码逻辑影响：当前门禁覆盖范围滞后于功能复杂度。
- 风险后果：全量测试已失败但 critical subset 可能通过，发布信号失真。
- 建议：扩展 critical list，至少纳入商城、账号共享、API Key、BulkEdit、ModelWhitelist、Toast/Dialog。
- 置信度：High

## [P3] audit exception 有高危依赖例外，需要到期前收口

- 状态：已确认问题
- 类型：供应链 / 维护性
- 位置：`C:\Users\寇振琦\Desktop\Codex\api\sub2api-0.1.119\.github\audit-exceptions.yml`
- 证据 1：`security-scan.yml` 先生成 `audit.json`，再用 `tools/check_pnpm_audit_exceptions.py` 校验例外清单，不是简单忽略。
- 证据 2：例外清单里存在 axios、xlsx、lodash 等 high/critical 到期例外。
- 触发场景：例外到期、依赖漏洞扩大、或 scanner 规则更新。
- 用户体验：通常无感，但漏洞修复被延后。
- 代码逻辑影响：安全扫描依赖例外文件质量。
- 风险后果：供应链风险积累。
- 建议：到期前升级或替换相关依赖；例外需要 owner、到期日和修复计划。
- 置信度：High
