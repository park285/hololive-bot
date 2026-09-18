# Local CI NilAway driver validation

결정은 `DEC-20260914-local-ci-nilaway-driver`, 실행 상태는 메타의 `PLN-20260914-local-ci-nilaway-driver`가 소유한다.

## Scope

사용자 요청은 GitHub Actions가 아닌 로컬 CI 최적화 구현이다. 작업 공간은 `/home/kapu/work/iris-stack/.worktrees/local-ci-optimization-20260914/stack/hololive-bot`, 브랜치는 `codex/local-ci-optimization-20260914`, 기준은 `d16609cc1104b16129d5cbc258e2a816153aebf6`이다. 메타는 `f08edcbb44de7e3f50c20d7ce6a6914be9e7b392`의 detached worktree이며 다른 저장소도 독립 detached checkout으로 검증 입력을 고정했다.

변경은 Hololive 로컬 NilAway 실행, 해당 자기 테스트·변경 경로 연결 및 계획/결정/검증 기록이다. Actions, 서비스 코드, 의존성·도구 핀, DB/race/lint/freshness 명령을 변경하지 않았다. 커밋·main 통합·push·배포는 수행하지 않았다.

## Implementation

`scripts/ci/local-ci-nilaway.sh`가 기존 소유 패키지 패턴을 그대로 받아 고정 NilAway 바이너리를 `go vet -vettool`로 실행한다. Go의 package fact 캐시를 사용하며 별도 성공 receipt나 파일 해시 기반 검사 생략을 만들지 않는다. `include-pkgs`로 의존성을 분석에서 제외하지 않는다.

고정 NilAway `v0.0.0-20260808063849-8649a03c818a`의 `cmd/nilaway/main.go`는 `singlechecker.Main`을 호출한다. 바이너리가 사용하는 x/tools singlechecker는 `.cfg` 입력을 `unitchecker.Run`으로 전달한다. 이 기존 경로를 사용하므로 custom golangci plugin이나 새 의존성이 필요 없다.

Go vet가 패키지 디렉터리에서 도구를 실행하므로 `-include-errors-in-files`에 원래 저장소 루트를 명시한다. 이를 생략하면 기존 standalone의 루트 기준 진단 범위가 달라질 수 있다. `NILAWAY_PARALLEL=1|2`를 `-p`에 전달하고 기존 `NILAWAY_GOMEMLIMIT` 검증과 상한을 유지한다. 백그라운드 프로세스·wait 수집·로그 임시 파일은 Go 드라이버의 foreground 실행으로 대체했다.

## Measurement

Go 1.27.1, 동일한 기존 NilAway 바이너리(Go 1.27.0 빌드), 동일 소스 및 아래 6개 패턴을 사용했다. GOMEMLIMIT=10GiB, 분석 동시성 1이다. 각 측정은 MemoryHigh=16G, MemoryMax=24G, RuntimeMaxSec=15min, KillMode=control-group의 일회성 user cgroup에서 순서대로 실행했다.

`./...`, `./hololive/hololive-api/...`, `./hololive/hololive-alarm-worker/...`, `./hololive/hololive-dbtest/...`, `./hololive/hololive-shared/...`, `./hololive/hololive-youtube-collector/...`

| 실행 | wall time | GNU time max RSS | 결과 |
|---|---:|---:|---|
| 기존 standalone, 패턴별 순차 실행 | 100.58초 | 10,305,696 KiB | exit 0 |
| 새 driver, 최초 전체 범위 실행 | 95.30초 | 915,072 KiB | exit 0 |
| 새 driver, 동일 범위 반복 실행 | 1.20초 | 90,728 KiB | exit 0 |
| 새 driver, API auth 패키지에 비동작 선언을 추가한 증분 실행 | 4.05초 | 181,272 KiB | exit 0 |

최초 전체 범위 실행도 기존 Go 빌드 캐시와 앞선 루트 패키지 탐색의 fact 캐시를 사용했다. 캐시 전체를 비운 cold benchmark가 아니다. 반복 실행은 기존 방식 대비 98.8% 단축됐고 최초 전체 범위의 최대 RSS는 약 91.1% 감소했다. RSS는 GNU time이 집계한 프로세스 최대값이며 cgroup 전체 합산 메모리가 아니다. 단일 호스트의 단계별 관측값으로 전체 pre-push 단축률을 뜻하지 않는다.

