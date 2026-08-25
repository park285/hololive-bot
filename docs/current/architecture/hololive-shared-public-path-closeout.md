# hololive-shared Public Path Closeout

## Decision boundary

`DEC-20260825-hololive-shared-public-path-scoped-retention`에 따라 public path closeout은 `hololive-shared/pkg` 디렉터리 전체 삭제가 아닙니다. 단일 runtime 실행 구현은 module `internal/`로 이동하고, cross-runtime 계약·진성 다중 소비자·producer/consumer 양측 계약면만 shared에 남기는 것이 완료 조건입니다.

## Current state

- YouTube outbox dispatcher는 `hololive-alarm-worker/internal/egress/youtubedispatch`가 소유합니다.
- poller 구현은 `hololive-youtube-collector/internal/runtime/pollers`가 소유합니다.
- `service/delivery`는 reactive reply와 proactive egress가 함께 소비하므로 shared에 남습니다.
- YouTube outbox store/format/deliverysql/dispatchstate와 tracking/observation은 producer와 consumer 또는 shared 내부 다중-runtime 그래프가 함께 사용하므로 자동 이동·삭제하지 않습니다.
- alarm HTTP provider의 target owner는 alarm-worker이며, hololive-api 등록은 명시된 caller cutover 조건을 가진 compatibility facade입니다.
- `repository-ownership.allowlist`는 table owner/writer/reader 정본이고 import graph로 재해석하지 않습니다.

## Validation

2026-08-25 최종 tree에서 다음 검증을 수행했습니다.

```bash
bash scripts/architecture/check-repository-ownership.sh
bash scripts/architecture/ci-notification-egress-gate.sh
bash scripts/architecture/check-project-map.sh
bash scripts/architecture/check-contract-map.sh
bash scripts/architecture/check-runbook-coverage.sh
```

다섯 명령과 수정한 shell script의 `bash -n`, 관련 경로의 `git diff --check`가 모두 통과했습니다. 관련 current 문서와 gate의 역할 구분이 일치하며, 이 변경은 runtime, schema, queue, retry, fallback, deploy 또는 production data를 변경하지 않습니다.
