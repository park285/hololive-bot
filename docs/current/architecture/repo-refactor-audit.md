# Repo-Wide Refactor Audit

이 문서는 전 레포 리팩터링 후보를 활성 운영 영향 기준으로 분류합니다. 역사 보존용 문서와 `docs/history/**` 아래의 과거 계획은 실행 대상이 아니며, 현재 런타임/배포/CI/문서 진입점만 우선순위에 포함합니다.

## Completed In Current Cleanup

| Area | Finding | Resolution | Verification |
| --- | --- | --- | --- |
| 퇴역 dispatcher | 독립 dispatcher와 옛 운영 연결 | 코드·배포·문서 연결 제거 | 현행 발송 동작 시험 |
| Go workspace module lists | CI/build commands carried repeated module package lists | `scripts/ci/go-workspace-modules.sh` owns active Go module package expansion | `go test` and architecture gate use the helper |
| Compose service aliases | `build-all.sh`, `compose-redeploy-service.sh`, and logs tooling had separate alias maps | `build-all.sh`는 Dockerfile 목록에 있는 서비스 이름만 build target으로 받음. `scripts/deploy/lib/compose-services.sh`는 로그 대상 이름 해석만 소유 | `scripts/build/build-all_test.sh` in pre-push gate, `scripts/deploy/test-compose-services.sh` in architecture gate |
| Compose env source | build, redeploy, and direct compose could diverge on env resolution | `scripts/deploy/lib/compose-env.sh`가 host `compose.env` 형식 검사와 값 읽기를 소유하고 `scripts/deploy/sync-opt-current.sh`가 설치 전에 사용. Compose wrapper `scripts/deploy/compose.sh`는 #747에서 삭제 | `scripts/deploy/test-compose-env.sh` in architecture gate |
| Current docs root drift | `docs/current/` mixed core SSOT files with runbooks, architecture guidance, review bundle policy, and historical YouTube records | moved operational YouTube docs to `docs/current/runbooks/`, governance docs to `docs/current/architecture/`, historical YouTube records to `docs/history/youtube/` | active `rg` scan |
| Flat Go helper packages | report Markdown helpers and YouTube producer helper implementations were embedded in broad root packages | split into semantic internal packages behind root facades; the YouTube producer and ops CLI modules that owned them were later retired and deleted | targeted package tests at the time |
| Repo-wide Go package tree depth | Large public/internal roots exposed implementation files directly, and initial cleanup used generic `core`/`servicecore` buckets | preserved public import paths with root packages and moved implementations under semantic internal packages; `domain` now owns its model types directly. Later runtime splits replaced those trees with module-owned packages such as collector `internal/runtime/*` and alarm-worker `internal/egress/*` | targeted package tests, active `rg` scan |

## Active Findings

| Priority | Area | Finding | Action |
| --- | --- | --- | --- |
| High | Runtime ownership docs | `docs/current/services/*.md` and several runbooks still mark readiness/metrics as `검토 필요` | Keep as explicit unknowns until live endpoints and metric names are verified; do not invent values from old dispatcher docs |
| Medium | Shell operational helpers | 중앙 배포 helper(`scripts/deploy/sync-opt-current.sh`, `scripts/deploy/quadlet-redeploy-service.sh`)가 문서화된 진입점이지만 운영자가 `podman`을 직접 실행할 수 있음 | helper를 문서화된 진입점으로 유지. host 수준 shell alias/policy는 운영자 승인이 있을 때만 추가 |
| Medium | Dockerfile/build contexts | Service Dockerfiles still duplicate workspace context patterns | Refactor only after build cache behavior is measured; changing Docker contexts can invalidate production cache unexpectedly |
| Medium | Large semantic implementation leaves | Some implementation leaves remain intentionally cohesive behind root facades | Split another level only when a behavior family has a clear package contract and package-local tests |
| Medium | Large test files | Several Go test files exceed normal review size | Split only with package-specific behavior tests open; mechanical test splitting has low value without failing cases |
| Low | Historical dispatcher references | `docs/history/**` and old plan kits mention removed dispatcher modules | Preserve as historical evidence; active gates exclude historical archives |

## Guardrails

- 로그 대상 이름은 `scripts/deploy/lib/compose-services.sh`에서만 해석합니다. build target 별칭은 두지 않습니다.
- Compose env policy changes must be added through `scripts/deploy/lib/compose-env.sh`.
- 퇴역 코드는 삭제하며, gate는 제품 동작·보안 설정 결함만 검사합니다.
- Go implementation subpackages use role-specific names reviewed alongside their behavior tests.
- 리팩터링 뒤에도 운영 배포 명령은 저장소 script와 [Quadlet README](../../../deploy/quadlet/README.md) 절차를 따릅니다.
