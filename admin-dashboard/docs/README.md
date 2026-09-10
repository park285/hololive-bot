# Admin Dashboard Docs

관리자 UI와 Go BFF는 같은 contract generation으로 빌드하고 함께 전환합니다. 현재 작업과 출시 여부는 `DEC-20260909-hololive-admin-bigbang-replacement` 및 연결된 PLN이 소유합니다.

## 현재 구조

| 경계 | Owner |
|---|---|
| 실행과 자원 수명 | `backend/cmd/admin-dashboard/`, `backend/internal/bootstrap/` |
| HTTP·인증·CSRF·generation·업무 admission·WS | `backend/internal/httpapi/` |
| 명세·모델·접근 목록·Docker 정책 | `backend/internal/contract/` |
| Holo·Docker 외부 I/O | `backend/internal/adapters/holo/`, `backend/internal/adapters/docker/` |
| 세션·family·rotation·mutation 선점 | `backend/internal/session/` |
| 상태 집계·샘플·구독 | `backend/internal/observations/` |
| 설정 | `backend/internal/config/load.go`, `secrets.go`, `validate.go`; `config.go`는 타입과 기본값 |
| 기존 인증·정적 파일 기능 | `backend/internal/auth/`, `backend/internal/httpx/`, `backend/internal/static/` |
| 앱 조립 | `frontend/src/main.tsx`, `app/bootstrap.ts`, `App.tsx` |
| 주입된 SDK·transport·오류 | `frontend/src/api/` |
| 인증과 정책·CSRF의 단일 상태 | `frontend/src/session/` |
| 조회·업무 변경·편집 초안 | `frontend/src/queries/`, `operations/`, `editors/` |
| 전체 메뉴·Docker | `frontend/src/features/`; Docker는 설정 메뉴 안에서 제공 |

공유 포트는 `30190`이고 Vite 개발 프록시는 `http://localhost:30190`의 `/admin/meta.json`과 `/admin/api`를 사용합니다. 버전 정본은 `backend/go.mod`, `frontend/package.json`과 lockfile입니다.

## 계약과 문서

- [OpenAPI pipeline](openapi-pipeline.md): `backend/internal/contract/openapi.json` → Go embed·접근 목록·SDK·standalone validator.
- [세션과 세대](bigbang/session-generation.md), [재전송의 효과·불확실성](bigbang/mutation-replay-evidence.md).
- [범위 manifest](bigbang/endpoint-feature-parity.json), [기능별 현재 소비 경로](bigbang/candidate-parity.json), [제거 대상](bigbang/retirement.json).
- [운영 runbook](../../docs/current/runbooks/admin-dashboard.md): secret 파일·로컬 빌드·원격 no-build·첫 전환·전체 복구.

`SESSION_PREWARNING_BACKEND_CONTRACT.md`, `FRONTEND_IMPROVEMENTS.md`, `BOT_ONLY_MIGRATION_STATUS_20260309.md`는 과거 구현·인계 기록입니다. 현재 실행 경로나 완료 판정의 정본으로 사용하지 않습니다.

## 검증

`hololive-bot/`에서 실행합니다. 브라우저 시험은 세 엔진의 실행 환경이 필요하며 skip은 출시 통과가 아닙니다.

```bash
./scripts/ci/admin-dashboard-go-ci.sh
./scripts/architecture/check-admin-contract.sh
node scripts/architecture/check-admin-feature-parity.mjs
(cd admin-dashboard/frontend && corepack npm run lint && corepack npm run build)
(cd admin-dashboard/frontend && corepack npm test)
```

`corepack npm run generate:api`는 frontend 디렉터리에서 실행합니다. 생성 파일을 손으로 수정하거나 별도 Swagger mirror를 만들지 않습니다. production build의 모듈·파일 hash 목록은 `frontend/node_modules/.cache/admin-build-inventory.json`에 기록하며 공개 assets에 포함하지 않습니다.
