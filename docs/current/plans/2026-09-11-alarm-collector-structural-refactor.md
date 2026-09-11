# 알람 워커·컬렉터 구조 개선 실행 계획

## Execution capsule

**Goal:** 알람 생성부터 egress, collection discovery부터 publish까지 책임과 수명을 점검하고 확인된 구조·신뢰성 부채를 구현과 검증으로 해소합니다.
**Context:** `48a3205bd` 기반 worktree의 검증된 변경을 보존하며, 후속 지시에 따라 `b262d3a46`의 원본 main 작업 폴더로 통합합니다.
**Constraints:** collector/API/egress 소유권, 외부 이름·스키마·retry 한도·unknown 결과를 유지합니다. 후속 지시에 따른 로컬 main 커밋을 포함하며 새 의존성·배포·원격 쓰기는 범위에 없습니다.
**Evidence:** [선행 분석](../../history/architecture/2026-09-11-alarm-collector-nfr-audit.md), 현재 코드·테스트, architecture gate, Node pagination/PG observation 성능 예산 검사입니다.
**Success:** 각 실행 영역의 소유자가 명확하며 확인된 오류 소실·자원 수명·발송 전 대기 문제의 회귀 테스트가 통과하고, 남긴 동작의 근거와 검증 한계가 명시됩니다.
**Output:** 코드·회귀 테스트·현재 계약 문서·분석/검증 기록을 원본 main의 커밋으로 전달하고 작업 worktree도 보존합니다.

## 실행 경계

이 submodule worktree에는 meta-repository의 DEC/PLN adapter가 없습니다. 저장소의 기존 계획 경로에 unmanaged Markdown을 두며 별도 catalog나 lifecycle 상태를 만들지 않습니다. 아래 T/AC/V는 구현 결과와 검증 근거를 연결하는 식별자입니다.

표준 `context`, `errors`, `errgroup`, 기존 panic guard와 ledger API를 재사용합니다. 사용자에게 새 확인을 요구할 일반적인 구현 선택은 없습니다. 외부 부작용이 시작된 뒤의 timeout은 계속 unknown이며, 실제 sender 호출이 시작되지 않았다는 코드·테스트 증거가 있는 admission 실패에만 기존 retryable 전이를 사용합니다.

### T01 런타임 백그라운드 작업의 수명 소유권 통합

소유: alarm-worker `internal/service/workerruntime`, `internal/app/workerapp`의 runtime 구성, `internal/service/alarm/scheduler`의 의존성 조립, shared `pkg/applifecycle`의 직접 start 오류 경계. 스케줄러·egress·설정 subscriber의 취소와 join을 런타임이 추적하도록 정리합니다. 반복되는 named runner 실행은 실제 두 호출자가 공유하는 하나의 내부 구현으로 정리합니다. 자식 작업의 자체 deadline과 부모 종료를 구분하며 기존 종료 오류 보존을 유지합니다. foundation의 같은 AlarmService 이중 필드와 DB 참조 중복을 제거하고 scheduler의 긴 위치 인자를 내부 dependency 계약으로 정리합니다. 설정 callback의 build context 캡처는 현재 장애가 아니라 장기 수명 경계의 부채로 다룹니다. shared subscriber의 log-only 오류 계약은 유지하면서 시작한 실행의 취소·join은 추적합니다.

### T02 알람 payload 거절의 terminal 처리와 dedup 정리 일치

소유: shared `pkg/service/alarm/dispatchoutbox`의 consumer와 집중 테스트. JSON 오류, 잘못된 delivery context, event payload 누락을 같은 terminal 소유 경계에서 처리합니다. worker fence와 rows-affected 검사로 DLQ commit이 확인된 뒤 PG `Record.ClaimKeys`를 기존 releaser로 해제합니다. 전이 실패·unknown·retry에는 해제를 추가하지 않습니다. 실제 notifier의 방별 exclusive claim 경로와 기존 정상 DLQ의 정책을 재사용합니다.

