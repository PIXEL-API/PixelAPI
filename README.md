# PixelAPI

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.4+-4FC08D.svg)](https://vuejs.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791.svg)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D.svg)](https://redis.io/)
[![License](https://img.shields.io/badge/License-LGPL--3.0-blue.svg)](LICENSE)

**面向账号共享的 AI API 网关平台**

中文 | [English](README_EN.md)

线上站点：[ai-pixel.online](https://ai-pixel.online)

</div>

> 本项目是 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 的二次开发分支（fork 自 v0.1.119），并非上游官方版本。
> 上游项目入口、许可与版权说明见文末 [上游项目](#上游项目)。

---

## 项目简介

PixelAPI 把 AI 订阅账号（Claude、Codex/OpenAI、Gemini、Antigravity、Grok）接入统一网关，
对外以标准 API 协议提供服务，对内负责鉴权、调度、并发控制、Token 级计费与账务结算。

与上游主要面向「站长自建号池」不同，本分支的重心是**多方参与的账号共享**：
号主把自己的账号托管进平台，用户按房间/分组选择号池发起调用，平台负责路由、计量、分账与风控。

## 与上游的主要差异

| 方向 | 本分支的增量 |
| --- | --- |
| 账号共享 | 私有自用 / 公共共享 / 账号广场房间三种模式，房间预约、排队、租约与结算生命周期 |
| 号主侧 | 号主收益账本、结算比例、提现与收款配置 |
| 上游平台 | 新增 Grok / xAI 接入，完善 Antigravity 与 OpenAI 图像、视频端点兼容 |
| 调度 | 代理归属（按账号绑定独立出站代理）、渠道监控、账号健康探测与不可用重排 |
| 计费 | 倍率积分、收益账本、计费 intent 状态机与异常结算收口 |
| 运营 | 发卡商城、兑换码、订阅、邀请返利、活动抽奖、发票、风控面板 |
| 运维 | 集群运行时、数据保留清理、备份、显式 SQL 迁移体系 |

## 功能

### 网关与协议兼容

| 端点 | 说明 |
| --- | --- |
| `POST /v1/messages`、`/v1/messages/count_tokens` | Anthropic Messages 协议 |
| `POST /v1/chat/completions` | OpenAI Chat Completions 协议 |
| `POST /v1/responses`、`/backend-api/codex/responses` | OpenAI Responses / Codex 协议 |
| `POST /v1beta/models/*` | Gemini generateContent 协议 |
| `POST /v1/images/generations`、`/v1/images/edits` | 图像生成与编辑 |
| `POST /v1/videos/generations`、`/edits`、`/extensions` | 视频生成相关端点 |
| `POST /antigravity/v1/messages`、`/antigravity/v1beta/` | Antigravity 专用端点 |

### 账号与调度

- 多平台账号接入：Anthropic、OpenAI、Gemini、Antigravity、Grok，支持 OAuth 与 API Key 两类凭证
- 分组调度与多分组路由回落，粘性会话保持同一上游账号
- 按用户、按账号的并发上限与请求/Token 限流
- 每账号独立代理归属，避免共享出站 IP 造成关联
- 账号健康探测、渠道监控与不可用账号自动重排

### 账号共享

- 私有模式：账号仅本人可用
- 公共模式：账号进入公共号池，按调用产生收益
- 账号广场：号主开房间自定义定价与限制，用户预约后由房间调度健康账号

### 计费与账务

- Token 级用量记录与成本核算，支持模型倍率与积分
- 号主收益账本、结算比例与提现流程
- 钱包充值、订阅套餐、订单与发票
- 计费熔断：计费异常时拒绝放行，避免无账可计的调用

### 管理与运维

- 管理端：用户、账号、分组、渠道、代理、活动、公告、风控、备份与运营数据面板
- 集群运行时与请求准入控制
- 显式 SQL 迁移体系（`backend/migrations`），生产升级需单独执行迁移
- 独立文档站（`docs/site`，Next.js + Fumadocs）

## 技术栈

| 组件 | 技术 |
| --- | --- |
| 后端 | Go 1.26、Gin、Ent |
| 前端 | Vue 3.4+、Vite、TailwindCSS |
| 数据库 | PostgreSQL 15+ |
| 缓存 / 队列 | Redis 7+ |
| 文档站 | Next.js + Fumadocs |

## 部署

### ⚠️ 本项目不提供 Docker 镜像和预编译二进制

**这一点请务必先看清楚，否则你装到的会是另一个项目。**

本仓库**没有**发布任何 Docker 镜像，Docker Hub 上不存在 PixelAPI 的镜像；本仓库的 Releases
也只提供源码，**不附带预编译二进制**。

而 `deploy/` 目录下的部署文件是从上游继承下来的，里面写死的地址**全部指向上游 Sub2API**：

| 文件 | 里面写的东西 | 实际会装到什么 |
| --- | --- | --- |
| `deploy/docker-compose.yml`<br>`deploy/docker-compose.local.yml` | `image: weishaw/sub2api:latest` | **上游 Sub2API 的官方镜像**，不是本项目 |
| `deploy/install.sh` | `GITHUB_REPO="Wei-Shaw/sub2api"` | 从**上游 Releases** 下载的二进制，不是本项目 |

也就是说，直接 `docker compose up -d` 或者跑那条一键安装脚本，起来的是上游 Sub2API，
本分支的账号广场、共享结算、Grok 接入等功能一个都不会有。

**唯一受支持的方式是[从源码构建](#从源码构建)。**

如果你确实需要容器化部署，得自己构建镜像——仓库根目录的 `Dockerfile` 是完整的多阶段构建
（前端 + 内嵌前端的 Go 二进制），可以直接用：

```bash
# 自行构建镜像
docker build -t pixelapi:local .

# 然后把 compose 文件里的 image 换成自己构建的 tag
#   image: weishaw/sub2api:latest   ->   image: pixelapi:local
```

同理，`deploy/install.sh` 若要复用，需要自行把 `GITHUB_REPO` 和产物下载逻辑改成你自己的发布源。

### 从源码构建

前置条件：Go 1.26+、Node.js 18+、pnpm、PostgreSQL 15+、Redis 7+。

```bash
git clone https://github.com/PIXEL-API/PixelAPI.git
cd PixelAPI

# 1. 构建前端，产物输出到 backend/internal/web/dist/
cd frontend
pnpm install
pnpm run build

# 2. 构建内嵌前端的后端二进制（不加 -tags embed 则不提供前端页面）
cd ../backend
go build -tags embed -o pixelapi ./cmd/server

# 3. 准备配置
cp ../deploy/config.example.yaml ./config.yaml
```

`config.yaml` 关键配置：

```yaml
server:
  host: "0.0.0.0"
  port: 8080
  mode: "release"

database:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "your_password"
  dbname: "pixelapi"

redis:
  host: "localhost"
  port: 6379
  password: ""

jwt:
  secret: "change-this-to-a-secure-random-string"
  expire_hour: 24
```

数据库迁移与启动：

```bash
# 先显式跑迁移，确认无误后再启动服务
./pixelapi --migrate-only

./pixelapi
```

### Nginx 反向代理注意事项

Nginx 默认会丢弃带下划线的请求头（如 `session_id`），这会破坏多账号场景下的粘性会话。
在 `http` 块中加入：

```nginx
underscores_in_headers on;
```

### 安全相关配置

`config.yaml` 中的安全项：

- `cors.allowed_origins`：CORS 允许来源
- `security.url_allowlist`：上游 / 计价 / CRS 域名白名单
- `security.url_allowlist.allow_insecure_http`：关闭白名单校验后是否允许 HTTP（明文传输，生产禁用）
- `security.response_headers`：响应头过滤
- `security.csp`：Content-Security-Policy
- `billing.circuit_breaker`：计费异常时熔断
- `server.trusted_proxies`：可信代理，决定 `X-Forwarded-For` 解析
- `turnstile.required`：release 模式下强制人机校验

## 开发

```bash
# 后端
cd backend
go run ./cmd/server

# 前端
cd frontend
pnpm run dev

# 文档站
cd docs/site
pnpm install
pnpm dev
```

修改 `backend/ent/schema` 后需要重新生成 Ent 与 Wire：

```bash
cd backend
go generate ./ent
go generate ./cmd/server
```

更多开发约定见 [DEV_GUIDE.md](DEV_GUIDE.md)。

## 目录结构

```
PixelAPI/
├── backend/                  # Go 后端
│   ├── cmd/server/           # 程序入口
│   ├── ent/                  # Ent schema 与生成代码
│   ├── migrations/           # 显式 SQL 迁移
│   └── internal/
│       ├── config/           # 配置
│       ├── domain/           # 领域常量与模型
│       ├── service/          # 业务逻辑（账号、共享、计费、调度）
│       ├── handler/          # HTTP 处理器
│       ├── server/routes/    # 路由与网关端点
│       ├── payment/          # 支付渠道
│       └── web/              # 前端内嵌产物
│
├── frontend/                 # Vue 3 前端
│   └── src/
│       ├── views/user/       # 用户端页面
│       ├── views/admin/      # 管理端页面
│       ├── stores/           # 状态管理
│       └── components/
│
├── docs/site/                # 文档站（Next.js + Fumadocs）
└── deploy/                   # 部署配置与脚本
```

## 文档

- 使用与接入文档：`docs/site`（线上文档以站内入口为准）
- 开发指南：[DEV_GUIDE.md](DEV_GUIDE.md)
- 部署说明：[deploy/README.md](deploy/README.md)

## 上游项目

本项目基于 [Sub2API](https://github.com/Wei-Shaw/sub2api) 二次开发，fork 自 v0.1.119。
上游项目的说明、部署方式与官方渠道请以上游仓库为准：

- 上游仓库：<https://github.com/Wei-Shaw/sub2api>
- 上游官方域名：`sub2api.org`、`pincc.ai`（本项目与其官方运营主体无从属关系）
- 上游作者：Wesley Liddick，版权与许可见 [LICENSE](LICENSE)

感谢上游作者与所有贡献者的工作。本分支自行承担其修改部分的维护责任，
遇到本分支的问题请在本仓库提 Issue，不要占用上游仓库的支持资源。

## 免责声明

> **使用前请仔细阅读：**
>
> :rotating_light: **服务条款风险**：使用本项目可能违反上游 AI 服务商（Anthropic、OpenAI、Google、xAI 等）的服务条款，
> 请自行阅读并评估。因使用本项目产生的一切风险由使用者自行承担。
>
> :book: **免责声明**：本项目仅用于技术学习与研究。作者对因使用本项目导致的账号封禁、服务中断或任何其他损失不承担责任。
>
> :moneybag: **账号共享风险**：账号共享功能涉及凭证托管与多方计费，请在自建部署前充分评估合规、资金与数据安全风险。

## 许可证

本项目基于 [GNU Lesser General Public License v3.0](LICENSE)（或更高版本）授权，与上游保持一致。

Copyright (c) 2026 Wesley Liddick（上游原始代码）
