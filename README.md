# StockTake Server

Stock counting management system for Total Retail / Spar Zimbabwe.

**Repo:** `stocktake-server` — Go API + Next.js admin portal  
**Mobile:** See separate `stocktake-mobile` repo (Android/Kotlin)

---

## Stack

| Service | Technology |
|---|---|
| API | Go 1.22, Gin, PostgreSQL 15, Redis 7 |
| Admin portal | Next.js 14, React, Tailwind CSS |
| Database | PostgreSQL 15 (Docker volume) |
| Cache / OTP | Redis 7 |
| Proxy | Nginx (TLS termination) |

---

## Quick start (development)

```bash
# 1. Clone and copy env
git clone <repo-url> stocktake-server
cd stocktake-server
cp .env.example .env
# Edit .env — fill in secrets and LS credentials

# 2. Start the stack
docker-compose up --build

# 3. Admin portal: http://localhost (or https if certs are configured)
# 4. API:          http://localhost/api/v1
# 5. WebSocket:    ws://localhost/ws/sessions/:id
```

For local development without Docker:

```bash
# API
cd backend
go run ./cmd/api

# Web
cd web
npm install
npm run dev   # http://localhost:3000
```

---

## Project structure

```
stocktake-server/
├── backend/                    Go API
│   ├── cmd/api/main.go         Entrypoint
│   ├── internal/
│   │   ├── auth/               JWT + OTP
│   │   ├── config/             Env config
│   │   ├── counting/           Count lines, bin submissions
│   │   ├── db/                 PostgreSQL connection + migrations
│   │   ├── ls/                 LS Commerce Service client
│   │   ├── reporting/          Counter performance reports
│   │   ├── server/             Gin router — wires all handlers
│   │   ├── session/            Stock take sessions + counters
│   │   ├── sms/                sms.localhost.co.zw client
│   │   ├── store/              Stores, zones, aisles, bays
│   │   ├── variance/           Consolidated view, audit, variance flags
│   │   └── ws/                 WebSocket hub
│   ├── migrations/             SQL migrations (goose)
│   └── pkg/middleware/         JWT auth middleware
│
├── web/                        Next.js admin portal
│   └── src/
│       ├── app/                App Router pages
│       │   ├── dashboard/      Overview dashboard
│       │   ├── stores/         Store + layout management
│       │   └── sessions/       Sessions + all sub-pages
│       │       └── [id]/
│       │           ├── monitor/        Live WebSocket feed
│       │           ├── consolidated/   Counts vs theoretical
│       │           ├── audit/          Per-item count lines
│       │           ├── variance/       Variance report + flag
│       │           └── performance/    Counter performance
│       ├── components/         Shared UI components
│       ├── lib/                API client, WebSocket hook, auth
│       └── types/              TypeScript types (mirrors backend)
│
├── nginx/                      Nginx config + certs dir
├── docker-compose.yml
├── .env.example
└── README.md
```

---

## Environment variables

Copy `.env.example` to `.env` and fill in:

| Variable | Description |
|---|---|
| `DB_PASSWORD` | PostgreSQL password |
| `REDIS_PASSWORD` | Redis password |
| `JWT_SECRET` | JWT signing secret (min 32 chars) |
| `OTP_SECRET` | OTP hash secret |
| `SMS_API_KEY` | sms.localhost.co.zw API key |
| `LS_BASE_URL` | LS Commerce Service OData base URL |
| `LS_USERNAME` | BC service user |
| `LS_PASSWORD` | BC service user password |
| `LS_COMPANY_ID` | BC company ID |
| `ERP_BACKEND` | `ls` (LS Central) or `vantage` (Vantage Retail) |
| `DEPLOYMENT_NAME` | Name the app's Server setup screen shows for this server (optional) |
| `DEVICE_TOKEN_DAYS` | Lifetime of a shared (NFC card) device's token, default 365; revoking the device cuts it off at once |

Shared counting devices with NFC counter cards, and the app's Server setup screen
(`GET /api/v1/system/info`), are described in `../NFC_Shared_Devices.md`.

---

## Database migrations

Migrations run automatically on startup via `goose`. Migration files are in `backend/migrations/` and are embedded in the binary.

To run manually:
```bash
cd backend
goose -dir migrations postgres "$DATABASE_URL" up
```

---

## Development phases

| Phase | Scope | Status |
|---|---|---|
| 1 | Foundation — Docker, DB, store/layout CRUD, label PDF | Scaffolded |
| 2 | Sessions, counter auth, OTP SMS | Scaffolded |
| 3 | Mobile counting app (separate repo) | — |
| 4 | Theoretical pull, audit/consolidated views, WebSocket | Scaffolded |
| 5 | Variance report, recount loop, accept/reject | Scaffolded |
| 6 | Final submission, counter performance reports, UAT | Scaffolded |

**Scaffolded** = structure, types, and service interfaces in place; business logic TODOs marked inline.

---

## Open items (confirm before build)

1. LS Commerce Service endpoint for stock count worksheet pull
2. LS Commerce Service endpoint for final count submission
3. sms.localhost.co.zw API auth method and payload format
4. Variance tolerance defaults
5. Number of stores in initial rollout
6. Production server specification