기존 pre-push receipt도 같은 커밋의 성공 결과를 재사용하므로 증분 실행을 별도로 측정했다. `hololive-api/internal/planes/admin/internal/service/auth`에 임시 Go 파일과 사용되지 않는 상수 선언 하나를 추가하고 같은 6개 패턴 전체를 재검사했다. 4.05초에 성공했으며 임시 파일은 제거했다. 이는 한 패키지의 비동작 소스 변경 사례로, 공개 타입·의존성 변경이나 광범위한 무효화에 같은 시간을 보장하지 않는다. 실제 의존성의 nil 반환으로 동작을 바꾸는 별도 fixture에서는 진단 무효화를 검증했다.

기존 실행의 모듈별 시간은 root 1.76초, API 33.24초, alarm-worker 19.27초, dbtest 8.19초, shared 22.75초, collector 15.33초이다. 합계와 전체 시간의 차이는 프로세스 기동·집계 비용이다. 재현 명령은 각 패턴에 `/usr/bin/time env GOMEMLIMIT=10GiB GOFLAGS=-mod=readonly <pinned-nilaway> -pretty-print <pattern>`을 순차 실행한 뒤 새 `check_nilaway` 함수에 같은 패턴 목록을 전달하는 것이다.

## Validation

- 실제 고정 analyzer fixture: 정상 소스 standalone/native 성공, native 반복 성공, 패키지 간 nil 반환 전파, 성공 뒤 dependency 변경의 무효화, 반복 실패 차단, 수정 뒤 성공, external test package의 nil 진단, 동시성 2에서 동일 진단, 컴파일 오류 차단을 확인했다.
- `bash scripts/ci/local-ci-nilaway_test.sh`: 11개 실제 분석 시나리오 통과. 최초 작성 중 컴파일 오류 메시지 예상값이 실제 `expected declaration`과 달라 fixture만 수정한 후 전부 통과했다.
- `bash scripts/ci/nilaway-inputs_test.sh`: 동시성·메모리 입력 및 주입 방지 검사 통과.
- `bash scripts/ci/test-local-ci-packages.sh`: 기존 라우팅과 새 드라이버/입력 helper 변경의 전체 범위 선택 통과.
- `bash scripts/ci/pre-push-gate-profile-v1_test.sh`: phase·fingerprint 및 새 드라이버 변경 시 실제 analyzer 자기 테스트 선택 통과.
- 변경 셸 8개: `bash -n` 및 `shellcheck -x -P scripts/ci` 통과. 최초 ShellCheck 호출은 source 검색 경로 없이 실행돼 SC1091/SC2034를 냈고, 올바른 source 해석 옵션으로 확인했다. suppression은 추가하지 않았다.
- 전체 로컬 CI 후속 조사에서 메타 정본은 source를 따라가지 않는 `shellcheck --norc --severity=warning`임을 확인했다. 이 옵션에서 새 fixture의 RUN_NILAWAY/NILAWAY_PARALLEL이 SC2034로 판정돼 두 테스트 환경 변수를 export로 명시했다. 정본 옵션과 실제 analyzer fixture를 다시 검증했다. 앞선 source-aware 검사만으로 메타 게시 게이트까지 검증됐다고 보지 않는다.
- 메타 `bash tools/checks/check-ci-consistency.sh`: 15 workflows/8 repos 통과. 이미 준비된 uv 0.12.13을 작업별 PATH로 선택했고 공유 설치본은 변경하지 않았다.
- 메타 `bash tools/checks/check-decision-catalog.sh`: 285개 결정 및 11개 생성 색인 검증 통과, 미분류 pending 0.
- 최종 소스 diff 검토와 `git diff --check` 통과. 다른 7개 소스 worktree는 변경이 없다.

전체 서비스 build·DB/race suite·배포 검사는 재실행하지 않았다. 변경된 analyzer의 전체 소유 패키지 검사와 CI 연결 회귀 검증을 수행했으며 다른 필수 검사의 명령과 경계를 그대로 유지했다. 새 fallback은 없다.
