# 测试、构建与工程卫生审查

## 已确认问题

### T-01 P1 前端 CI 和 `make test` 只跑 6 个 critical Vitest

- 位置：`Makefile:3`、`Makefile:32`、`Makefile:38`、`.github/workflows/backend-ci.yml:48`、`frontend/package.json:14`
- 证据：`FRONTEND_CRITICAL_VITEST` 只列 6 个文件，`make test-frontend` 执行 lint、typecheck 和 critical Vitest。
- 对照证据：当前 `frontend/src` 下有 92 个 `.spec/.test.ts` 文件；`frontend/package.json` 已有完整 `test:run`，但 CI 只调用 `make test-frontend`。
- 影响：CI 通过不能证明前端组件、store、API、router 的全量回归。新增页面或账号模式 UI 测试不会默认进入 gate。
- 建议：CI 增加 `pnpm --dir frontend run test:run`。保留 critical Vitest 作为 smoke，但不要把它当完整前端测试。覆盖率门槛需单独跑 `test:coverage`。
- 置信度：高。

### T-02 P2 后端本地/文档验证口径与 Go build tag 不一致

- 位置：`backend/Makefile:13`、`docs/OFFICIAL_UPDATE_AND_DEPLOY_CN.md:181`
- 证据：本地入口和文档主要写 `go test ./...`；当前后端 536 个测试文件中 239 个带 build tag，其中包括 `unit`、`integration`、`e2e`、`embed`。
- 影响：维护者按文档执行后容易误以为跑了全量后端测试，但带 tag 的测试不会被裸 `go test ./...` 覆盖。
- 建议：文档和根入口明确区分 `go test ./...`、`make -C backend test-unit`、`make -C backend test-integration`、`embed` 构建测试。
- 置信度：高。

### T-03 P2 多个 Makefile/工具入口已失效

- 位置：`Makefile:23`、`Makefile:43`、`backend/Makefile:24`、`deploy/Makefile:8`
- 证据：`datamanagement` 目录不存在；`tools/secret_scan.py` 不存在；`backend/scripts/e2e-test.sh` 不存在；`deploy/Makefile` 在 deploy 目录内引用不存在的 `deploy/cmd/server` 和 `deploy/go.mod`。
- 影响：工具链入口给维护者错误信号，关键时刻运行会失败。`secret-scan` 失效尤其会降低发布前安全检查可信度。
- 建议：删除失效目标，或改成真实存在的脚本/目录；`deploy/Makefile` 若保留，应代理到 `../backend`。
- 置信度：高。

### T-04 P2 嵌入式前端构建被本地 ignored 产物掩盖

- 位置：`.gitignore:99`、`backend/internal/web/embed_on.go:27`
- 证据：`.gitignore` 注释要求保留 `backend/internal/web/dist/.keep`，但当前 `.keep` 不存在；本机 `backend/internal/web/dist/index.html` 和 `assets/` 存在且被 ignore。
- 影响：本地 `-tags embed` 构建可能因为残留 dist 产物通过，干净 clone 或 CI 若没有先构建前端则不可复现。
- 建议：提交 `.keep` 并明确 embed 构建前置步骤，或让 `build-embed/test-embed` 显式依赖 `pnpm --dir frontend run build`。
- 置信度：高。

### T-05 P2 当前脏工作区会让验证和发布不可复现

- 位置：全仓库状态。
- 证据：`git status --porcelain` 当前 221 条；27 个 tracked 删除，25 个未跟踪文件，包括新迁移、账号模式代码和测试。
- 影响：本地测试会包含未跟踪文件，但提交、CI、发布包可能不包含；删除的测试和部署文件也会影响覆盖口径。
- 建议：发布或交付前用 `git status --short`、`git ls-files -o --exclude-standard`、`git ls-files -d` 固化边界。未跟踪迁移和测试必须纳入同一变更或明确排除。
- 置信度：高。

### T-06 P3 Vitest 全局 setup 文件未注册

- 位置：`frontend/vitest.config.ts:13`、`frontend/src/__tests__/setup.ts:1`
- 证据：`vitest.config.ts` 的 `test` 配置没有 `setupFiles`；`setup.ts` 定义了 `requestIdleCallback`、`IntersectionObserver`、`ResizeObserver` 等全局 mock，但未被引用。
- 影响：测试基础设施形同死代码，依赖这些 mock 的测试只能单测内重复 stub，容易产生随机失败或覆盖空洞。
- 建议：如果需要全局 mock，加入 `test.setupFiles`；如果不需要，删除该 setup 文件。
- 置信度：中。

### T-07 P3 未使用 helper 增加维护噪音

- 位置：`backend/internal/service/account_share_mode.go:1883`
- 证据：`AccountShareHourlyCharge` 只在定义处被搜索到，没有业务调用。
- 影响：容易误导维护者以为小时计费逻辑在使用该 helper；真实计费路径不在这里。
- 建议：删除未使用函数，或补到真实计费路径并加测试。
- 置信度：高。

### T-08 P3 行尾规则未覆盖部分关键文本文件

- 位置：`.gitattributes:1`
- 证据：当前规则覆盖 SQL、Go、前端源码、shell、yaml、Dockerfile，但 `git diff --check` 提示 `backend/cmd/server/VERSION`、`backend/go.sum`、`deploy/README.md` 下次会 LF->CRLF。
- 影响：Windows 本机容易产生无业务意义的换行噪音，影响 review 和补丁可信度。
- 建议：为 `*.md`、`go.sum`、`VERSION` 增加规则，或建立全局 `* text=auto eol=lf`。
- 置信度：中。

## 建议验证矩阵

- 快速后端：`go test ./...`，只覆盖 untagged 测试。
- 后端 unit：`make -C backend test-unit`。
- 后端 integration：`make -C backend test-integration`，需要 Docker/Postgres/Redis 依赖。
- 前端完整：`pnpm --dir frontend run test:run`。
- 前端类型与 lint：`pnpm --dir frontend run typecheck`、`pnpm --dir frontend run lint:check`。
- 嵌入构建：先 `pnpm --dir frontend run build`，再 `go -C backend build -tags embed ./cmd/server`。
