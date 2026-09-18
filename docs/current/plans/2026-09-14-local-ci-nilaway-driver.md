# Local CI NilAway driver

**Decisions:** `DEC-20260914-local-ci-nilaway-driver` (governing)

## Execution capsule
**Goal:** 로컬 CI의 반복 NilAway 분석 비용을 줄인다.
**Context:** local-ci.sh는 모듈별 standalone 프로세스로 공통 의존성을 반복 분석한다. 고정 바이너리의 singlechecker는 Go vet의 unitchecker 실행도 지원한다.
**Constraints:** 동일 패키지·테스트·의존성 분석과 실패 차단을 유지한다. Actions·운영·의존성·도구 핀·커밋·push는 범위 밖이다.
**Evidence:** local-ci.sh, nilaway-inputs.sh, 고정 NilAway 및 x/tools 소스, 실제 로컬 비교 결과.
**Success:** 교차 패키지 진단·캐시 무효화·실패 검증을 통과하고 동일 범위의 반복 분석 시간이 감소한다.
**Output:** 독립 worktree의 구현·fixture·측정 기록과 DEC/PLN.

## Scope and order
현재 세션이 `.worktrees/local-ci-optimization-20260914/stack/hololive-bot`를 소유한다. 기준은 d16609cc1104b16129d5cbc258e2a816153aebf6이며 형제 모듈은 읽기 전용 작업 공간 입력이다. T01 → T02 순서로 수행한다. 분석은 RuntimeMaxSec와 KillMode=control-group 및 메모리 상한이 있는 transient cgroup에서 실행한다. 진단 누락·캐시 오판·실패 은폐가 발견되면 해당 구현을 전달하지 않고 원인을 해결한다.

### T01 Implement and verify the native analysis driver
동일 NilAway 바이너리를 go vet -vettool로 실행하는 로컬 소유 함수를 구현한다. 모듈 패턴은 그대로 전달하고 저장소 루트 진단 범위와 1/2 프로세스 상한을 명시한다. 실제 analyzer fixture로 AC01/V01을 검증한다.

### T02 Integrate and measure the local gate
pre-push 자기 테스트 및 변경 경로에 새 소유 파일을 연결한다. 이전 standalone과 새 드라이버를 같은 소스·도구·패키지에서 비교하고 AC02/V02를 검증한다.

### AC01 Preserve analysis and failure contracts
패키지 간 nil 흐름과 테스트 코드 진단, 성공 후 의존성 변경의 무효화, 반복 실패와 컴파일 오류가 모두 차단된다. 분석 범위 제외와 성공 캐시를 추가하지 않는다.

### AC02 Reduce repeated local analysis without changing other gates
동일 소유 패키지의 실제 분석이 통과하고 반복 실행 시간이 감소한다. race·DB·lint·freshness와 Actions 계약은 유지한다.

### V01 Exercise real analyzer regression fixtures
`bash scripts/ci/local-ci-nilaway_test.sh`와 `bash scripts/ci/nilaway-inputs_test.sh`를 실행한다. 실제 고정 NilAway로 cold/warm 성공·실패와 의존성 변경을 검증한다.

### V02 Check integration and compare measurements
`bash scripts/ci/pre-push-gate-profile-v1_test.sh`, `bash scripts/ci/test-local-ci-packages.sh`, 변경 셸의 ShellCheck와 bash -n, 메타 `check-ci-consistency.sh`와 결정 카탈로그를 검증한다. 동일 소유 패키지의 기존/신규 NilAway exit 상태·wall time·RSS를 기록하고 최종 diff를 검토한다.