### T03 Karing admission과 provider 실행 예산 분리

소유: alarm-worker `internal/egress/youtubedispatch`의 Karing 실행·실패 연결과 테스트. 기존 전역 직렬화는 유지합니다. mutex 획득 대기를 bounded admission으로 구분하고, lock을 얻은 뒤 provider 실행 timeout을 시작합니다. lock을 얻지 못해 sender 호출이 0회인 실패는 기존 retryable 상태로 처리합니다. 실제 provider 호출·handoff polling 이후의 불명확한 결과는 기존 SENDING/unknown 보존 계약을 유지합니다.

### T04 컬렉터 실행 의존성과 결과 판정 소유자 정리

소유: collector `internal/runtime/collectorruntime`, `joblease`와 관련 테스트. queue/lifecycle은 scheduler가, 실행·publish와 attempt 판정은 지속적인 executor가 소유하도록 매 호출 의존성 복사와 중계 wrapper를 제거합니다. `_ error` 인자와 reflection 기반 필드 복사 테스트를 없앱니다. provider admission의 typed error, callback 오류와 context 오류, fence loss와 callback 원인을 보존합니다. result invariant 실패가 성공 attempt로 기록되지 않게 합니다. 기존 `(joined, error)` 및 release 순서를 유지합니다.

### T05 전체 경계 점검 결과와 현재 계약 문서 정리

소유: 부모의 docs 및 검증 기록. 알람 checker의 개별 조회 실패, Official mixed-invalid COMPLETE, helper shim·취소·페이지 제한을 실제 소비자와 연결해 판단합니다. 동작 변경 근거가 없는 재작성은 하지 않고 그 근거를 기록합니다. collector helper cleanup과 lease supervision cleanup의 실제 fatal 경계를 문서에 구분합니다. T01~T04 뒤 산출물에서 해결된 항목을 후속 과제로 남기지 않습니다.

### T06 통합 검증과 독립 리뷰

T01~T04 및 T07은 파일 소유권을 나눠 병렬 구현하고 각 집중 회귀 검사 뒤 통합합니다. T05와 T06은 최종 구현을 기준으로 마무리합니다. 독립 리뷰의 actionable finding을 해결하고 최종 diff를 확인합니다. 검사 실패는 원인 확인 후 필요한 범위에서만 재실행합니다.

### T07 helper의 결과 예산을 실제 작업량에 적용

소유: collector `youtubejs/src`의 pagination/content/channel 및 집중 테스트. content에서 결과에 포함되지 않을 행의 premiere metadata 조회를 미리 수행하는 구조를 정리합니다. native async iteration과 기존 byte budget을 재사용해 max-results/bytes에 도달하면 추가 정규화·metadata 요청을 중단합니다. channel live snapshot도 응답 한도를 넘긴 뒤 불필요한 배열·metadata 작업을 계속하지 않도록 같은 응답 예산을 활용합니다. 새 protocol 필드·임의 row cap·provider fallback은 추가하지 않으며 출력 complete/partial 및 typed 오류 계약을 유지합니다.

### T08 종료 취소와 함께 반환된 scheduler 장애 보존

추가 감사 소유: shared `pkg/applifecycle`과 해당 회귀 테스트. 부모가 취소됐더라도 오류 트리에 context 오류 이외의 원인이 남아 있으면 오류 전달 또는 ERROR 로그로 보존합니다. 순수 context 오류만 정상 종료로 다루고 종료 중 미소비 채널 송신은 계속 취소 가능하게 합니다. Bot의 별도 종료 정책은 바꾸지 않습니다.

### T09 실패한 알람 배치의 미발송 lease 반환

