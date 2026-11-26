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

- `ci.yml`의 필수 `fast-gate`는 다섯 Go 모듈의 결과를 모은 뒤 같은 SHA의 셸 문법·gofmt, 스크립트 시험 모듈 전체 컴파일과 로그·runtime 스크립트 동작을 검사합니다. 별도의 정책 러너는 없습니다. collector 작업은 Node 설치 한 번으로 Go 통합 시험과 YouTube.js 타입 검사·시험을 수행합니다.
- `security.yml`은 비밀 없이 `main` push·정기·수동 이벤트에서 실행합니다. Go 스캐너는 고정된 형제 checkout과 Go 설치를 공유하고 npm은 독립적으로 감사합니다. 정기·수동 arm64 이미지 검사는 별도 러너에서 실행되며 새 main push가 취소하지 않습니다.
  Go 검사에는 고정 SHA의 형제 SDK를 사용합니다. API의 독립 Valkey 어댑터는 공개 프록시에서 `go.mod`의
  판본으로 별도 다운로드해 `go.sum` 변경이 없는지 확인한 뒤 workspace에 넣습니다. SDK 루트 checkout의
  판본을 바꾸거나 개발자 checkout을 운영 이미지로 복사하지 않습니다.
- 무거운 전체 검증은 로컬 pre-push gate(`scripts/ci/pre-push-gate.sh` -> `scripts/ci/local-ci.sh`)가 맡습니다.

## 차단 게이트

로컬 pre-push gate는 저장소 단위의 게시 전 최종 게이트이며, 한 번 실행에서 다음을 차례로 검사합니다.

1. 릴리스 입력(`VERSION` 세 파일·검사 스크립트)이 바뀌었거나 범위 미상·`full`이면 `scripts/check-release-version.sh`로 판본 형식을 검사합니다. 변경 이력의 제목·날짜 표기는 게시 차단 조건이 아닙니다.
2. 배포·운영 스크립트 테스트를 실행합니다. 정확한 push 범위에서 테스트 자신과 선언된 입력이 모두 바뀌지
   않았으면 건너뜁니다. 범위를 모르거나, `PRE_PUSH_MODE=full`이거나, 선언된 입력이 저장소에 없으면 실행합니다.
   `deploy/quadlet/` 변경·범위 미상·`full`이면 중앙 Quadlet 원본도 `quadlet -dryrun`으로 생성합니다.
3. `scripts/ci/local-ci.sh`를 실행합니다. architecture gate, 민감 로그 검사, Go toolchain 확인,
   전체 workspace의 Go 소스·테스트 import를 사용한 `go work sync` drift(루트와 소유 모듈의 메타데이터만 대조), 소유 모듈별 `go mod tidy -diff`, integration tag vet,
   golangci-lint(staticcheck·formatters·modernize 포함), NilAway와 Go build를 실행합니다. collector production 빌드는 한 번만 합니다. 빌더·검사기·시험 입력이 바뀌었거나 범위 미상·`full`이면 그 산출물의 사본으로 손상 거부도 확인합니다. X Spaces helper, production Go workspace와 PostgreSQL 용량 검사도 유지합니다.
   제품 race·non-race 시험과 선택한 통합 시험은 같은 일회용 PostgreSQL 서버를 씁니다. collector가 범위에 있으면 race JSON에서 실패·panic·예상 밖 skip을 검사하고 소스·시험 구성이 다른 CGO 비활성 패키지만 추가로 실행합니다. 배포·runtime 스크립트 시험은 제품 목록과 분리하여 `-p 1 -parallel 4 -count=1`로 실행하며, 스크립트만 선택된 경우 PostgreSQL 서버를 만들지 않습니다.
   fast는 두 게시 tip의 차이에서 이전·새 경로를 모두 읽습니다. 수동 실행은 staged·unstaged·untracked 입력도 포함합니다. Go 입력은 확장자가 아니라 소유 모듈과 소비자로 판정합니다. 공통 모듈·migration·워크스페이스 입력, `full` 또는 미상 범위는 전체 소유 모듈을 선택합니다.
   통합 시험의 opt-in 조건은 [scripts/README.md](../../../scripts/README.md)에 있습니다.
4. collector 또는 helper 설치기 변경·범위 미상·`full`이면 YouTube.js helper의 typecheck와
   테스트를 실행합니다.
5. 의존성 hygiene으로 `go.work`의 소유 제품 모듈마다 `govulncheck`를 실행합니다. 형제 저장소 모듈은 각 저장소
   게이트가 검사합니다.

pre-push hook이 넘긴 정확한 push 범위(`BASE_SHA..HEAD_SHA`)가 있고 `full` 모드가 아니며, 그 범위의 변경이
문서와 `VERSION`·`hololive/hololive-api/VERSION`·`hololive/hololive-alarm-worker/VERSION`뿐이면
이 저장소 게이트는 Python을 준비하지 않고 2~5단계를 건너뜁니다. 상위 스택 훅의 비밀 게시 검사는 그대로 실행합니다. `VERSION` 파일이 바뀌었으면 소유 제품 모듈마다
`GOWORK=off go build -mod=readonly ./...`도 실행합니다.
