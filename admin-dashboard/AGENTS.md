INHERIT: ../AGENTS.md

# Admin Dashboard

<!-- L0: inherited from root AGENTS.md — language=Korean, security, proto protection, persona -->
<!-- Project-specific: Production secrets must use `api-vault` only -->

## L1: Context

| Item | Value |
|------|-------|
| **Service** | Unified admin dashboard (Rust backend + React frontend) |
| **Entrypoints** | Backend: `backend/src/main.rs`, Frontend: `frontend/src/main.tsx` |
| **Ports** | :30190 (combined) |

### Tech Stack

| Layer | Technology |
|-------|------------|
| **Frontend** | React 19, TypeScript, Vite 7, TailwindCSS 4, shadcn/ui |
| **Backend** | Rust 1.94, axum, tokio, tower-http |
| **State** | TanStack Query v5 (server), Zustand (client) |
| **Auth** | Session cookie (HMAC signed), heartbeat rotation |

### Key Files

| Task | Location |
|------|----------|
| Route assembly | `backend/src/routes.rs` |
| Auth middleware | `backend/src/auth/middleware.rs` |
| Auth handlers | `backend/src/handlers/auth.rs` |
| Docker control | `backend/src/docker/` |
| Config | `backend/src/config.rs` |
| API client | `frontend/src/api/client.ts` |
| Generated types | `frontend/src/api/generated/` |

### Architecture Validation
- Validate structure using baseline rules:
  - `tree -L 3` (project root)
  - `tree -L 4 admin-dashboard`

## L2: Standards

### Architecture Patterns
- **Proxy Mode**: Authenticate and relay upstream bot service APIs
- **WebSocket**: Real-time system stats and Docker log streaming

### Security
- **CSRF**: Token-based protection (3-state: enforce/monitor/off)
- **Rate Limit**: In-memory per-IP login attempt limiting
- **Heartbeat**: Extend sessions and rotate tokens every 15 minutes

### Commands
```bash
# Backend
cd backend && make lint && make test && make build

# Frontend
cd frontend && npm run lint && npm run build

# Regenerate API types
cd frontend && npm run generate:api
```

### Delegation Runtime
- Assume active parent and subagent work may legitimately take time and that sufficient computing power is available for their scoped work.
- Do not treat elapsed time alone as a reason to recall, restart, close, or abandon a running subagent or workstream.
- Treat subagent wait timeouts, empty wait results, and delayed completion messages as `running`, not terminal.
- Prefer longer waits or useful parallel parent work over recall churn while a needed subagent is still running.

### Conventions (Frontend)
- **Components**: `src/components/{feature}/` split by feature
- **Hooks**: `src/hooks/` custom hooks
- **Stores**: `src/stores/` Zustand state management

> **See Also**: Root [AGENTS.md](../AGENTS.md) for global rules.