추가 감사 소유: shared `pkg/service/alarm/dispatchoutbox` consumer와 집중/PG 테스트. event load 또는 복원·DLQ cleanup 실패로 배치를 반환하지 못하면 기존 `ReleaseLeased`의 worker/status fence로 미발송 claim을 반환합니다. 확정된 DLQ 행은 반환에서 제외하고 dedup 키는 유지합니다. 부분 배치를 발송하거나 send-unit membership을 바꾸지 않습니다. 취소된 호출에서도 정리는 5초로 제한하고 원래 실패와 정리 실패를 함께 반환합니다.

### T10 channel hydration 진행 중 응답 크기 판정

추가 감사 소유: helper `fetch-channel.mjs`와 테스트. metadata 해결마다 scheduled/LIVE/unavailable 표현을 적용하고 기존 byte budget으로 확정된 출력 하한을 검사합니다. 초과가 확정되면 다음 player 요청을 중단합니다. 제한 영상의 큰 중복 행이 고유 unavailable identity로 축소될 수 있는 계약과 최종 RPC 크기 검사를 유지합니다.

### T11 검증된 변경을 원본 main 작업 폴더로 통합

후속 사용자 지시에 따른 부모 소유 작업입니다. 원본 `/home/kapu/work/iris-stack/hololive-bot`의 clean `b262d3a46`과 task worktree의 `48a3205bd`를 기준으로 변경 교집합·의존성·Git 소유권을 확인합니다. task-owned 53개 파일을 한 패치로 준비하고 적용 전 검사 뒤 원본에 적용합니다. 새 분류 커밋과 기존 worktree는 보존하며 index/ref·커밋·원격·운영 runtime은 변경하지 않습니다. 충돌이나 동시 수정이 관측되면 해당 반영을 멈추고 독립적으로 해결 가능한 준비만 진행합니다.

### T12 최종 리뷰와 로컬 main 커밋

후속 사용자 지시에 따라 원본 main의 53개 파일을 최종 리뷰하고 커밋합니다. 알람/shared와 collector/Node 영역을 독립적으로 검토하며 확인된 결함은 커밋 전에 해결합니다. 부모는 기존 검증 snapshot과 현재 내용의 일치, HEAD·index·writer lock 및 stage 파일 목록을 확인합니다. 검토한 파일만 명시적으로 stage하고 저장소 commit hook을 그대로 실행합니다. 원격 push와 배포는 포함하지 않습니다.

## 수용 조건

### AC01 실행·취소·join과 오류 전달 일치

부모가 살아 있을 때 자식의 deadline/error/panic이 정상 종료로 소실되지 않습니다. 종료 시 시작한 subscriber와 scheduler/egress를 기다리며 deadline 초과와 cleanup 오류를 모두 반환합니다.

### AC02 malformed delivery의 정리 순서와 fence 유지

세 decode/rehydrate 거절 경로는 DLQ 성공 뒤에만 releaser를 호출합니다. 소유권 불일치와 terminal 실패에서는 key가 남습니다. 성공·retry·unknown의 기존 dedup 보호를 유지합니다.

### AC03 발송 전 실패와 발송 결과 불명의 구분

admission timeout에서는 sender 호출이 없고 기존 retryable 전이가 사용됩니다. 전송 또는 polling 결과 불명은 재전송·claim 해제·fallback을 유발하지 않습니다. 직렬화와 기존 worker 상한은 유지됩니다.

### AC04 컬렉터 의존성과 실패의 단일 소유

실행 의존성을 job마다 복사하지 않으며 scheduler의 queue 상태와 executor의 결과 판정이 섞이지 않습니다. classified failure와 원인 error matching이 timeout/fence/cancel 경로에서 유지됩니다. invariant 실패는 success로 관측되지 않습니다.

### AC05 검토 범위와 남은 한계의 정확성

두 Go 모듈과 직접 연결 shared 계약, Node helper 및 API 소비 경계를 분석 기록에 포함합니다. 유지한 동작에는 코드·테스트 또는 결정 근거가 있고, 운영 수치와 로컬 측정을 혼동하지 않습니다.

### AC06 helper 작업량과 응답 예산 일치

