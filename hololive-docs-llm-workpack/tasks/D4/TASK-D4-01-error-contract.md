# TASK-D4-01. Error Contract 문서 추가

## Phase

D4. 오류 계약

## 목표

`docs/current/ERROR_CONTRACT.md`를 작성하여 내부 API 오류 응답과 client 해석 규칙을 고정합니다.

## 왜 필요한가

현재 server는 `{error: message}` 응답을 만들고, client는 status/body를 문자열 error로 묶습니다. 문서상 error code와 status mapping이 없으면 안정적 분기가 어렵습니다.

## 먼저 읽을 파일

- `hololive/hololive-shared/pkg/server/response.go`
- `shared-go/pkg/httputil/response.go`
- `hololive/hololive-shared/pkg/contracts/trigger/errors.go`
- `hololive/hololive-shared/pkg/contracts/membernews/types.go`

## 수정 또는 생성할 파일

- `docs/current/ERROR_CONTRACT.md`
- `docs/current/README.md`

## 작업 단계

1. 현재 호환 오류 포맷 `{error: string}`을 문서화합니다.
2. 강화 포맷 `{error, message, request_id, details}`를 목표 포맷으로 제시합니다.
3. HTTP status별 의미와 error code 예시를 표로 작성합니다.
4. client는 error string 전체 parsing을 하지 않는다는 규칙을 적습니다.
5. 계약별 error code는 각 contracts 문서와 연결합니다.
6. current README에 등록합니다.

## 금지 사항

- 이 task에서 `RespondError` 코드를 변경하지 마십시오.
- 이 task에서 `CheckStatus` 코드를 변경하지 마십시오.

## 완료 조건

- ERROR_CONTRACT.md가 생성됩니다.
- 기존 오류 포맷과 목표 오류 포맷이 모두 설명됩니다.
- status mapping 표가 존재합니다.
- 문자열 기반 status parsing 금지 원칙이 있습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D4-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
