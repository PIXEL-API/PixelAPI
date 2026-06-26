# 迁移、配置、部署与数据风险审查

## 已确认问题

### O-01 P1 迁移 checksum 兼容规则过宽

- 位置：`backend/internal/repository/migrations_runner.go:88`、`backend/internal/repository/migrations_runner.go:748`、`backend/internal/repository/migrations_runner_checksum_test.go:73`
- 证据：注释要求同时匹配“迁移名 + 数据库 checksum + 当前文件 checksum”，但 `isMigrationChecksumCompatible` 只检查 `dbChecksum` 和 `fileChecksum` 是否都存在于同一个 accepted 集合。
- 对照证据：测试明确允许“109 回滚到历史文件后仍兼容已应用的新 checksum”，见 `migrations_runner_checksum_test.go:73-80`。
- 影响：已上线迁移文件被替换成历史版本时仍可能通过校验，削弱迁移不可变性和审计能力，可能掩盖错误回滚或被动变更。
- 建议：改为显式 allowed pair，例如数据库 checksum 必须在 acceptedDBChecksum，当前文件 checksum 必须等于声明的 fileChecksum。若确实允许双向兼容，应改名和注释，并逐对声明。
- 置信度：高。

### O-02 P1 URL allowlist 默认关闭且允许私网/HTTP

- 位置：`backend/internal/config/config.go:1675`、`backend/internal/service/openai_gateway_service.go:5210`、`backend/internal/util/urlvalidator/validator.go:74`
- 证据：默认 `security.url_allowlist.enabled=false`、`allow_private_hosts=true`、`allow_insecure_http=true`。
- 对照证据：allowlist 关闭时 `validateUpstreamBaseURL` 只调用 `ValidateURLFormat`；该函数注释明确只做最小格式校验，不做白名单、私网或 SSRF 校验。
- 影响：如果生产沿用默认或示例配置，管理员可配置的上游 URL、定价 URL 等可能访问内网或明文 HTTP，存在 SSRF、凭据泄露和流量劫持风险。
- 建议：生产默认启用 allowlist，禁止私网和 HTTP。即使 allowlist 关闭，也应保留私网 IP 解析拦截。同步更新 `deploy/config.example.yaml` 和 README。
- 置信度：高。

### O-03 P1 root 安装脚本 checksum 缺失时继续安装

- 位置：`deploy/install.sh:572`、`README.md:161`、`deploy/README.md:356`
- 证据：脚本下载 checksums.txt 失败时只 `print_warning`，随后继续解压、复制、chmod 二进制。
- 对照证据：README 推荐 `curl ... install.sh | sudo bash` 的 root 安装路径。
- 影响：release 资产缺 checksum、网络异常或供应链被劫持时，脚本仍会以 root 权限安装未验证二进制。
- 建议：checksum 缺失默认 fail-closed。确需跳过时使用显式 `--allow-unverified`，并输出强提示。长期增加签名或 provenance 校验。
- 置信度：高。

### O-04 P2 部署脚本明文打印生成的生产密钥

- 位置：`deploy/docker-deploy.sh:103`、`deploy/docker-deploy.sh:138`
- 证据：脚本生成 `JWT_SECRET`、`TOTP_ENCRYPTION_KEY`、`POSTGRES_PASSWORD` 并写入 `.env`，随后在 stdout 明文打印三者。
- 影响：SSH 录屏、终端历史、CI 日志或协作平台日志可能泄露长期密钥。
- 建议：默认只打印 `.env` 路径和权限。密钥显示改为显式 `--show-secrets`，并可只显示前后几位。
- 置信度：高。

### O-05 P2 部署模板默认使用 mutable latest 且公网绑定

- 位置：`deploy/docker-compose.yml:19`、`deploy/docker-compose.yml:26`
- 证据：Compose 默认 `weishaw/sub2api:latest`，端口默认绑定 `${BIND_HOST:-0.0.0.0}`。
- 影响：生产可能被动升级到不可预期镜像；服务默认对公网暴露，增加误暴露和回滚失败风险。
- 建议：生产模板使用固定 tag/digest 或自建镜像 tag；默认绑定 `127.0.0.1`，公网暴露必须通过反代和防火墙显式配置。
- 置信度：高。

## 待确认风险

### O-R01 P1 破坏性迁移随启动自动执行

- 位置：`backend/migrations/172_drop_legacy_conversations.sql:3`、`backend/internal/repository/migrations_runner.go:177`、`backend/migrations/README.md:116`
- 证据：迁移直接 `DROP TABLE IF EXISTS conversation_messages, conversations`；runner 会按 SQL 文件排序执行未应用迁移；文档说明服务启动会自动执行迁移。
- 限制：本轮未访问生产数据库，无法确认目标表是否仍有数据或是否已有备份。
- 风险：如果目标表仍有生产数据或确认流程缺失，部署启动即可触发不可逆删除。
- 建议：破坏性迁移拆为显式运维动作，加入行数预检、备份确认、人工开关和审计记录。
- 置信度：中。

### O-R02 P2 事务内迁移包含全表更新、约束校验和非并发索引

- 位置：`backend/migrations/175_affiliate_weekly_invite_codes.sql:3`、`backend/internal/repository/migrations_runner.go:261`
- 证据：普通迁移在单事务内执行；该迁移对 `user_affiliates` 增加 NOT NULL DEFAULT、执行多次全表 UPDATE，再创建普通索引。`177_user_load_factor_credits.sql`、`180_proxy_owner_user_id.sql` 也有类似风险。
- 风险：热点大表上可能长时间锁表或阻塞写入，启动期迁移失败会影响发布窗口。
- 建议：按“加 nullable 列、批量回填、NOT VALID 约束、VALIDATE、并发索引 notx”拆分，并设置 `lock_timeout` 和 `statement_timeout`。
- 置信度：高。

### O-R03 P3 subsite 删除后仍残留部署模板和历史 schema 边界

- 位置：`deploy/.env.subsite.example:1`、`backend/migrations/176_drop_subsite_control_plane.sql:1`
- 证据：多数 subsite agent 文件已删除，但部署模板仍残留；176 迁移说明 cleanup deferred 且只 `SELECT 1`。
- 风险：维护者可能误用残留模板，或不清楚历史表是否应继续保留。
- 建议：明确产品决策。保留则标注废弃/历史兼容；移除则删除模板，数据库表清理单独走审批和维护窗口。
- 置信度：中。

## 数据安全说明

- 本报告没有执行任何数据库读写。
- 涉及余额负数、破坏性迁移是否影响生产数据的结论，必须用只读 SQL 现场验证后才能升级为已确认问题。