content `max_results=1`이면 채택하지 않을 이후 항목의 metadata fetch를 실행하지 않습니다. byte 한도 초과는 기존 typed 결과로 종료하며 계속되는 enrichment를 차단합니다. channel oversized fixture도 추가 hydration 전에 응답 한도 위반을 보존합니다. 기존 취소 provenance, pagination stop reason, restricted row와 schedule 처리의 회귀가 없습니다.

### AC07 혼합 오류의 실제 원인 관측

wrapped/joined 순수 취소는 정상 종료이며, 중첩된 실제 오류는 종료 중에도 사라지지 않습니다. 오류 채널 소비가 중단돼도 goroutine이 멈추지 않습니다.

### AC08 실패 배치의 claim 수명과 발송 단위 보존

valid/invalid 혼합과 event load 실패에서 반환하지 못한 미발송 row를 즉시 다시 claim할 수 있습니다. terminal·다른 worker의 row는 덮어쓰지 않고, attempt·send-unit·dedup 계약을 유지합니다. cleanup 실패와 취소는 원래 오류를 덮지 않습니다.

### AC09 hydration 후 초과에 대한 작업 중단

고유 restricted row 또는 schedule/LIVE 해결 결과가 응답 한도를 넘기면 후속 metadata 요청을 실행하지 않습니다. 작은 정상 결과와 큰 중복 restricted 입력의 축소는 계속 성공합니다.

### AC10 원본 통합의 내용 동일성과 새 커밋 보존

원본의 HEAD가 `b262d3a46`을 유지하고 변경 파일 집합은 task-owned 53개와 일치합니다. Go/Node 소스와 테스트는 검증된 worktree와 byte 단위로 같으며 새 분류 커밋의 파일은 변경하지 않습니다. 통합 기록을 양쪽 문서에 반영하고 원본 환경에서 연결된 경로를 검증합니다.

### AC11 검토된 내용의 main 커밋 일치

원본 main에 생성한 커밋은 `b262d3a46`을 부모로 갖고, 승인된 53개 파일만 포함합니다. HEAD의 내용이 최종 stage와 같으며 원본 작업 폴더에 미전달 수정이 남지 않습니다. 실제 commit ID와 완료 여부는 문서 자체에 순환 참조로 넣지 않고 Git ref/log에서 확인합니다.

## 검증

### V01 집중 회귀 검사

T01~T04의 관측 가능한 실패를 수정 전 재현하고 수정 후 해당 패키지 `go test -race`와 lint를 실행합니다. 단순 파일 배치나 필드 복사 자체를 검사하는 테스트는 추가하지 않습니다.

### V02 영향 범위 통합 검사

두 모듈과 변경 shared 패키지·직접 소비자의 Go 테스트/race/lint/NilAway를 실행합니다. Go workspace production 검사, collector hardening 계약, alarm/egress 계약 및 `scripts/architecture/ci-boundary-gate.sh`를 실행합니다. 실제 retry/dedup 영향을 stack 계약 검사에 반영합니다. 새 의존성·스키마·외부 이름이 없는지 diff로 확인합니다.

### V03 helper 및 NFR 검사

`npm --prefix hololive/hololive-youtube-collector/youtubejs test`, `run typecheck`, `run test:pagination-performance`, `bash scripts/perf/check-youtube-plane-budget.sh`를 사용합니다. source가 바뀌지 않은 통과 검사는 유효한 기존 실행 근거를 재사용합니다. 운영 부하·외부 provider smoke는 이번 로컬 검증으로 대체했다고 주장하지 않습니다.

### V04 독립 리뷰와 전달 검사

최종 코드에 독립 Astra 리뷰를 수행하고 actionable finding을 해결합니다. `git diff --check`, 최종 diff, 작업/원본 checkout 상태를 확인합니다. 결과 보고에는 worktree·브랜치, 실제 검사, 남은 통합 단계와 미검증 항목을 명시합니다.

