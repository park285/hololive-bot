# Self-hosted CI 경계

Hololive Bot GitHub Actions workflow는 신뢰하지 않는 pull request 코드를 저장소 self-hosted runner에서
실행하지 않습니다. 현재 `ci.yml`과 `security.yml`의 모든 job은 GitHub-hosted `ubuntu-latest` runner에서
실행합니다. 따라서 PR이 바꾼 스크립트·테스트·패키지 lifecycle hook은 GitHub가 준비한 격리 환경에서 실행됩니다.

과거 저장소 self-hosted Linux x64 runner의 label은 `hololive-ci`였으며, 현재 이 label을 쓰는 workflow는 없습니다.
self-hosted runner를 다시 붙인다면 이미 신뢰된 코드를 checkout하는 branch 작업이나 수동 작업에만 씁니다.
`pull_request` job, PR head를 checkout하는 `pull_request_target` job, 공격자가 고칠 수 있는 저장소 스크립트를
실행하는 workflow에는 붙이지 않습니다. self-hosted runner 사용 시간은 GitHub-hosted Actions 사용 시간으로
과금되지 않지만, 이 비용 절감은 보안 통제가 아닙니다.

## Workflow 분담

- `ci.yml`은 비밀을 쓰지 않는 PR fast gate입니다.
- `security.yml`은 신뢰된 non-PR 이벤트(`main` push, `schedule`, `workflow_dispatch`)에서만 실행하며
  현재는 비밀을 쓰지 않으며, 비공개 모듈 자격 증명이 필요해지면 이 non-PR workflow에만 둡니다.
- 무거운 전체 검증은 로컬 pre-push gate(`scripts/ci/pre-push-gate.sh` -> `scripts/ci/local-ci.sh`)가 맡습니다.

## 차단 게이트

로컬 pre-push gate는 저장소 단위의 게시 전 최종 게이트이며, 한 번 실행에서 다음을 차례로 검사합니다.

1. `scripts/check-release-version.sh`로 release 판본 정책을 검사합니다.
2. 배포·운영 스크립트 테스트를 실행합니다. 정확한 push 범위에서 테스트 자신과 선언된 입력이 모두 바뀌지
   않았으면 건너뜁니다. 범위를 모르거나, `PRE_PUSH_MODE=full`이거나, 선언된 입력이 저장소에 없으면 실행합니다.
   `deploy/quadlet/`이 바뀌었으면 중앙 Quadlet 원본도 `quadlet -dryrun`으로 생성해 봅니다.
3. `scripts/ci/local-ci.sh`를 실행합니다. architecture gate, 민감 로그 검사, Go toolchain 확인,
   `go work sync` drift, 소유 모듈별 `go mod tidy -diff`와 `GOWORK=off` vet, integration tag vet,
   golangci-lint(staticcheck·formatters·modernize 포함), NilAway, Go build, collector production JSON 시험
   (collector 모듈이 범위에 있을 때)과 production build, X Spaces helper 시험, production Go workspace 검사,
   PostgreSQL 용량 검사를 거칩니다. 이어 rootless Podman 일회용 PostgreSQL 하나로 YouTube plane 성능 예산
   벤치마크와 race·non-race 전용 Go 테스트를 실행합니다. golangci-lint·NilAway·build·Go 테스트의 패키지 범위는
   기본 `fast` 모드에서 바뀐 Go 입력이 속한 모듈이고(hololive-shared, go.work·go.mod·go.sum·.golangci.yml, CI 스크립트,
   migration이 바뀌면 소유 모듈 전체), `full` 모드에서는 소유 모듈 전체입니다.
   integration tag 테스트는 기본으로 건너뛰며, 실행 조건은 [scripts/README.md](../../../scripts/README.md)에 있습니다.
4. `full` 모드이거나 `hololive/hololive-youtube-collector/`가 바뀌었으면 YouTube.js helper의 typecheck와
   테스트를 실행합니다.
5. 의존성 hygiene으로 `go.work`의 소유 모듈마다 `govulncheck`를 실행합니다. 형제 저장소 모듈은 각 저장소
   게이트가 검사합니다.

pre-push hook이 넘긴 정확한 push 범위(`BASE_SHA..HEAD_SHA`)가 있고 `full` 모드가 아니며, 그 범위의 변경이
경로와 관계없는 `*.md` 파일과 `VERSION`·`hololive/hololive-api/VERSION`·`hololive/hololive-alarm-worker/VERSION`뿐이면
1단계만 실행하고 2~5단계를 건너뜁니다. 이때 `VERSION` 파일이 바뀌었으면 소유 모듈마다
`GOWORK=off go build -mod=readonly ./...`도 실행합니다.
