# OpenAPI Pipeline

현재 admin-dashboard는 Rust backend의 `utoipa` 스키마를 SSOT로 사용합니다. 프런트 generated client는 이 스키마에서만 생성합니다.

## 규칙

- `backend/src/openapi.rs`가 export 대상 경로와 schema를 소유합니다.
- `backend/src/bin/export-openapi.rs`가 OpenAPI JSON을 stdout으로 내보냅니다.
- `frontend/src/api/generated/*`는 generated output이므로 수동 수정하지 않습니다.
- `frontend/src/api/adminClient.ts`가 generated `Admin` singleton을 소유하고, `core.ts`와 `holoClient.ts`는 이 인스턴스만 사용합니다.
- `/admin/api/holo/*`는 backend가 소유하는 typed contract를 먼저 추가하고, wildcard proxy는 websocket/compatibility fallback으로만 남깁니다.

## 생성 명령

```bash
cd admin-dashboard/frontend
mkdir -p ../backend/docs
(cd ../backend && cargo run --quiet --bin export-openapi > docs/swagger.json)
npm exec -- swagger-typescript-api generate -p ../backend/docs/swagger.json -o src/api/generated --axios --modular
```

`npm run generate:api`는 위 명령을 래핑합니다.

## 검증 순서

```bash
cd admin-dashboard/backend
cargo test

cd ../frontend
npm run generate:api
npm test
git diff --exit-code -- ../backend/docs/swagger.json src/api/generated
npm run build
```

## 변경 시 체크리스트

1. backend handler와 `openapi.rs`를 같이 수정합니다.
2. `npm run generate:api`로 generated client를 갱신합니다.
3. 생성된 `Admin` client는 `src/api/adminClient.ts` singleton 하나로만 연결합니다.
4. generated wrapper는 `src/api/core.ts`, `src/api/holoClient.ts`에서만 얇게 감쌉니다.
5. `git diff --exit-code -- ../backend/docs/swagger.json src/api/generated`로 drift가 없는지 확인합니다.
6. 테스트와 빌드를 다시 돌려 contract drift가 없는지 확인합니다.