### V05 추가 감사 회귀와 통합 검증

T08~T10은 disjoint ownership으로 구현하며 수정 전 실패와 수정 후 targeted race/Node 테스트를 확인합니다. T09는 task-owned PostgreSQL의 integration-tag race로 실제 claim 반환·fence를 검증합니다. 변경 후 전체 Node/typecheck, 영향 Go 패키지 및 local CI, stack retry/worker 계약, 독립 Astra 리뷰와 최종 diff를 확인합니다. Collector buffered callback 우선은 기존 테스트 계약으로 유지하며 terminal commit과 supervisor cleanup의 관측 범위를 문서에 명시합니다. 새 결함 증거가 없는 영역은 통과한 선행 검사를 반복하지 않습니다.

### V06 원본 반영 및 연결 경로 검증

`git apply --check`, 적용 전후 HEAD/index·파일 집합과 SHA-256을 확인합니다. 새 main 분류 변경과 연결되는 alarm checker·egress·shared mekparkhost 및 bot handlers를 포함한 targeted Go race/build, Node 전체/typecheck, stack retry/worker 계약을 원본에서 확인합니다. 동작 코드가 이전 검증 내용과 동일하면 선행 전체 CI/NilAway/PG 결과를 재사용하며, 충돌 해결로 코드가 달라지면 그 변경을 별도 검증·독립 리뷰합니다. 최종 diff 및 문서 구조를 검사합니다.

### V07 커밋 전후 검증

최종 독립 리뷰에서 blocker가 없는지 확인하고 `git diff --cached --check`와 파일별 내용 비교를 수행합니다. 명시적인 53개 stage 범위에는 hook의 documented bulk-stage 확인을 해당 커밋에만 적용하며 검사 제외나 `--no-verify`를 사용하지 않습니다. commit hook 성공 뒤 parent·변경 파일 목록·tree·main ref·원본 clean 상태를 확인합니다. 이미 통과한 source-identical 전체 CI/NilAway/PG 및 원본 통합 검증을 재사용하고 리뷰 중 변경된 코드만 필요한 검사를 추가합니다.

## 실행 근거

아래 항목의 코드와 검사 결과는 [분석·검증 기록](../../history/architecture/2026-09-11-alarm-collector-nfr-audit.md)에 함께 정리했습니다. 운영 배포·source 이관 상태를 뜻하지 않습니다.

