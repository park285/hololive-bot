# Repo Structure Refactor Worklog

## Purpose

이 문서는 2026-05-15 기준 repository structure refactor의 실제 완료 범위, 검증 증거, 남은 작업 순서를 기록한다. 현재 운영 SSOT가 아니라, 다음 리팩터 작업을 이어가기 위한 design/worklog 문서다.

## Current Baseline

현재 기준 커밋:

- `ca7a3533 docs(architecture): define repo tree policy`
- `032159c4 fix(shared): preserve original youtube publish timestamps`

현재 `go.work` module 경계와 Docker Compose runtime 경계는 유지한다. 이번 단계에서는 Go module path, runtime binary path, Docker build target을 바꾸지 않았다.

## Completed Slice 1: Repo Tree Governance

완료한 일:

- `docs/current/architecture/repo-tree-policy.md` 추가
- `docs/current/architecture/README.md`에서 tree policy 연결
- `docs/design/repo-tree-classification.md` 추가
- `docs/README.md`에서 docs classification 문서 연결
- `scripts/architecture/check-tracked-local-artifacts.sh` 강화

정책으로 고정한 내용:

- root는 repository entrypoint와 workspace-level contract 중심으로 유지한다.
- `docs/current/`, `docs/history/`, `docs/design/`, `docs/superpowers/specs/`, `docs/superpowers/plans/` 역할을 구분한다.
- `logs/`, `data/`, `backups/`, `.review-bundles/`, local `.env*`, key/pem/archive, non-example `runtime-config/` 파일은 tracked local artifact로 막는다.
- `artifacts/architecture/go-workspace-import-graph.txt`, `logs/.gitkeep`, `runtime-config/.gitkeep`, `runtime-config/README.md`, `runtime-config/*.example` 예외는 유지한다.

검증:

```bash
bash -n scripts/architecture/check-tracked-local-artifacts.sh
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-current-docs-no-historical-body.sh
./scripts/architecture/check-doc-links-no-local-paths.sh
./scripts/architecture/check-tracked-local-artifacts.sh
./scripts/architecture/ci-boundary-gate.sh
```

추가 negative test:

- 임시 `GIT_INDEX_FILE`로 `.env`, `.env.local`, `.env.*.local`, `logs/...`, `runtime-config/secret.env`를 강제 stage했다.
- `check-tracked-local-artifacts.sh`가 해당 파일들을 실패 처리하는 것을 확인했다.
- 임시 파일은 제거했다.

## Completed Slice 2: SHARED Timestamp Preservation

완료한 일:

- `hololive/hololive-shared/pkg/service/youtube/poller/repository_batch_writes.go`
  - existing `published_at`이 있으면 later scrape value로 덮어쓰지 않도록 변경
- `hololive/hololive-shared/pkg/service/youtube/tracking/*`
  - existing `actual_published_at`이 있으면 later value로 덮어쓰지 않도록 변경
- 회귀 테스트 추가
  - video upsert published_at 보존
  - community post upsert published_at 보존
  - content tracking actual_published_at 보존
  - alarm state actual_published_at 보존
  - source posts actual_published_at 보존

검증:

```bash
go test ./hololive/hololive-shared/pkg/service/youtube/poller ./hololive/hololive-shared/pkg/service/youtube/tracking
go test ./hololive/hololive-shared/...
```

## Not Completed Yet

전체 구조 리팩토링은 아직 끝나지 않았다. 남은 큰 축은 다음과 같다.

1. `hololive-stream-ingester` 내부 구조 cleanup
   - runtime assembly
   - YouTube control-plane
   - ops/reporting package 분리
   - oversized ops/report files 분해

2. `hololive-shared` slimming
   - app/runtime assembly 성격이 강한 shared package 축소
   - shared domain/contracts/infra helper와 runtime-owned composition 경계 분리
   - 현재 touched YouTube repository files 주변부터 작은 단위로 진행

3. `hololive-llm-sched` scheduler/runtime 구조 정리
   - scheduler lifecycle 중복 제거
   - `internal/schedulerkit` 기준이 아직 필요한지 현재 코드 기준 재검토

4. bot command assembly 단순화
   - factory/registry/deps/router 책임 중복 재검토
   - public command behavior 유지

5. docs plan-kit 정리
   - `docs/holobot-*`, `docs/hololive-*` plan-kit directory를 한 번에 이동하지 않는다.
   - active reference와 link를 먼저 확인하고 directory family 하나씩 이동한다.

## Recommended Next Step

다음 작업은 `hololive-stream-ingester` 또는 `hololive-shared` 중 하나만 골라 별도 plan으로 진행한다. 현재 커밋 기준으로는 `hololive-shared`가 깨끗해졌으므로, 다음 선택지는 두 가지다.

- 낮은 위험: `docs/` plan-kit classification을 실제 move plan으로 확장한다.
- 높은 효과: `hololive-stream-ingester` 구조 cleanup plan을 최신 코드 기준으로 재검토하고 첫 package move slice를 실행한다.

각 slice는 다음 원칙을 지킨다.

- 한 커밋은 한 ownership boundary만 바꾼다.
- file move와 behavior change를 섞지 않는다.
- target package test를 먼저 돌리고, 필요하면 module-level test로 확장한다.
- production deploy/restart는 별도 명시 요청 없이는 하지 않는다.
