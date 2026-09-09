# Admin Dashboard

Read `../AGENTS.md` for repository-wide rules. This file adds dashboard-specific guidance.

## Context

| Item | Value |
|------|-------|
| **Service** | Unified admin dashboard (Go backend + React frontend) |
| **Entrypoints** | Backend: `backend/cmd/admin-dashboard/main.go`, Frontend: `frontend/src/main.tsx` |
| **Ports** | :30190 (combined) |

### Tech Stack

| Layer | Technology |
|-------|------------|
| **Frontend** | React, TypeScript, Vite, TailwindCSS, shadcn/ui; versions are owned by `frontend/package.json` and its lockfile |
| **Backend** | Go 1.27.1 toolchain, Gin 1.12 on `net/http`, Valkey session store, gorilla/websocket |
| **State** | TanStack Query v5 (server), Zustand (client) |
| **Auth** | HMAC-signed session cookie, CSRF token, heartbeat rotation |

### Key Files

| Task | Location |
|------|----------|
| Route assembly | `backend/internal/app/app.go` |
| Auth helpers | `backend/internal/auth/` |
| Session store | `backend/internal/session/` |
| Docker control | `backend/internal/docker/` |
| Config | `backend/internal/config/` |
| Holo API proxy | `backend/internal/holo/` |
| API client | `frontend/src/api/client.ts` |

## Standards

### Architecture Patterns

- **Proxy Mode**: Authenticate requests at the dashboard, then forward them to the upstream Hololive Admin API with `X-API-Key` injection.
- **Runtime Contract**: Admin dashboard backend is Go-only. `scripts/architecture/check-admin-dashboard-go-only.sh` fails when `admin-dashboard/backend` carries `*.rs`, `Cargo.toml`, or `Cargo.lock`, or when the backend or `admin-dashboard/Dockerfile` still references Rust-only tooling.
- **WebSocket**: Enforce concurrency limits and origin validation for the real-time system statistics stream.

### Security

- **CSRF**: Token-based protection with enforce/monitor/off modes.
- **Rate Limit**: In-memory per-IP login attempt limiting with lockout.
- **Heartbeat**: Session refresh and token rotation every configured interval.
- **Cookies**: HttpOnly session cookie, SameSite=Strict, Secure controlled by `FORCE_HTTPS`.

### Commands

Select checks for the changed behavior and run them from the `hololive-bot` repository root. Documentation-only edits normally need diff inspection; publication and explicitly required gates remain mandatory. Subshells keep each command's working directory independent.

```bash
# Backend
(cd admin-dashboard/backend && make lint && make test && make build)

# Frontend
(cd admin-dashboard/frontend && npm run lint && npm run build)
```

For full backend validation required by the publication workflow, applicable instructions, or the requested validation scope, use the gate below. It includes the Go-only architecture check, backend build, tests, and lint checks; do not repeat covered checks for the same unchanged inputs.

```bash
./scripts/ci/admin-dashboard-go-ci.sh
```

### Architecture Validation

Run the Go-only check when changes affect backend language/tooling, `admin-dashboard/Dockerfile`, or dashboard membership in the Go workspace or CI module list. The full backend gate above already includes it.

```bash
./scripts/architecture/check-admin-dashboard-go-only.sh
```
