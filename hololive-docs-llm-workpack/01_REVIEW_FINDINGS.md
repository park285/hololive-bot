# 01. 재검토 결과

이 문서는 현재 저장소 문서와 코드 표면을 다시 확인한 뒤 작성한 근거 요약입니다.

## 1. Project Map은 7 runtime 기준

`docs/current/PROJECT_MAP.md`는 현재 runtime binary를 7개로 정리합니다.

- `bot`
- `admin-api`
- `alarm-worker`
- `dispatcher-go`
- `llm-scheduler`
- `stream-ingester`
- `youtube-scraper`

관련 근거 파일:

- `docs/current/PROJECT_MAP.md`
- `go.work`

## 2. 루트 README는 오래된 runtime 기준을 일부 유지

루트 `README.md`는 runtime binary를 5개 중심으로 설명하고, module 수도 현재 Project Map과 다르게 표현합니다.

관련 근거 파일:

- `README.md`
- `docs/current/PROJECT_MAP.md`
- `go.work`

판단:

루트 README는 상세 SSOT가 아니라 gateway 역할로 낮추고, 현재 구조 요약은 `docs/current/PROJECT_MAP.md`와 일치시켜야 합니다.

## 3. current 문서에 historical 성격 문서가 섞여 있음

`docs/current/README.md`는 현재 운영 기준 문서만 둔다고 설명하지만, current 아래에는 완료/이력 문서가 섞여 있습니다.

특히 `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md`는 자체적으로 `CLOSED / HISTORICAL` 상태라고 선언합니다.

관련 근거 파일:

- `docs/current/README.md`
- `docs/current/ALARM_DISPATCH_REMEDIATION_20260414.md`
- `docs/current/RUNTIME_SPLIT_HANDOFF_20260416.md`

판단:

current에는 현재 기준 요약만 남기고, handoff/remediation 원문은 history로 이동해야 합니다.

## 4. 계약 패키지는 있으나 계약 문서가 부족

코드에는 다음 계약 패키지들이 이미 존재합니다.

- `hololive-shared/pkg/contracts/majorevent`
- `hololive-shared/pkg/contracts/membernews`
- `hololive-shared/pkg/contracts/trigger`
- `hololive-shared/pkg/contracts/settings`
- `hololive-shared/pkg/contracts/alarm`

하지만 문서에는 전체 계약 지도인 `CONTRACT_MAP.md`가 없습니다.

관련 근거 파일:

- `hololive/hololive-shared/pkg/contracts/majorevent/routes.go`
- `hololive/hololive-shared/pkg/contracts/membernews/routes.go`
- `hololive/hololive-shared/pkg/contracts/membernews/types.go`
- `hololive/hololive-shared/pkg/contracts/trigger/routes.go`
- `hololive/hololive-shared/pkg/contracts/trigger/errors.go`
- `hololive/hololive-shared/pkg/contracts/settings/contracts.go`
- `hololive/hololive-shared/pkg/contracts/alarm/contracts.go`

판단:

계약 문서는 코드의 contracts 패키지를 따라가야 합니다. 코드에 존재하는 계약이 문서에 없으면, LLM과 운영자가 경계를 잘못 이해합니다.

## 5. Error contract가 약함

현재 server 공통 응답 helper는 `RespondError(c, status, message, extra)` 형태이고, 기본 payload는 `{ "error": message }`입니다.

현재 HTTP client의 `CheckStatus`는 status와 body를 문자열 error로 반환합니다.

관련 근거 파일:

- `hololive/hololive-shared/pkg/server/response.go`
- `shared-go/pkg/httputil/response.go`

판단:

문서상 error code, status mapping, client parsing 원칙을 먼저 고정해야 합니다. 이후 typed error helper를 도입합니다.

## 6. Queue/PubSub 문서화가 부족

Alarm queue는 contract와 version이 있습니다. Consumer도 version을 확인합니다. Settings Pub/Sub도 contract는 있지만 메시지 자체에 version field가 없고 `{type, payload}` 형태입니다.

관련 근거 파일:

- `hololive/hololive-shared/pkg/contracts/alarm/contracts.go`
- `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`
- `hololive/hololive-shared/pkg/contracts/settings/contracts.go`
- `hololive/hololive-shared/pkg/service/configsub/subscriber.go`
- `hololive/hololive-shared/pkg/service/configsub/dispatcher.go`

판단:

Queue/PubSub은 별도 문서로 “현재 shape”, “지원 version”, “DLQ”, “retry”, “유실 가능성”, “명령성 이벤트 처리 원칙”을 기록해야 합니다.

## 7. Architecture gate는 존재하지만 문서 계약을 충분히 강제하지 않음

현재 `ci-boundary-gate.sh`는 여러 architecture gate를 실행합니다. 그중 M1에는 alarm contract sanity check와 trigger route hardcoding check가 있습니다.

관련 근거 파일:

- `.github/workflows/architecture-gates.yml`
- `scripts/architecture/ci-boundary-gate.sh`
- `scripts/architecture/check-go-alarm-contracts.sh`
- `scripts/architecture/check-go-trigger-route-hardcoding.sh`

판단:

문서 작업 후 다음 gate를 추가해야 합니다.

- current 문서에 historical 상태 문서가 남아 있는지 검사
- runtime별 runbook coverage 검사
- contract map coverage 검사
- `/internal/membernews`, `/internal/majorevent`, `/internal/alarm` hardcoding 검사
- error contract 검사

## 8. 작업 결론

지금 필요한 일은 코드 대수술이 아닙니다. 먼저 문서를 다음 네 축으로 단단하게 만들어야 합니다.

1. 현재 운영 기준 문서
2. 서비스 소유권 문서
3. 내부 계약 문서
4. 운영 Runbook과 CI 문서 게이트

이 작업팩의 task들은 이 순서로 쪼개져 있습니다.