| 식별자 | 결과와 근거 |
| --- | --- |
| T01 / AC01 | foundation 단일 서비스, scheduler Dependencies, shared named runner와 subscriber join, parent/child context 오류 구분, build callback 수명 분리 구현. `runtime_background_test.go`, `lifecycle_scheduler_context_test.go`, `build_runtime_config_test.go` 및 builder nil 회귀/race/NilAway 통과. |
| T02 / AC02 | `consumer_envelope.go`가 복원·terminal 거절을 소유하며 fence 확정 뒤 key 해제. `consumer_payload_cleanup_test.go`의 세 거절 경로·전이 실패·해제 실패·정상 입력 검사, 실제 PG integration-tag race 통과. |
| T03 / AC03 | admission을 BeginSending 전으로 이동하고 전송 직후 slot 해제. `send_engine_karing_admission_test.go`와 DB 회귀에서 sender 0회 실패, 재시도 후 정확히 1회 성공, completion DB 지연 중 다른 방 sender 진입, post-send unknown 보존 검증. |
| T04 / AC04 | executor 1회 조립, wrapper/필드 복사 제거, typed cause와 panic/fence/deadline 보존, invariant의 failed attempt 기록 구현. `executor_test.go`, `run_join_test.go`, 두 패키지 전체 race 및 collector 전체 NilAway 통과. |
| T05 / AC05 | 현재 서비스·런북과 분석 기록 정리. canonical reader 30분/15분 제한·coverage/freshness 차이, Official row 보존 계약, checker 부분 성공, helper raw 입력 한계를 직접 소비자 근거로 구분. |
| T06 / V02 / V04 | 최종 `LOCAL_CI_GO_SCOPE=changed BASE_REF=48a3205bd HEAD_REF=HEAD bash scripts/ci/local-ci.sh` 종료 0. shared 변경에 따라 전체 workspace Go 검사가 선택됨. stack retry/worker 계약 통과. 독립 리뷰 P2 두 건 해결 및 후속 새 actionable 결함 없음. 최종 diff/원본·형제 checkout 상태 확인. |
| T07 / AC06 / V03 | content lazy async iteration과 channel 행별 최소 byte 검사 구현. Node 159 tests/typecheck 통과. PAG-014 증가율 예산 통과, 최종 PG observation 모델 0.322초/3.6초 예산 통과. |
| V01 | 종료·nil builder·DLQ cleanup·Karing admission/완료 분리·collector cause/invariant/panic·helper 초과 후 작업 중단을 각각 수정 전 실패로 재현하고 수정 후 통과 확인. |
| T08 / AC07 | context-only error tree와 혼합 원인을 분리했습니다. nested mixed 오류의 종료 중 ERROR 로그, 미소비 채널 무블록, 순수 wrapped/joined context 정상 종료 회귀와 lifecycle/직접 runtime 소비자 race가 통과했습니다. |
| T09 / AC08 | 실패 배치의 기존 ReleaseLeased 반환과 confirmed DLQ 제외를 구현했습니다. 원인 Join·독립 5초 정리·정상/실패 단위 7경로, 임시 PostgreSQL의 load/terminal/key/다른 owner 4경로 및 전체 package integration-tag race가 통과했습니다. |
| T10 / AC09 | metadata별 실제 표현과 array 비용을 반영하고 충돌 검사를 hydration으로 단일화했습니다. 두 번째 metadata에서 초과하면 세 번째 조회가 없고, UTF-8·중복·정확한 한도 및 restricted 축소는 성공합니다. 최종 Node 166 tests/typecheck 통과. |
| V05 | 추가 구현·테스트 정리 뒤 `LOCAL_CI_GO_SCOPE=changed BASE_REF=48a3205bd HEAD_REF=HEAD RACE_TEST_PARALLEL=4 bash scripts/ci/local-ci.sh` 종료 0 / Passed. 전체 Go/race/NilAway/필수 lint·build, 임시 PG integration-tag race, Node 166/typecheck, stack retry/worker 및 문서 검사 통과. 독립 Astra 리뷰에 새 actionable 결함 없음. 별도 integration-tag 전체 lint는 기존 테스트 부채 460건으로 실패했으며 신규 테스트 진단은 0건입니다. 대표 근거와 후속 범위는 분석 기록에 남겼습니다. |
| T11 / AC10 / V06 | 원본 main `b262d3a46`의 clean 작업 폴더에 53개 파일을 미커밋 통합했습니다. 새 classifier 커밋의 19개 경로와 교집합 0, 적용 검사 및 SHA-256 53개 일치, HEAD/index 보존을 확인했습니다. 원본에서 bot handlers·mekparkhost·YouTube egress·alarm dispatch/checker의 5패키지 race, alarm-worker/collector build, Node 166/typecheck, stack retry/worker와 문서 검사가 통과했습니다. 형제 리비전·소스가 선행 검증과 동일해 전체 CI/NilAway/PG 결과는 재사용했습니다. 작업 worktree를 보존하며 커밋·원격·배포는 수행하지 않았습니다. |
| T12 / AC11 / V07 | 최종 알람/shared 및 collector/Node 독립 리뷰에 actionable finding이 없습니다. 추가 Node 집중 검사 66개와 collector/joblease 종료·오류 회귀 10개의 race 검사가 통과했습니다. 커밋 대상은 검증된 53개 파일이며 이 문서를 포함한 main 커밋의 실제 parent·tree·완료 상태는 Git log/ref가 소유합니다. |
