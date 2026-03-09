INHERIT: ../AGENTS.md

# Admin Dashboard

<!-- L0: inherited from root AGENTS.md — language=Korean, security, proto protection, persona -->
<!-- Project-specific: Production secrets must use `api-vault` only -->

## L1: Context

| Item | Value |
|------|-------|
| **Service** | Unified admin dashboard (Go backend + React frontend) |
| **Entrypoints** | Backend: `backend/cmd/admin/main.go`, Frontend: `frontend/src/main.tsx` |
| **Ports** | :30190 (combined) |

### Tech Stack

| Layer | Technology |
|-------|------------|
| **Frontend** | React 19, TypeScript, Vite 7, TailwindCSS 4, shadcn/ui |
| **Backend** | Go 1.26.1, Gin framework, Swagger docs |
| **State** | TanStack Query v5 (server), Zustand (client) |
| **Auth** | Session cookie (HMAC signed), heartbeat rotation |

### Key Files

| Task | Location |
|------|----------|
| API routes | `backend/internal/server/routes.go` |
| Auth logic | `backend/internal/auth/auth.go` |
| Docker control | `backend/internal/docker/` |
| API client | `frontend/src/api/client.ts` |
| Generated types | `frontend/src/api/generated/` |

### Architecture Validation
- Validate structure using baseline rules:
  - `tree -L 3` (project root)
  - `tree -L 4 admin-dashboard`

## L2: Standards

### Architecture Patterns
- **Proxy Mode**: Authenticate and relay upstream bot service APIs
- **SSR Data**: Inject initial data via `window.__SSR_DATA__`
- **WebSocket**: Real-time system stats and Docker log streaming

### Security
- **CSRF**: Token-based protection
- **Rate Limit**: Limit login attempt frequency
- **Heartbeat**: Extend sessions and rotate tokens every 5 minutes

### Commands
```bash
# Backend
cd backend && make lint && make test && make build

# Frontend
cd frontend && npm run lint && npm run build

# Regenerate API types
cd frontend && npm run generate:api
```

### Conventions (Frontend)
- **Components**: `src/components/{feature}/` split by feature
- **Hooks**: `src/hooks/` custom hooks
- **Stores**: `src/stores/` Zustand state management

> **See Also**: Root [AGENTS.md](../AGENTS.md) for global rules.
