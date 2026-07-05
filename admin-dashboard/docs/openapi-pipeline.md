# OpenAPI Pipeline

admin-dashboard의 OpenAPI SSOT는 **손으로 관리하는** `backend/internal/openapi/spec.json`입니다. 이 파일이 embed되어 런타임 노출과 프런트 generated client의 단일 출처가 됩니다.

## 흐름

```
backend/internal/openapi/spec.json   (SSOT, 수동 편집)
        │  go:embed
        ▼
backend/cmd/export-openapi           (spec을 stdout으로 export)
        │  go run ./cmd/export-openapi
        ▼
backend/docs/swagger.json            (미러; generated client 입력)
        │  swagger-typescript-api
        ▼
frontend/src/api/generated/*         (generated output — 수동 수정 금지)
```

## 규칙

- `backend/internal/openapi/spec.json`이 유일한 SSOT입니다. path/schema 변경은 이 파일을 직접 편집합니다.
- `backend/internal/openapi/spec.go`가 `spec.json`을 `go:embed`로 담고 `version`만 주입합니다.
- `backend/cmd/export-openapi`가 spec을 stdout으로 내보냅니다.
- `backend/docs/swagger.json`은 `spec.json`의 미러이므로 직접 편집하지 않고 export로만 갱신합니다.
- `frontend/src/api/generated/*`(`Admin.ts`, `data-contracts.ts`, `http-client.ts`)는 generated output이므로 수동 수정하지 않습니다.
- runtime 노출은 build-time export와 분리됩니다. `EnableOpenAPI`일 때 `GET /admin/api/openapi.json`, `EnableSwaggerUI`일 때 `GET /admin/docs`(인증 뒤)로만 열립니다.

## 생성 명령

프런트 `package.json`의 `generate:api` 스크립트가 export와 client 생성을 한 번에 수행합니다.

```bash
cd admin-dashboard/frontend
npm run generate:api
```

`generate:api`가 실제로 실행하는 것:

```bash
mkdir -p ../backend/docs \
  && (cd ../backend && go run ./cmd/export-openapi > docs/swagger.json) \
  && swagger-typescript-api generate -p ../backend/docs/swagger.json -o src/api/generated --axios --modular
```

## 검증 순서

```bash
# 1. 미러가 SSOT와 일치하는지 확인 (backend/ 기준)
cd admin-dashboard/backend
go run ./cmd/export-openapi | diff - docs/swagger.json

# 2. generated client 재생성 후 drift 확인 (frontend/ 기준)
cd ../frontend
npm run generate:api
git diff --exit-code -- ../backend/docs/swagger.json src/api/generated

# 3. 빌드/테스트
npm run build
npm test
```

## 변경 시 체크리스트

1. handler나 schema를 바꾸면 `backend/internal/openapi/spec.json`을 같이 수정합니다.
2. `go run ./cmd/export-openapi | diff - docs/swagger.json`로 미러 drift가 없는지 확인하고, 필요하면 미러를 다시 export합니다.
3. 프런트가 새 계약을 써야 하면 `npm run generate:api`로 `swagger.json` 미러와 generated client를 재생성합니다.
4. `git diff --exit-code -- ../backend/docs/swagger.json src/api/generated`로 drift가 없는지 확인합니다.
5. backend `make test`와 프런트 `npm run build`를 다시 돌려 contract drift가 없는지 확인합니다.
