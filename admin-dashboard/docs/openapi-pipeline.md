# OpenAPI Pipeline

관리자 계약의 유일한 정본은 `backend/internal/contract/openapi.json`입니다. Go embed가 runtime OpenAPI를 제공하고, `frontend/scripts/generate-api.mjs`가 같은 정본에서 SDK·standalone validator·operation 접근 목록을 직접 생성합니다.

```text
backend/internal/contract/openapi.json
 ├─ Go embed → runtime OpenAPI
 ├─ operations_generated.go → route/access parity
 └─ frontend/scripts/generate-api.mjs
     ├─ src/api/generated/Admin.ts, data-contracts.ts, http-client.ts
     ├─ schema-bundle.json → validation.ts, validators/*.mjs, validators/*.d.mts
     └─ operation-contracts.json, generation.ts, provenance.json
```

## 생성과 검증

`hololive-bot/`에서 실행합니다.

```bash
(cd admin-dashboard/frontend && corepack npm run generate:api)
(cd admin-dashboard/frontend && corepack npm run test:contract)
```

생성 SDK는 template가 선언하는 `HttpClient`를 생성 시 주입받습니다. 생성 후 Axios instance를 재할당하거나 생성기가 별도 네트워크 client를 만들지 않습니다. 세대·CSRF·`X-Admin-Mutation-ID` header는 transport가 소유합니다. 생성 파일은 손으로 수정하지 않습니다.

업무 변경의 mutation ID는 논리 제출마다 한 번 정하고 BFF가 세션 family 안에서 원자적으로 선점합니다. 브라우저가 응답 유실 후 같은 HTTP를 재전송해도 실제 upstream 효과는 최대 한 번입니다. 오류의 `notDispatchedMutationId`는 최초 선점 후 upstream 전송 전 거부에만 붙이며, 원래 요청 ID와 일치할 때만 효과 부재의 근거가 됩니다. HTTP 거부 코드만으로 앞선 시도의 효과를 부정하지 않습니다. [재현과 계약 근거](bigbang/mutation-replay-evidence.md).

JSON Schema는 2020-12이며 operation의 path/query/header/body와 응답 status/content-type/header/body를 모두 연결합니다. OpenAPI `example` annotation은 schema bundle의 `examples`로 옮기며 검증 의미를 바꾸지 않습니다. 외부 ref나 미지원 형식은 생성 실패입니다. 미분류 접근과 같은 operationId의 중복도 실패합니다.

Ajv `coerceTypes`, `useDefaults`, `removeAdditional`은 모두 false입니다. 생성 ESM의 CJS helper 참조는 bundler가 처리하여 최종 ESM에 포함합니다. `provenance.json`과 구문 검사는 runtime에 Ajv compiler·동적 코드 생성·미해결 require가 들어오면 실패합니다. `test:contract`는 동적 코드 생성을 금지한 Node에서 생성 validator를 실행하고 수동 Go DTO와 재생성의 byte 일치를 검사합니다. [Ajv standalone 문서](https://ajv.js.org/standalone.html).

C07의 초기 JS 실측 초과에 따라 operation 검증기는 기능별 module로 생성합니다. transport는 해당 입력과 모든 응답 검증기를 전송 전에 함께 준비하고, 준비 실패·취소·세션 변경이면 HTTP를 보내지 않습니다. 준비 시간과 HTTP는 기존 30초 예산을 공유합니다. 응답 뒤 새 검증 코드를 가져오거나 재시도하지 않습니다. WS의 `system-stats` 검증기는 동기 import이며 모델 전체 검증 module은 계약 시험에서만 소비합니다.

`generation.ts`와 Go `Generation`은 같은 정본 내용의 SHA-256을 사용합니다. runtime version과 session signing secret은 별개이며 secret은 생성 입력이나 산출물에 포함하지 않습니다.

## 출시 증거의 범위

contract unit test 통과는 브라우저 CSP·기능·전체 candidate 성능·전환/복구 완료를 뜻하지 않습니다. backend adapter와 frontend mapper의 실제 소비 경로, old/new bundle의 양방향 세대 차단, 전체 route 등록과 Docker/CI 입력은 실행 단계별로 추가 검증합니다. 상태는 `PLN-20260909-hololive-admin-bigbang-replacement`에 기록합니다.
