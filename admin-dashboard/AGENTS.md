INHERIT: ../AGENTS.md

# Admin Dashboard

## L1: Context

| Item | Value |
|------|-------|
| **Service** | Unified admin dashboard (Go backend + React frontend) |
| **Entrypoints** | Backend: `backend/cmd/admin-dashboard/main.go`, Frontend: `frontend/src/main.tsx` |
| **Ports** | :30190 (combined) |

### Tech Stack

| Layer | Technology |
|-------|------------|
| **Frontend** | React 19, TypeScript, Vite 7, TailwindCSS 4, shadcn/ui |
| **Backend** | Go 1.26.4 toolchain, `net/http` ServeMux, Valkey session store, gorilla/websocket |
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

## L2: Standards

### Architecture Patterns

- **Proxy Mode**: Authenticate once at dashboard and relay upstream Hololive Admin API with `X-API-Key` injection.
- **Runtime Contract**: Admin dashboard backend is Go-only. Rust/Cargo artifacts under `admin-dashboard/backend` are forbidden.
- **WebSocket**: Real-time system stats stream is bounded by concurrency limit and origin validation.

### Security

- **CSRF**: Token-based protection with enforce/monitor/off modes.
- **Rate Limit**: In-memory per-IP login attempt limiting with lockout.
- **Heartbeat**: Session refresh and token rotation every configured interval.
- **Cookies**: HttpOnly session cookie, SameSite=Strict, Secure controlled by `FORCE_HTTPS`.

### Commands

```bash
# Backend
cd backend && make lint && make test && make build

# Strict backend gate
../../scripts/ci/admin-dashboard-go-ci.sh

# Frontend
cd frontend && npm run lint && npm run build
```

### Architecture Validation

```bash
./scripts/architecture/check-admin-dashboard-go-only.sh
./scripts/ci/admin-dashboard-go-ci.sh
```
