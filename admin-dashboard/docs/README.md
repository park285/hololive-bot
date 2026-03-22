# Admin Dashboard Docs

이 디렉터리는 `admin-dashboard` 전용 문서를 둡니다.

## 현재 문서

- `README.md`: 문서 인덱스
- `BOT_ONLY_MIGRATION_STATUS_20260309.md`: 과거 상태 기록
- `FRONTEND_IMPROVEMENTS.md`: 프런트 개선 메모
- `openapi-pipeline.md`: OpenAPI / generated client 흐름

## 현재 기준

- 백엔드는 Rust입니다.
- 프런트 generated client는 Rust backend OpenAPI에서 생성합니다.
- 프런트 개발 프록시 기본 대상은 `http://localhost:30190`입니다.

## 권장 검증 명령

```bash
cd admin-dashboard/backend
cargo fmt --check
cargo clippy -- -D warnings
cargo test

cd ../frontend
npm ci
npm run generate:api
npm run lint
npm run build
```
