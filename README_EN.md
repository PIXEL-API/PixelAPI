# PixelAPI

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.4+-4FC08D.svg)](https://vuejs.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-15+-336791.svg)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D.svg)](https://redis.io/)
[![License](https://img.shields.io/badge/License-LGPL--3.0-blue.svg)](LICENSE)

**An AI API gateway platform built around account sharing**

[中文](README.md) | English

Live site: [ai-pixel.online](https://ai-pixel.online)

</div>

> This is a downstream fork of [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api), forked at v0.1.119.
> It is **not** the official upstream distribution. See [Upstream Project](#upstream-project) below.

---

## Overview

PixelAPI connects AI subscription accounts (Claude, Codex/OpenAI, Gemini, Antigravity, Grok) to a single
gateway. It exposes standard API protocols to clients and handles authentication, scheduling, concurrency
control, token-level billing and settlement internally.

Where upstream targets a single operator running their own account pool, this fork focuses on
**multi-party account sharing**: account owners host their credentials on the platform, users pick a pool
by room or group, and the platform handles routing, metering, revenue split and risk control.

## What This Fork Adds

| Area | Additions |
| --- | --- |
| Account sharing | Private / public / marketplace-room modes, with room booking, queueing, lease and settlement lifecycle |
| Owner side | Owner revenue ledger, settlement ratios, withdrawal and payout configuration |
| Upstream platforms | Grok / xAI support; broader Antigravity, OpenAI image and video endpoint coverage |
| Scheduling | Per-account outbound proxy binding, channel monitoring, health probing and unavailable-account rescheduling |
| Billing | Rate multipliers and credits, revenue ledger, billing-intent state machine with anomaly containment |
| Operations | Card store, redeem codes, subscriptions, affiliate rebates, campaigns, invoices, risk-control panel |
| Infrastructure | Cluster runtime, data-retention cleanup, backups, explicit SQL migration system |

## Features

### Gateway & Protocol Compatibility

| Endpoint | Protocol |
| --- | --- |
| `POST /v1/messages`, `/v1/messages/count_tokens` | Anthropic Messages |
| `POST /v1/chat/completions` | OpenAI Chat Completions |
| `POST /v1/responses`, `/backend-api/codex/responses` | OpenAI Responses / Codex |
| `POST /v1beta/models/*` | Gemini generateContent |
| `POST /v1/images/generations`, `/v1/images/edits` | Image generation and editing |
| `POST /v1/videos/generations`, `/edits`, `/extensions` | Video endpoints |
| `POST /antigravity/v1/messages`, `/antigravity/v1beta/` | Antigravity dedicated endpoints |

### Accounts & Scheduling

- Multi-platform accounts: Anthropic, OpenAI, Gemini, Antigravity, Grok — OAuth and API Key credentials
- Group-based scheduling with multi-group fallback routing; sticky sessions pin a conversation to one account
- Per-user and per-account concurrency caps plus request/token rate limits
- Per-account outbound proxy binding to avoid shared-egress correlation
- Health probing, channel monitoring and automatic rescheduling away from unavailable accounts

### Account Sharing

- **Private** — the account serves only its owner
- **Public** — the account joins the public pool and earns on each call
- **Marketplace** — owners open rooms with their own pricing and limits; users book a room and the room
  dispatches a healthy account

### Billing & Accounting

- Token-level usage records and cost accounting, with model rate multipliers and credits
- Owner revenue ledger, settlement ratios and withdrawal flow
- Wallet top-up, subscription plans, orders and invoices
- Billing circuit breaker: requests fail closed when billing cannot be recorded

### Administration & Operations

- Admin console for users, accounts, groups, channels, proxies, campaigns, announcements, risk control,
  backups and operational dashboards
- Cluster runtime with request admission control
- Explicit SQL migrations (`backend/migrations`) — production upgrades run migrations as a separate step
- Standalone documentation site (`docs/site`, Next.js + Fumadocs)

## Tech Stack

| Component | Technology |
| --- | --- |
| Backend | Go 1.26, Gin, Ent |
| Frontend | Vue 3.4+, Vite, TailwindCSS |
| Database | PostgreSQL 15+ |
| Cache / Queue | Redis 7+ |
| Docs site | Next.js + Fumadocs |

## Deployment

> **Do not confuse this project's artifacts with upstream's.** This project ships
> `ghcr.io/pixel-api/pixelapi` and a binary named `pixelapi`. The widely circulated
> `weishaw/sub2api` image and `Wei-Shaw/sub2api` install script belong to **upstream Sub2API** and
> contain none of this fork's account marketplace, shared-revenue settlement or Grok support.

### Option 1: Docker Image

Images are published to the GitHub Container Registry for linux/amd64 and linux/arm64:

```bash
docker pull ghcr.io/pixel-api/pixelapi:latest
```

Available tags: `latest`, `1.2.29` (exact version), `1.2` (minor track), `1` (major track).

Deploy with Docker Compose (bundles PostgreSQL and Redis):

```bash
mkdir -p pixelapi-deploy && cd pixelapi-deploy

# Fetch the deployment files
curl -sSLO https://raw.githubusercontent.com/PIXEL-API/PixelAPI/main/deploy/docker-compose.local.yml
curl -sSLO https://raw.githubusercontent.com/PIXEL-API/PixelAPI/main/deploy/.env.example
cp .env.example .env

# Generate secrets for .env: POSTGRES_PASSWORD / JWT_SECRET / TOTP_ENCRYPTION_KEY
openssl rand -hex 32

mkdir -p data postgres_data redis_data
docker compose -f docker-compose.local.yml up -d
```

Open `http://YOUR_SERVER_IP:8080` for the setup wizard.

### Option 2: Install Script

Downloads the matching binary from this repository's Releases and registers a systemd service:

```bash
curl -sSL https://raw.githubusercontent.com/PIXEL-API/PixelAPI/main/deploy/install.sh | sudo bash
```

Prerequisites: Linux (amd64 or arm64), PostgreSQL 15+ and Redis 7+ already installed and running,
root privileges.

Afterwards:

```bash
sudo systemctl start pixelapi
sudo systemctl enable pixelapi
```

Installs to `/opt/pixelapi`, config in `/etc/pixelapi`, service name `pixelapi`.

### Option 3: Download a Binary

[Releases](https://github.com/PIXEL-API/PixelAPI/releases) carry archives for five platform targets
across Linux, macOS and Windows plus `checksums.txt`. Extract and run — the frontend is embedded, so
there are no runtime dependencies.

### Build From Source

Prerequisites: Go 1.26+, Node.js 18+, pnpm, PostgreSQL 15+, Redis 7+.

```bash
git clone https://github.com/PIXEL-API/PixelAPI.git
cd PixelAPI

# 1. Build the frontend; output lands in backend/internal/web/dist/
cd frontend
pnpm install
pnpm run build

# 2. Build the backend with the frontend embedded
#    (without -tags embed the binary will not serve the UI)
cd ../backend
go build -tags embed -o pixelapi ./cmd/server

# 3. Prepare configuration
cp ../deploy/config.example.yaml ./config.yaml
```

Key settings in `config.yaml`:

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

Migrate, then start:

```bash
# Run migrations explicitly and verify before starting the service
./pixelapi --migrate-only

./pixelapi
```

### Nginx Reverse Proxy Note

Nginx drops headers containing underscores by default (e.g. `session_id`), which breaks sticky session
routing in multi-account setups. Add this to the `http` block:

```nginx
underscores_in_headers on;
```

### Security-Related Configuration

- `cors.allowed_origins` — CORS allowlist
- `security.url_allowlist` — upstream / pricing / CRS host allowlists
- `security.url_allowlist.allow_insecure_http` — allow plaintext HTTP when the allowlist is disabled
  (unsafe; never in production)
- `security.response_headers` — response header filtering
- `security.csp` — Content-Security-Policy
- `billing.circuit_breaker` — fail closed on billing errors
- `server.trusted_proxies` — controls `X-Forwarded-For` parsing
- `turnstile.required` — require Turnstile in release mode

## Development

```bash
# Backend
cd backend
go run ./cmd/server

# Frontend
cd frontend
pnpm run dev

# Docs site
cd docs/site
pnpm install
pnpm dev
```

After editing `backend/ent/schema`, regenerate Ent and Wire:

```bash
cd backend
go generate ./ent
go generate ./cmd/server
```

See [DEV_GUIDE.md](DEV_GUIDE.md) for further conventions.

## Project Structure

```
PixelAPI/
├── backend/                  # Go backend
│   ├── cmd/server/           # Application entry
│   ├── ent/                  # Ent schema and generated code
│   ├── migrations/           # Explicit SQL migrations
│   └── internal/
│       ├── config/           # Configuration
│       ├── domain/           # Domain constants and models
│       ├── service/          # Business logic (accounts, sharing, billing, scheduling)
│       ├── handler/          # HTTP handlers
│       ├── server/routes/    # Routing and gateway endpoints
│       ├── payment/          # Payment channels
│       └── web/              # Embedded frontend assets
│
├── frontend/                 # Vue 3 frontend
│   └── src/
│       ├── views/user/       # User-facing pages
│       ├── views/admin/      # Admin console pages
│       ├── stores/           # State management
│       └── components/
│
├── docs/site/                # Documentation site (Next.js + Fumadocs)
└── deploy/                   # Deployment configuration and scripts
```

## Upstream Project

This project is derived from [Sub2API](https://github.com/Wei-Shaw/sub2api), forked at v0.1.119.
For upstream documentation, deployment methods and official channels, refer to the upstream repository:

- Upstream repository: <https://github.com/Wei-Shaw/sub2api>
- Upstream official domains: `sub2api.org`, `pincc.ai` (this fork is not affiliated with them)
- Upstream author: Wesley Liddick — copyright and license in [LICENSE](LICENSE)

Thanks to the upstream author and all contributors. This fork maintains its own modifications; please
open issues about this fork here rather than consuming upstream's support resources.

## Disclaimer

> **Please read carefully before using this project:**
>
> :rotating_light: **Terms of Service risk**: Using this project may violate the terms of service of
> upstream AI providers (Anthropic, OpenAI, Google, xAI and others). Read and assess them yourself. All
> risk arising from use of this project is borne solely by the user.
>
> :book: **No warranty**: This project is for technical learning and research. The authors accept no
> responsibility for account suspension, service interruption or any other loss.
>
> :moneybag: **Account-sharing risk**: Account sharing involves custody of credentials and multi-party
> billing. Assess compliance, financial and data-security risk thoroughly before self-hosting.

## License

Licensed under the [GNU Lesser General Public License v3.0](LICENSE) (or later), same as upstream.

Copyright (c) 2026 Wesley Liddick (original upstream code)
