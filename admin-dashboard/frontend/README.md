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
npm run build
```

## 개발 서버

기본 프록시 대상은 `http://localhost:30190`입니다.

```bash
cd admin-dashboard/frontend
ADMIN_DASHBOARD_PROXY_TARGET=http://localhost:30190 npm run dev
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
