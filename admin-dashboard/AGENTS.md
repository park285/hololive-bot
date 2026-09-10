# Admin Dashboard guidance

Read `../AGENTS.md` for Hololive rules. This dashboard uses a Go backend and React frontend on combined port `30190`. Versions belong to `backend/go.mod`, `frontend/package.json`, and their lockfiles.

## Ownership

Backend: Gin/net/http, Valkey sessions, gorilla/websocket. Frontend: React, TypeScript, Vite, TailwindCSS, shadcn/ui, TanStack Query v5 for server state, and Zustand for client state.

| Task | Owner |
|---|---|
| Entrypoints | `backend/cmd/admin-dashboard/main.go`, `frontend/src/main.tsx` |
| Route assembly | `backend/internal/httpapi/routes.go`, `backend/internal/httpapi/access.go` |
| Auth and sessions | `backend/internal/auth/`, `backend/internal/session/` |
| Docker control | `backend/internal/adapters/docker/`, `backend/internal/contract/docker-policy.json` |
| Config | `backend/internal/config/` |
| Holo API adapter | `backend/internal/adapters/holo/` |
| Contract and generated client | `backend/internal/contract/`, `frontend/scripts/generate-api.mjs` |
| Frontend composition and transport | `frontend/src/app/bootstrap.ts`, `frontend/src/api/client.ts`, `frontend/src/api/transport.ts` |
| Frontend session and server state | `frontend/src/session/`, `frontend/src/queries/` |
| Business mutations and drafts | `frontend/src/operations/`, `frontend/src/editors/`, `frontend/src/features/` |

## Contracts

- Authenticate at the dashboard before proxying to Hololive Admin API with `X-API-Key` injection.
- Backend is Go-only. `scripts/architecture/check-admin-dashboard-go-only.sh` rejects backend Rust files/manifests/lockfiles and Rust tooling in the backend or `admin-dashboard/Dockerfile`.
- Preserve WebSocket concurrency limits/origin validation, per-IP login rate limits/lockout, HMAC-signed session cookies, CSRF token protection (`enforce/monitor/off`), and configured heartbeat refresh/token rotation.
- Session cookies remain HttpOnly and SameSite=Strict; `FORCE_HTTPS` controls Secure.

## Verification

Run relevant checks from the Hololive root; docs-only edits normally need diff inspection. Required publication gates still apply.

```bash
./scripts/ci/admin-dashboard-go-ci.sh
(cd admin-dashboard/frontend && corepack npm run lint && corepack npm run build)
./scripts/architecture/check-admin-contract.sh
```

Full backend validation uses `./scripts/ci/admin-dashboard-go-ci.sh`, including build/tests/lint and the Go-only check; do not repeat covered checks for unchanged inputs. Run `./scripts/architecture/check-admin-dashboard-go-only.sh` when backend language/tooling, Dockerfile, or Go workspace/CI module membership changes, unless already covered by the full gate.
