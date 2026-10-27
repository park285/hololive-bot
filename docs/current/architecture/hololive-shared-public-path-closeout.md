# hololive-shared Public Path Closeout

## Decision boundary

`DEC-20260825-hololive-shared-public-path-scoped-retention`에 따라 public path closeout은 `hololive-shared/pkg` 디렉터리 전체 삭제가 아닙니다. 단일 runtime 실행 구현은 module `internal/`로 이동하고, cross-runtime 계약·진성 다중 소비자·producer/consumer 양측 계약면만 shared에 남기는 것이 완료 조건입니다.

## Current state

- YouTube outbox dispatcher는 `hololive-alarm-worker/internal/egress/youtubedispatch`가 소유합니다.
- YouTube 수집 scheduler는 `hololive-youtube-collector/internal/runtime/collectorruntime`, provider별 수집기는 `internal/runtime/{holodexcollector,officialcollector,youtubejscollector}`가 소유합니다.
- 범용 notification delivery dispatcher·consumer store·maintenance·SQL은 `hololive-alarm-worker/internal/egress/notificationdelivery`가 소유합니다. shared `pkg/service/delivery`에는 API llm plane producer와 worker consumer가 함께 쓰는 producer enqueue 저장소, 요청·결과·sender interface, Iris transport와 locker만 남습니다.
- runtime 설정 집계는 API `internal/config`(`BotPlaneConfig`·`AdminPlaneConfig`·`APIWorkerProfile`)와 worker `internal/config`(`RuntimeConfig`·`AlarmWorkerProfile`)가 소유하고, shared `pkg/config/settings`에는 설정 구획 타입·loader·순수 검증만 남습니다. shared provider는 `pkg/providers/{database,holodex,iris,modules}` 하위 package만 두며 root `providers` package는 삭제했습니다.
- YouTube outbox `analytics`/`deliverysql`/`telemetry`/`timeline`과 tracking/observation은 producer와 consumer 또는 shared 내부 다중-runtime 그래프가 함께 사용하므로 shared에 남습니다. YouTube store·format과 dispatch 설정·결과(`youtubedispatch`)는 alarm-worker `internal/`이 소유합니다.
- alarm HTTP provider(handler·route registrar·binding DTO)는 alarm-worker `internal/service/alarm/httpapi`로 이관을 마쳤습니다. shared `pkg/service/alarmhttp`에는 hololive-api bot/admin이 쓰는 client만 남습니다.
- `repository-ownership.allowlist`는 table owner/writer/reader 정본이고 import graph로 재해석하지 않습니다.

## Validation

2026-08-25 최종 tree에서 다음 검증을 수행했습니다.

```bash
bash scripts/architecture/check-repository-ownership.sh
bash scripts/architecture/ci-notification-egress-gate.sh
```

다섯 명령과 수정한 shell script의 `bash -n`, 관련 경로의 `git diff --check`가 모두 통과했습니다. 관련 current 문서와 gate의 역할 구분이 일치하며, 이 변경은 runtime, schema, queue, retry, fallback, deploy 또는 production data를 변경하지 않습니다.
