# Repo-Wide Refactor Audit

이 문서는 전 레포 리팩터링 후보를 활성 운영 영향 기준으로 분류합니다. 역사 보존용 문서와 `docs/history/**` 아래의 과거 계획은 실행 대상이 아니며, 현재 런타임/배포/CI/문서 진입점만 우선순위에 포함합니다.

## Completed In Current Cleanup

| Area | Finding | Resolution | Verification |
| --- | --- | --- | --- |
| Standalone notification dispatcher | `hololive-dispatcher-go`, `dispatcher-go`, `legacy-dispatcher-go`가 활성 compose, docs, CI, build path에 남아 있을 수 있었음 | standalone dispatcher runtime, compose profile, deploy/log/runbook references removed | `check-removed-runtime-references.sh`, `ci-notification-egress-gate.sh`, active `rg` scan |
| Go workspace module lists | CI/build commands carried repeated module package lists | `scripts/ci/go-workspace-modules.sh` owns active Go module package expansion | `go test` and architecture gate use the helper |
| Compose service aliases | `build-all.sh`, `compose-redeploy-service.sh`, and logs tooling had separate alias maps | `scripts/deploy/lib/compose-services.sh` owns build, redeploy, and log target resolution | `scripts/deploy/test-compose-services.sh` in architecture gate |
| OpenBao compose env source | build, redeploy, and direct compose could diverge on env resolution | `scripts/deploy/lib/compose-env.sh` and `scripts/deploy/compose.sh` enforce one env policy | `scripts/deploy/test-compose-env.sh` in architecture gate |
| Current docs root drift | `docs/current/` mixed core SSOT files with runbooks, architecture guidance, review bundle policy, and historical YouTube records | moved operational YouTube docs to `docs/current/runbooks/`, governance docs to `docs/current/architecture/`, historical YouTube records to `docs/history/youtube/`, and added a root allowlist gate | `check-current-docs-root-allowlist.sh`, `check-current-docs-no-historical-body.sh`, active `rg` scan |
| Flat Go helper packages | report Markdown helpers and YouTube producer helper implementations were embedded in broad root packages | moved community/shorts Markdown helpers to `internal/ops/communityshorts/internal/markdown`, `ytInitialData` parsing to `scraper/internal/initialdata`, and browser snapshot HTTP client implementation to `scraper/internal/browserfetcher` while preserving root package facades | targeted package tests for `communityshorts`, `scraper`, and extracted internal packages |
| Repo-wide Go package tree depth | Large public/internal roots exposed implementation files directly, and initial cleanup used generic `core`/`servicecore` buckets | preserved public import paths with root packages and moved implementations under semantic internal packages such as `youtube/outbox/internal/delivery`, `youtube/poller/internal/polling`, `youtube/scraper/internal/scraping`, `config/internal/settings`, `server/internal/httpserver`, Kakao `app/internal/botruntime`, and youtube producer `runtime/internal/producerruntime`; `domain` now owns its model types directly | targeted package tests, `check-go-generic-internal-package-names.sh`, active `rg` scan |

## Active Findings

| Priority | Area | Finding | Action |
| --- | --- | --- | --- |
| High | Runtime ownership docs | `docs/current/services/*.md` and several runbooks still mark readiness/metrics as `검토 필요` | Keep as explicit unknowns until live endpoints and metric names are verified; do not invent values from old dispatcher docs |
| Medium | Shell operational helpers | Compose wrapper is enforced in docs, but operators can still run raw `docker compose` manually | Keep wrapper as documented entrypoint; add future host-level shell alias/policy only with operator approval |
| Medium | Dockerfile/build contexts | Service Dockerfiles still duplicate workspace context patterns | Refactor only after build cache behavior is measured; changing Docker contexts can invalidate production cache unexpectedly |
| Medium | Large semantic implementation leaves | Some implementation leaves remain intentionally cohesive behind root facades, for example `youtube/poller/internal/polling` and `communityshorts/internal/reports`; `youtube/outbox/internal/delivery` has since been split into a root facade over `dispatch`/`store`/`dispatchstate` (+ leaf) sub-packages | Split another level only when a behavior family has a clear package contract and package-local tests; generic `core`/`servicecore` buckets are blocked by CI |
| Medium | Large test files | Several Go test files exceed normal review size | Split only with package-specific behavior tests open; mechanical test splitting has low value without failing cases |
| Low | Historical dispatcher references | `docs/history/**` and old plan kits mention removed dispatcher modules | Preserve as historical evidence; active gates exclude historical archives |

## Guardrails

- New service aliases must be added through `scripts/deploy/lib/compose-services.sh`.
- Compose env policy changes must be added through `scripts/deploy/lib/compose-env.sh`.
- Removed runtime names must remain blocked by architecture gates before merge.
- Go implementation subpackages must use role-specific names. `internal/core`, `servicecore`, and `import core "..."` are blocked by `check-go-generic-internal-package-names.sh`.
- Refactor changes must keep production deploy commands on repository scripts, not raw `docker compose`.
