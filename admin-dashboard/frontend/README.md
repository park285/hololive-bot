# Admin Dashboard Frontend

`admin-dashboard` 프런트엔드입니다. React 19, TypeScript, Vite 8, TanStack Query, Zustand를 사용합니다.

## 핵심 포인트

- 인증 상태는 브라우저 저장값이 아니라 서버 세션으로 부트스트랩합니다.
- `/admin/api/*` 호출은 공용 Axios 인스턴스를 통해 CSRF/401 처리와 함께 동작합니다.
- generated client는 Rust backend OpenAPI에서 재생성합니다.
- Docker 제어와 상태 집계는 Rust backend 계약을 기준으로 동작합니다.

## 주요 명령

```bash
cd admin-dashboard/frontend
npm ci
npm run generate:api
npm run lint
npm run typecheck
npm run build
```

## 현재 툴체인 메모

- TypeScript는 `6.0.2` 기준으로 맞춰져 있습니다.
- `tsconfig.app.json`은 `baseUrl` 없이 `paths`만 사용하도록 정리되어, TS 6/7 deprecation 경고를 피합니다.
- ESLint는 `9.x` 안정판 조합을 사용합니다.
- `eslint-plugin-react-hooks`는 안정판 `7.0.1`을 사용합니다.
- `follow-redirects`는 axios transitive advisory 대응을 위해 `overrides`로 상향 고정되어 있습니다.
- `@tanstack/react-query-devtools`는 개발 환경에서만 lazy-load 됩니다.
- `msw`는 opt-in 개발 mocking 용도로 셋업되어 있으며, `VITE_ENABLE_MSW=true`일 때만 worker가 시작됩니다.
- 긴 리스트는 `@tanstack/react-virtual` 기반 `VirtualList`로 점진적으로 가상화됩니다.

## 개발 서버

기본 프록시 대상은 `http://localhost:30190`입니다.

```bash
cd admin-dashboard/frontend
ADMIN_DASHBOARD_PROXY_TARGET=http://localhost:30190 npm run dev
```

```bash
cd admin-dashboard/frontend
VITE_ENABLE_MSW=true npm run dev
```

## 주요 디렉터리

```text
src/
├── api/         # 공용 client + generated wrapper
├── components/  # 대시보드/Docker/설정 UI
├── hooks/       # auth bootstrap, heartbeat, websocket, SSR helpers
├── layouts/     # App shell
├── lib/         # toast, query client, 공용 유틸
├── pages/       # login
├── routes/      # lazy route definitions
└── stores/      # auth store
```

## 생성 파일

- `src/api/generated/Admin.ts`
- `src/api/generated/data-contracts.ts`
- `src/api/generated/http-client.ts`

이 파일들은 `npm run generate:api`로 갱신합니다.
