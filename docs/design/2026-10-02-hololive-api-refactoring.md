# Hololive 통합 리팩토링 계획

API 책임·성능 분석과 저장소 폴더·파일 개편안을 합친 **단일 실행 계획**입니다. 기존 링크를 보존하기 위해 이 파일을 정본으로 재사용합니다. [API 심층 분석](2026-10-02-hololive-api-deep-analysis.md)과 [저장소 구조 분석](2026-10-02-repository-layout-refactoring.md)은 코드 근거·실험·이동표를 소유하는 참고 문서이며 별도의 실행 순서를 운영하지 않습니다.

목표는 **동작과 결과 계약 복원 → 작은 패키지 결합 제거 → API 구성 및 shared 책임 회수 → observation 이관**입니다. 동작을 보존하는 작은 성능 개선은 구조 이관과 독립적으로 진행하고, namespace·Node·최상위 폴더 개편은 필요가 입증된 범위만 후순위로 진행합니다. 최초 통합은 문서 작업이었으며, 아래 착수 기록부터 제품 구현 결과를 구분하여 기록합니다.

## 적대적 리뷰 후속 수정 — 2026-10-03

### 재리뷰와 운영 반영 준비

사용자가 추가 병렬 리뷰 뒤 커밋·원격 push·라이브 반영을 요청했고, 기존 감사·collector 개선을 포함한 검증된 Hololive 전체 변경의 게시도 승인했습니다. 별도 병렬 리뷰에서 확인한 다음 경계를 추가로 수정했습니다.

- Console은 worker의 `outcome_unknown` 응답을 확정 실패로 표시하지 않고 미확인으로 표시합니다. 공개 응답을 실제 SDK·검증기·controller로 소비하는 회귀와 성공·거절 구분을 확인했습니다.
- aggregate/Fx의 Close 대기는 호출자별 시한을 지키며, 최초 timeout 뒤에도 같은 plane owner에 다시 합류해 미완료 자원을 정리합니다. 실제 admin 작업과 Fx `Stop → SafetyClose`, 동시 대기, 원래 오류 보존, telemetry 정리 1회를 검증했습니다.
- YouTube의 첫 Start와 Close가 동시에 호출돼도 Close가 같은 lifecycle 잠금에서 시작을 차단하고 task snapshot을 확정합니다. 기존 Shutdown-before-Start 의미와 늦은 claim 해제를 유지합니다.

추가 변경은 대상 race·lint·NilAway 또는 Console 타입 검사와 동작 시험을 통과했고 독립 리뷰도 완료했습니다. 이 결과는 아래 전체 CI 이후의 후속 검증입니다. 게시 대상은 원격 main의 7.2.2·migration 256·Seoul 원본 전송 수정을 보존하여 통합하며, 통합본의 게시 검사와 실제 운영 수용 결과는 별도로 기록합니다.

통합 구현과 당시 전체 검증 뒤 적대적 리뷰에서 7개 미해결 경계를 확인했습니다. 사용자 요청에 따라 7건의 제품 수정·독립 리뷰·최종 전체 CI를 완료했습니다. `alarmAdvanceMinutes`는 사용자 승인에 따라 `1..1440`으로 확정했으며 0은 저장·worker 호출 전에 400으로 거절합니다. 조건부 Node·최상위 폴더·H3/local 항목은 비용과 계약 근거에 따라 채택 여부를 결정했습니다. 아래 1차 기록과 후반 분석 원문은 이전 범위·조사 시점의 이력이며, 현재 완료 판정은 이 절과 체크리스트를 따릅니다.

### 후속 수정 범위

- [x] PO issuer Docker build의 API module metadata COPY·dockerignore를 실제 소비 조건에 맞춥니다.
- [x] AP 실제 preflight·rsync preview의 정확한 metadata 허용 범위를 맞추고 기존 금지 범위를 유지합니다.
- [x] LLM H3 종료를 단일 작업으로 소유하며 caller deadline 내 반환·실제 join 전 자원 보유·후속 plane 정리를 보장합니다.
- [x] 설정의 Get/merge/persist/apply 순서를 공유 owner에서 조정하고 빈 요청의 stale snapshot 저장을 제거합니다.
- [x] 알람 적용 queue 대기와 전송을 같은 전체 호출 예산으로 제한합니다.
- [x] YouTube claim 등록이 끝나기 전에 빈 release를 최종 완료로 확정하지 않습니다.
- [x] 정상 bot 시작 취소와 실제 readiness 실패의 오류 identity·분류를 구분합니다.

### 후속 수정 결과와 개별 검증

| 결함 | 수정과 확인한 경계 |
|---|---|
| PO issuer build 실패 | API의 `go.mod`·`go.sum`만 COPY·dockerignore에 포함했습니다. 실제 Docker `broker-build` stage가 Linux AMD64·ARM64 모두 통과했습니다. worker 구현이나 metadata는 이 builder에 추가하지 않았습니다. |
| AP 실제 preflight 거절 | 실제 `ap-deploy.sh`와 preview에서 API/worker의 정확한 module metadata 파일을 허용했습니다. 정상 입력 5개·거부 입력 27개로 파일 종류와 하위 경로 경계를 확인했고, 실제 entrypoint에 symlink manifest 입력 시 SSH/rsync 호출 전에 실패함을 확인했습니다. |
| LLM 종료 시한 무시 | HTTP stop은 하나의 진행 작업만 소유하고 대기 caller는 자신의 시한에 반환합니다. 실제 인증된 H3 trigger가 남아 있어도 다음 plane의 Shutdown이 호출됩니다. HTTP·scheduler join 전에 DB를 해제하지 않으며, metrics/pprof handler가 남은 timeout과 후속 Close·동시 Close에서도 최초 오류와 자원 소유권을 보존합니다. |
| 설정 저장·적용 순서 역전 | 장수명 admin Handler의 공유 gate로 Get/merge/persist/apply와 일관된 GET snapshot을 조정합니다. `{}`는 저장·적용·활동 기록을 생략합니다. 실제 worker 연결에서 두 변경의 순서, 취소된 대기/조회, 실제 Linux 부분 쓰기 실패, 적용 뒤 응답 유실을 검증했습니다. |
| 알람 대기 예산 누락 | admin의 기존 10초 예산에 gate 대기부터 적용까지 포함했습니다. client도 실제 양수 HTTP timeout을 대기와 전송의 전체 예산으로 적용하며, timeout 0 비활성·더 짧은 caller deadline·전송 전 거부와 전송 후 unknown 구분을 보존합니다. |
| 늦은 YouTube claim 해제 누락 | claim producer가 등록을 마칠 때까지 release 완료를 봉인하지 않습니다. 등록 join과 token-fenced release는 같은 기존 settlement 예산을 사용하고, 늦은 worker의 해제된 token 재등록을 막습니다. 새 5개 회귀가 수정 전 overlay에서 모두 실패하고 수정 후 통과했습니다. 이는 제어된 스케줄 지연 재현이며 운영 DB 경합 관측은 아닙니다. |
| 정상 bot 시작 취소의 fatal 오판 | cache readiness 오류가 `ctx.Err()` identity를 보존하고, bot은 모든 말단 원인이 실제 runtime 종료 원인과 일치할 때만 정상 종료로 처리합니다. 실제 miniredis/cache/orchestration, 독립 readiness deadline, timeout 직후 runtime 취소, 중첩 fatal 보존 회귀를 검증했습니다. |

- 영향 package 전체 race·golangci-lint(`0 issues`)·NilAway가 통과했습니다. 별도 독립 리뷰가 7건을 확인했고 LLM/admin/alarm/YouTube 지정 회귀를 다시 race로 실행했습니다. 리뷰 중 추가로 확인한 API metadata 하위 경로 누락과 readiness timeout 직후 취소 경계도 보완 후 통과했습니다.
- AP 관련 기존 시험·실제 preview 입력·shellcheck·구문 검사를 통과했습니다. 로컬 Docker build는 배포·활성화나 ARM64 실행 결과가 아닙니다.
- Stack DB access policy와 두 변경 저장소의 `git diff --check`가 통과했습니다. Iris Console의 공개 입력·생성물은 후속 수정에서 바뀌지 않았으므로 아래 기존 전체 검증을 재사용합니다.
- Fallback delta: none. 자동 재시도·새 runtime dependency·SQL/schema 변경은 없으며 실제 운영·Git 게시·배포를 수행하지 않았습니다.

### 후속 수정 최종 통합 검증

- `RUN_INTEGRATION_TESTS=true RUN_RACE_TESTS=true RUN_NILAWAY=true STRICT_STATICCHECK=true RACE_TEST_PARALLEL=2 bash scripts/ci/local-ci.sh`: **exit 0**, 마지막 `[LOCAL CI] Passed`까지 확인했습니다. 외부 DB·Valkey 시험 주소와 package scope 환경을 제거하여 전체 범위와 격리 서비스를 사용했습니다.
- Architecture·민감 로그·toolchain·workspace drift·모듈별 tidy 및 canonical vet·integration-tag 컴파일·Staticcheck·golangci-lint(**0 issues**)·NilAway·제품 build가 통과했습니다. collector production JSON/build·Node helper·production workspace·AP manifest·PostgreSQL capacity·YouTube plane budget 검사도 통과했습니다.
- 일반 Go 테스트 **242 package**, `-race -count=1` **242 package**가 모두 통과했습니다. 같은 실행의 격리 PostgreSQL·Valkey에서 integration-tag 2개 package와 LLM summarizer·worker YouTube dispatch·collector joblease 통합 4개 package가 통과했고, 시험 서비스 정리도 완료했습니다.
- 전체 로그: `/tmp/hololive-adversarial-fixes-local-ci.log`. 위 개별 검증과 최종 통합 실행에서 남은 조치 대상 결함은 없으며, 수정 코드의 운영 활성화는 이 검증 범위에 포함하지 않습니다. 이 절의 후속 검증이 아래 이전 기록보다 우선합니다.

### 통합 구현 결과

- strict 설정·API config·DB factory·공통 foundation과 순수 시간/target 정책을 실제 소유 leaf로 옮겼습니다. shared HTTP 기능과 admin A2, worker 전용 7 package 및 alarmdispatch, collector/API observation 구현과 교차 시험을 이행했습니다.
- observation SQL 85개와 worker 이관 SQL 38개의 내용은 변경하지 않았습니다. 격리 PostgreSQL에서 실제 양 구현을 연결한 observation·canonical clock·repository·ACL 시험을 통과했습니다. 기존 observation 시험·benchmark 212개를 유지했습니다.
- Fx/aggregate/각 plane의 종료 오류 보고와 자원 해제 조건을 분리하고, 모든 sampler·scheduler·reload 작업 join 및 남은 hard deadline 전달을 검증했습니다. 실제 HTTP/H3 요청의 admission·join, 중복 정리 방지, timeout 후 실제 종료 확인과 원래 오류 보존, 인증서 갱신 panic 전달도 확인했습니다.
- member-news의 lazy 본문 정규화·기간 후보·immutable run snapshot과 prepared metadata를 적용했습니다. 합성 선택률/동시성 matrix와 dispatch 집계 갱신 전후 plan을 측정했으며 운영 개선율로 해석하지 않습니다. 추가 리뷰의 SQL 경계 오류 두 건도 실제 DB 재현과 회귀로 해결했습니다.
- 현재 소유권·계약·runbook 문서와 Docker/AP 경로 소비자를 이행했습니다. 외부 의존성의 선언 버전과 실제 MVS 선택 버전은 변경하지 않았습니다. 새 runtime dependency·fallback·Git 게시·배포·운영 설정 변경은 없습니다.

### 성능 측정과 조건부 변경 결정

실제 적용한 member-news 경로는 run마다 같은 `now`로 공통 후보를 한 번 조회하고 immutable metadata를 준비합니다. 방별 멤버·alias 관측과 guard/요약/render/enqueue는 방 처리 시점과 실패 단위를 유지합니다. 공통 후보 조회 실패는 run 오류이며 이전 snapshot이나 빈 성공을 반환하지 않습니다. 단일 방 호출은 멤버가 없으면 후보 조회 전에 기존 오류를 반환합니다.

격리 PostgreSQL 18.6·합성 20방 상당·최대 병렬 5·pool 12, 각 조건 3회 중앙값입니다. 실제 peak 5·pool wait 0을 확인했고 모든 후보 값·순서 동등성을 비교했습니다. 개발 호스트의 다른 검증 부하는 통제하지 않았습니다. 결과는 운영 지표가 아니며 세 열의 개선율을 합산하지 않습니다.

| 후보 수 / 선택률 | 전체 조회 + 방별 필터 | 기간 조회 + 방별 필터 | run snapshot + prepared metadata |
|---|---:|---:|---:|
| 1,000 / 1% | 31.69ms | 13.82ms | 2.36ms |
| 1,000 / 10% | 26.98ms | 19.05ms | 4.08ms |
| 1,000 / 100% | 94.31ms | 111.78ms | 20.87ms |
| 10,000 / 1% | 194.69ms | 89.59ms | 18.57ms |
| 10,000 / 10% | 265.30ms | 193.21ms | 31.51ms |
| 10,000 / 100% | 616.67ms | 756.33ms | 173.61ms |

- 기간 SQL은 활성 후보 scan에 ordinal을 붙인 뒤 기간을 걸러 해당 실행에서 관측한 scan 순서를 유지합니다. 기존 쿼리 자체에 정렬 계약이 없으므로 parallel Gather의 실행 간 동률·fallback 순서까지 보장하지 않습니다. 적대적 비교에서 baseline 자체도 반복마다 달라졌으며 새 회귀로 분류하지 않았습니다. **DB 기본 scan은 여전히 전체 후보**이며 선택률 100%에서 기간 조회만의 시간은 악화했습니다. 전송/Go 후보 수와 공통 조회 횟수를 줄이는 변경입니다. 10,000/1%의 EXPLAIN은 전체 조회 2,000 buffers·5.797ms, 기간 조회 2,003 buffers·27.743ms·반환 100행·sort 170kB이며 temp write는 없습니다. 기존 NULL 배열 원소의 scan 오류를 보존하는 unnest subplan도 10,000회 실행됐습니다.
- CPU만 분리한 합성 비교는 eager 97.52ms/141.14MB, lazy 42.00ms/46.58MB, prepared 10.77ms/8.22MB였습니다. 전체 값·순서 60조건, 날짜 우선순위·UTC/Seoul/New York·KST 경계·non-finite·fallback 회귀를 유지합니다. 추가 리뷰에서 발견한 기간 밖 NULL 배열 원소의 scan 오류 은폐와 유한 DATE의 timestamp 변환 오류를 실제 PostgreSQL에서 재현·수정했고, 다차원/비표준 하한 배열과 DATE 최소·최대값까지 검증했습니다.
- dispatch 집계는 **현행 `count(id)` 유지**로 결정했습니다. 50만 행 VACUUM 직후 `count(*)` 30.16ms 대 현행 50.59ms였으나 5% 갱신 후에는 141.96ms 대 72.38ms였습니다. 갱신 후 index-only plan의 heap fetch는 500,068회였고 12 caller/pool 4의 합산 acquire wait도 2,178ms 대 1,843ms로 악화했습니다. stale cache·보존 기간 축소·새 index를 추가하지 않습니다.
- 내부 **H3 유지**: 합성 transport-only 1,000회 p50/p95/p99는 0.197/0.306/0.633ms, 약 35KB/call입니다. typed copy가 더 싸지만 DB·LLM·application 비용을 포함하지 않으며, trigger 취소/실행 예산·auth·DTO·오류 경계 변경을 정당화하지 않습니다. 기동 옵션 주입과 transport 수명 소유권 수정은 완료했으며 local binding으로 바꾸지 않았습니다.
- **Holodex 현행 오류 계약 유지**: origin 100ms·6 caller에서 성공은 1회 fetch/마지막 106.9ms, 실패는 6회 fetch/마지막 605.7ms였습니다. 비용은 확인됐지만 오류 공유는 독립 caller의 취소·실패 예산을 바꿉니다. 새 negative cache나 실패 공유를 추가하지 않습니다.
- **A3·Node·최상위 경로 개편은 채택하지 않습니다.** 실제 소유권과 구성 결합은 현재 module 위치에서 분리됐습니다. 추가 namespace 단축은 fan-out을 줄이지 않으며, Node/최상위 이동은 테스트 탐색·fixture·배포 경로의 추가 이행 비용에 비해 제품 이득이 확인되지 않았습니다. 기존 `hololive/hololive-*` module identity와 helper 설치 위치를 유지합니다. private claim/send 분할도 계획의 명시적 제외 범위를 유지합니다.

실행 근거: [matrix](evidence/2026-10-02-hololive-api-implementation/membernews-matrix.txt), [값·순서 동등성](evidence/2026-10-02-hololive-api-implementation/membernews-matrix-equality.txt), [후보 query plan](evidence/2026-10-02-hololive-api-implementation/membernews-query-plans.txt), [SQL 경계 수정 전 재현](evidence/2026-10-02-hololive-api-implementation/membernews-boundary-before.txt), [수정 후 회귀](evidence/2026-10-02-hololive-api-implementation/membernews-boundary-after.txt), [집계 갱신 전후 plan](evidence/2026-10-02-hololive-api-implementation/dispatch-count-plans.txt), [H3 비용](evidence/2026-10-02-hololive-api-implementation/h3-transport-cost.txt), [Holodex/초기 합성 실행](evidence/2026-10-02-hololive-api-implementation/initial-h3-holodex-probes.txt). 마지막 파일은 최초 H3 임시 시험의 package-name 오류도 그대로 보존하며, H3 결과는 수정 후 별도 성공한 `h3-transport-cost.txt`를 기준으로 합니다. 새 성능 gate나 timing 합격선을 추가하지 않았습니다.

### 이전 통합 검증 — 후속 수정 전

- `RUN_INTEGRATION_TESTS=true RACE_TEST_PARALLEL=2 bash scripts/ci/local-ci.sh`: **exit 0**. Architecture·민감 로그·toolchain·workspace drift·모듈별 tidy 및 `GOWORK=off` vet·일반/integration-tag vet·Staticcheck·golangci-lint(**0 issues**)·NilAway·제품 build를 통과했습니다.
- 같은 전체 실행에서 일반 Go 테스트 **242 package**, `-race -count=1` **242 package**가 통과했습니다. 기존 Node helper 시험, collector production JSON/production workspace/AP manifest/PostgreSQL capacity/YouTube plane budget 검사도 통과했습니다. 전체 로그: `/tmp/hololive-full-refactor-local-ci-verified.log`.
- 격리 PostgreSQL 18.6·Valkey에서 dispatchoutbox/batchrepo integration-tag 시험과 LLM summarizer·worker YouTube dispatch·collector joblease 통합 그룹을 실행했습니다. 시험이 종료된 뒤 같은 명령이 테스트 서비스를 정리하고 `[LOCAL CI] Passed`로 끝났습니다.
- 전체 실행 외에 admin dispatchops 기존 8개 integration race, 실제 worker 응답 유실·동시 적용·0분 거절, settings 파일 쓰기 실패, stats 취소, plane의 HTTP/H3/background 종료 회귀를 통과했습니다. stack DB access policy 검사도 통과했습니다.
- 6모듈 canonical tidy/-diff, 3제품 sparse readonly build와 collector의 `GOWORK=off CGO_ENABLED=0`·빈 tag·`-pgo=off` 생산 빌드, 3 Docker Go builder stage(Linux AMD64)가 통과했습니다. peer module은 metadata만 build context에 포함했으며 제품 dependency graph의 cross-runtime peer 구현 import는 0입니다. ARM64 실행이나 운영 배포 검증으로 해석하지 않습니다.
- Iris Console OpenAPI·생성 검증기·SDK와 0/1/1440/1441 경계 시험을 이행했습니다. 저장소의 QUIC Node 환경을 지정한 `bash scripts/verify-all.sh`가 **exit 0**이며 frontend 201·contract 9·Hololive 135·gateway 250개 시험, 서버 줄 coverage 95.51%, 브라우저 658+381+36개가 통과했습니다. 기존 CDP 전용 제외 2개는 그대로입니다. 최초 Firefox timeout은 격리 3회와 전체 재실행에서 통과했으며 timeout·skip 조건을 바꾸지 않았습니다. 로그: `/tmp/iris-console-advance-verify-final.log`.
- 당시 리뷰에서 확인한 결함을 해결한 결과입니다. 이후 적대적 리뷰의 7건은 위 후속 수정 범위에서 별도로 추적합니다. 변경 대상 현재 문서의 경로 127개를 확인했고 두 저장소의 `git diff --check`가 통과했습니다. 기존 미커밋 변경을 보존했으며 commit·push·게시·배포는 수행하지 않았습니다.
- Fallback delta: none. 합성 성능은 운영 개선율이나 timing 합격선이 아닙니다. A3·Node·최상위 경로 이동, H3/local 전환 및 Holodex 오류 공유는 앞의 근거대로 미채택이며 미완료 필수 작업이 아닙니다.

### 1차 구현 기록

- 1차 구현 범위: 1단계의 settings 저장 순서·stats 취소·alarm advance 결과 포트와 2단계의 A1 HTTP helper 흡수·formatter 전달 함수 제거·collector pagination 이관입니다.
- 기존 미커밋 변경을 보존하며 현재 서브모듈에서 파일별 소유 범위를 나누어 구현했습니다. Go 1.27.1 대상 Modern Go Guidelines CLI를 적용했습니다.
- 설정 파일 쓰기 실패에서 이전 disk 값은 유지되지만 Get은 새 값으로 바뀌던 회귀를 실제 파일 접근으로 재현한 뒤 수정했습니다. 저장 실패 후 Get·disk 보존과 worker 미호출을 검증했습니다.
- stats는 단일 refresh의 완료 채널을 context와 함께 기다립니다. TTL·clone·실패 뒤 다음 요청의 재수집을 유지하며 완료와 취소가 겹쳐도 취소된 대기자는 오류를 반환합니다.
- alarm advance는 local/remote/HTTP/caller/mock을 결과+error 포트로 전환했습니다. 전송 전 거부·worker의 인증/입력 거부·전송 후 결과불명을 구분하고 원래 오류를 보존합니다. 실제 worker 적용 뒤 응답 연결을 끊는 시험에서 파일과 worker의 적용값, admin 200 및 unknown 이유, target 생략과 후속 GET의 과거 cache 미노출을 확인했습니다. 동일 client의 동시 PUT은 취소 가능한 방식으로 직렬화하여 늦은 성공 응답이 unknown을 덮지 않게 합니다.
- A1 HTTP helper와 pagination은 실제 소비자 package로 흡수했습니다. worker formatter 전달 함수는 제거하고 실제 dispatchrun이 기존 shared 구현을 직접 호출하며 오류 문맥을 보존합니다. 기존 동작 시험을 실제 소비자 경로로 옮겼습니다.
- 이 시점에는 0분의 제품 의미와 후속 lifecycle·구성·worker/observation 이관·성능 항목이 남아 있었습니다. 현재 결정과 진행은 위 통합 구현 기록을 따릅니다.
- Fallback delta: none. 자동 재시도·polling·파일 rollback을 추가하지 않았습니다. 게시·배포·운영 설정 변경은 수행하지 않았습니다.

### 1차 실제 검증

- API/shared/worker/collector 네 모듈의 workspace 제품 `go build` 통과. API·worker의 `GOWORK=off go build ./...`와 collector `public-pr-go-gate.sh ... build-prod`도 통과했습니다. collector는 CGO 비활성·빈 tag·PGO 비활성 조건과 산출물 검사를 유지했습니다.
- 네 모듈의 무태그 및 `-tags=integration` 테스트 패키지 컴파일을 `go test -p 2 -run '^$'`로 확인했습니다. 이는 전체 테스트 실행이나 전체 DB integration 실행 결과가 아닙니다.
- settings 저장서비스 전체·API settings 실패 회귀·stats 전체·bot HTTP lifecycle 대상 race, collector collectutil/youtubejscollector 전체 race, worker 실제 formatter 렌더 회귀 3개 race가 통과했습니다. bot bootstrap/http package의 기존 시험도 통과했습니다.
- 알림 포트의 owner/caller 8개 package 선택 회귀 race가 통과했습니다. 실제 local service·HTTP adapter/client의 성공·인증/입력 거부·원래 전송 오류·취소·응답 유실과 실제 admin 저장/조회 경로를 검증했습니다. 동시 PUT과 대기 취소는 살아 있는 두 요청의 순서를 synctest로 제어했습니다.
- 변경 대상 package의 Stage 3 lint(`0 issues`)·Staticcheck·NilAway, `scripts/architecture/ci-boundary-gate.sh`, `git diff --check`가 통과했습니다. 린트 설정을 완화하지 않았습니다. 최종 리뷰에서 동시 PUT의 unknown cache 덮어쓰기와 그 회귀 공백을 해결한 뒤 추가 finding이 없음을 확인했습니다.
- 전체 `local-ci.sh`, 전체 모듈 테스트/race, 운영 성능·배포 검증은 이 1차 결과에 포함하지 않습니다. 후속 통합 검증과 구분합니다.

## 범위와 유지할 계약

- 단일 API 프로세스와 bot/admin/LLM/YouTube plane, plane별 bounded DB pool, 세 종류 outbox 및 collector/API/worker의 저장·발송 소유권을 유지합니다.
- 초기에는 `hololive/hololive-*`, Go module identity, runtime binary·service·port, migration 위치와 설치 경로를 유지합니다. 단일 go.mod 통합은 이 계획의 기본 범위가 아닙니다.
- 내부 이관은 실제 caller·구현·SQL·embed·테스트·경로 소비자를 같은 변경에서 갱신하고 구 alias/forwarder를 제거합니다. 파일 이동과 SQL 의미·저장 schema·재시도 정책 변경을 한 패치에 섞지 않습니다.
- 공개 HTTP/JSON·설정값, 오류 identity/wrapping, 관측 generation·hash·lease·receipt·replay·DB ACL을 보존합니다. 퇴역 env 거절, 공개 입력 alias, 보관 payload 필드는 소비자·드레인 근거 없이 삭제하지 않습니다.
- 기존 미커밋 작업을 보존합니다. 실제 운영·배포 절차는 current runbook이 소유하며 이 계획이나 과거 문서로 대체하지 않습니다.

## 중복 제안과 충돌을 정리한 결정

| 주제 | 통합 결정 |
|---|---|
| observation API 목적지 | `internal/youtube/sourceobservation`. replay-epoch cmd가 접근하지 못하는 `planes/youtube/internal`은 사용하지 않습니다. |
| API 공통 foundation | `internal/apifoundation` leaf. root `internal/app`을 plane에서 import하여 cycle을 만들지 않습니다. |
| formatter | 새 formatter 구현 package를 추출하지 않습니다. 기존 shared `outbox/format`을 직접 호출하고 오류 문맥을 보존합니다. 이후 worker 회수에서 **그 기존 구현 자체**만 이동할 수 있습니다. |
| DB factory | `shared/pkg/providers/database`로 DB 구성 책임을 추출합니다. 순수 `service/database`에 startup settings 의존을 추가하지 않습니다. |
| shared HTTP | 공통 서버 수명과 health/ready는 유지하고 admin 기능 handler와 API trigger 조립만 소유 모듈로 옮깁니다. 내부 H3의 in-process 전환과는 별도입니다. |
| worker alarmdispatch | 기존 `dispatchrun` runner를 `internal/egress/alarmdispatch`로 이동하는 일과 shared `dispatchoutbox` 저장 구현 회수는 다른 책임입니다. 양쪽을 하나의 큰 package로 합치지 않습니다. |
| observation 테스트 수 | 교차 시험 **32개 파일**과 기존 이름 기반 method hint **124개 함수**는 다른 집계입니다. 자동 hint를 이관 owner로 사용하지 않습니다. claim SQL 시험 두 파일은 API/교차 소유입니다. |
| API 경로 개편 | A1 단일소비 helper 흡수는 우선 후보, A2 admin runtime/HTTP 경계는 구성 변경과 조정, A3 namespace 단축은 후순위 선택입니다. A3만으로 fan-out이 줄었다고 보지 않습니다. |
| 검증 결과 | 원본·overlay·임시 사본, package load·compile·test execution·race를 구분합니다. 기존 통과 기록은 통합 변경의 최종 통과 기록이 아닙니다. |

## 확보한 검증과 아직 증명하지 않은 것

아래는 각 보고서가 기록한 조사 당시 결과입니다. 이번 문서 통합에서 재실행하지 않았으며, 구현 완료로 체크하지 않습니다.

| 근거 | 실제 확인 범위 | 한계 |
|---|---|---|
| API 오류·수명주기 | 저장 실패/취소, 적용 뒤 응답 유실, 0 입력, 중복 Close·sampler join·종료 시한 재현; 대상 race 통과 | 운영 장애 발생량·전체 race/NilAway는 미확인 |
| 성능 | 병렬5·기간 선택률 matrix, lazy/prepared 결과·순서 동등성, CPU/할당, summary 갱신 전후 plan | 합성 데이터이며 운영 처리량·H3/local 비용을 입증하지 않음 |
| package graph 6조건 | 기본/integration tests, ARM64 제품, GOWORK=off command별 로딩에서 Error/DepsErrors 0 | ARM64 실행·전체 test/build 통과와 다름 |
| API A1+A2+A3 임시 이동 | 84파일 이동 후 API 전체 제품 build, 하위 사례 포함 선택 시험45개 통과 | 원본 이관·전체 DB/race 검증 아님 |
| worker alarmdispatch 임시 이동 | 제품13·테스트25·SQL6 이동 후 제품 build·test binary compile | 기존 DB 시험 실행 안 함 |
| collector pagination 임시 이동 | 제품 build·test binary compile·기존 impossible-tuple 회귀 통과 | collector 전체 책임 분리 완료 아님 |
| admin DB fixture 대안 | 기존8개 integration 시험이 현재 전체 migration의 NewPool에서 통과 | 다른 fixture까지 자동 전환 가능하다는 뜻은 아님 |
| Node 경로 부정 실험 | nested test 누락, strict 목록 누락, helper fixture 경로 단절을 재현 | 현재 평면 배치의 제품 결함으로 일반화하지 않음 |

근거는 [API 확장 자료](evidence/2026-10-02-hololive-api-expansion/), [경로 개편 자료](evidence/2026-10-02-repository-layout/)와 각 보고서에 보존합니다. embed557개·검토한 경로 계약29개는 이동 검토 범위이며 새 구조 gate가 아닙니다. DB factory와 HTTP 기능 edge를 함께 끊는 가설의 **기존 shared 도달51→36**도 새 package를 제외한 반사실 그래프이며 실제 build/성능 결과가 아닙니다. DB 구성만 분리한 값은51→48입니다.

## 실행 순서와 완료 기준

아래 번호는 권고 순서이며 단계별 승인·증명 gate를 새로 만드는 것이 아닙니다. 각각의 변경은 실제 선행 조건만 충족하면 독립적으로 진행할 수 있습니다. 대규모 일괄 변경은 한 책임 경계의 모든 caller를 완결되게 바꾸는 범위로 제한합니다.

### 1. 동작과 설정 결과 계약 복원

- [x] settings Update를 persist-before-publish로 바꾸고 저장 실패에서 이전 Get·disk 값과 worker 미호출을 검증합니다.
- [x] stats refresh 대기를 context-aware로 바꾸되 TTL·clone·단일 refresh 의미를 유지합니다.
- [x] alarm advance의 local/remote 구현·HTTP adapter·caller·mock을 결과+error 포트로 함께 바꿉니다. 적용 확인·확정 거부·전송 후 결과불명을 구분하고 오류 없는 old method를 삭제합니다. 자동 재시도·파일 rollback은 추가하지 않습니다.
- [x] 사용자 승인에 따라 공개 입력을 `1..1440`으로 확정하여 저장서비스의 기존 양수 조건과 하한을 맞췄습니다. 파일 저장서비스 자체의 기존 양수 검증 범위는 유지합니다. 0은 저장·worker 호출 전 400이며 clamp·기본값 치환은 없습니다. Console OpenAPI와 생성 client도 같은 하한입니다.

완료 기준은 실제 handler/client/store의 실패·성공·응답 유실 회귀입니다. stub이 반환하는 성공만으로 저장·원격 적용을 검증하지 않습니다. 0분 결정은 다른 세 수정의 준비를 막지 않습니다.

### 2. 자원 소유권과 작은 패키지 결합 정리

- [x] bot/admin은 획득 즉시 rollback에 등록하고 성공 시 같은 owner를 runtime에 이전합니다. admin router/server 실패에서 final cleanup을 건너뛰지 않습니다.
- [x] bot orchestration의 DB/cache Close와 Holodex 소유권을 plane으로 옮깁니다. readiness는 Close 없는 포트로 유지하고 실제 command/room repository의 DB 입력은 보존합니다.
- [x] durable sampler를 포함한 관련 background task를 cancel/join합니다. 정상 Stop 성공과 join 미완료를 구분하고 미종료 작업의 DB/cache를 무조건 닫지 않습니다. 공유10초 drain·30초 hard deadline·병렬 이중 cleanup 금지는 유지합니다.
- [x] **A1:** bot `internal/app/runtime/http_server.go`를 `runtime/http_server_helpers.go`로 흡수합니다. nil·로그·오류 wrapping을 보존합니다. raw H3 종료 동작 변경은 정상/vanished/in-flight 구분을 검증하는 별도 행동 변경으로 다룹니다.
- [x] worker의 formatter 전달 함수는 기존 shared formatter 직접 호출로 바꿉니다. 전달 층이 붙이던 오류 문맥도 유지합니다.
- [x] collector `collectutil.PaginationOf`와 impossible-tuple 회귀를 `youtubejscollector/pagination*`로 옮기고 세 caller를 수정합니다. `collectutil`을 곧바로 generic types package로 재명명하지 않습니다.
- [x] parser 함수 alias11개, 불필요한 타입 alias·테스트 전용 wrapper/initializer·확인된 standalone Run을 제거합니다. 실제 parser/명령/lifecycle 회귀와 settings 파일 기동 검증은 유지합니다.

완료 기준: Start 전 Close, 획득 단계별 exactly-once rollback, member-cache 종료 후 PG/cache 해제, sampler까지 join, fatal/error 합산 및 timeout 상태 보존, 실제 worker profile settlement 예산 검증입니다. 대상 race를 실행합니다. 이 단계는 alias·작은 이동과 lifecycle 동작 변경을 하나의 거대한 패치로 묶으라는 뜻이 아닙니다.

### 3. API 구성과 shared의 두 의존 경로 분리

- [x] strict parser/policy의 실제 구현을 접근 가능한 leaf로 이동하고 API·worker·collector의 해당 import를 함께 수정합니다. blank/default·required blank 거절·오류 합산·마스킹·duration overflow를 보존하고 old internal/load alias를 남기지 않습니다.
- [x] apiplane 구현·테스트를 API `internal/config`로 옮기고 cmd/Fx/app/LLM/YouTube caller를 전환합니다. 공통 생성 코드는 `internal/apifoundation`으로 모으고 plane별 인스턴스·pool을 유지합니다.
- [x] `BuildInfraModule`은 실제 Valkey/Postgres options만 받게 하여 API bot/admin·worker 세 caller를 바꿉니다. `BotDependencyModules → Dependencies → six views`의 단순 전달과 미사용 슬롯을 없애되 commandInitView의 실제 조립은 유지합니다.
- [x] 내부 H3 옵션을 alarm, major-event/member-news, trigger, bot-room, system-health constructor에 전달합니다. transport 수명과 timeout은 각 plane이 계속 소유합니다. Iris URL 파일의 동적 reload는 유지합니다.
- [x] `DatabaseResources`, `ProvideDatabaseResources`, 기존 DB provider 테스트를 `shared/pkg/providers/database`로 추출하고 실제 caller를 전환합니다. settings→DB config 변환과 cleanup 계약을 유지합니다.
- [x] shared HTTP의 Stream/OAuth/WebSocket 기능은 admin 내부로, TriggerHandler와 `NewTriggerRuntimeRouter`·route registrar는 API 공통 internal로 옮깁니다. shared health/ready도 쓰는 private JSON 응답 primitive와 공통 H3/metrics/pprof는 남깁니다. 혼합 health/trigger 시험은 함수 단위로 나눕니다.
- [x] domain 시간 계산과 Valkey helper, target-minute 순수 정책과 checker 구현을 분리합니다. AlarmCRUD는 실제 소비자별 좁은 포트로 바꾸고 remote cache-warm no-op을 제거합니다.

**A2**의 admin `app → runtime`, `app/http → internal/httpapi` 이동은 admin 구성·HTTP 소유권 변경과 같은 최종 목적지로 조정합니다. 같은 파일을 임시 package를 거쳐 반복 이동하지 않습니다. 다만 동작을 바꾸는 commit과 이름만 바꾸는 commit은 구분해 검증할 수 있습니다.

완료 기준: 동일 설정의 listener/pool/profile·CORS/version 기본값·validation 순서, HTTP auth/응답·health/readiness, 모든 실제 consumer build/test와 설정 실패 회귀입니다. DB provider만 분리하고 HTTP→Holodex 결합까지 해소했다고 선언하지 않습니다. factory 분리, client options 주입, H3/local 전환은 서로 다른 작업입니다.

### 4. worker 구현과 egress 배치

- [x] 기존 `internal/service/dispatchrun` 제품13·테스트25·SQL6을 `internal/egress/alarmdispatch`로 옮기고 workerapp 및 실제 경로 소비자를 바꿉니다.
- [x] shared의 worker 전용7 package/제품59파일·테스트58파일·SQL32를 회수합니다. dispatchoutbox+queue와 alarmservice+private cache는 내부 의존을 함께 이행합니다.
- [x] alarmservice는 worker `internal/service/alarm/subscriptions`, private cache는 그 내부에 둡니다. 기존 shared formatter 자체를 worker 소유의 `internal/egress/youtubedispatch/format`으로 이동하고 alarmdispatch와 youtubedispatch가 직접 사용합니다. 별도 formatter 추출·복제는 하지 않으며 deliverysql·timeline·checker의 실제 공용 부분은 그대로 둡니다.
- [x] bot InvalidAction 시험은 기존 stub을 쓰고, dbtest의 실제 repository 시험은 worker로 옮깁니다. DB CHECK 시험은 dbtest에 남깁니다. admin dispatchops는 과거 fixture loader 대신 기존 `dbtest.NewPool`을 사용합니다.
- [x] UpcomingCandidates를 쓰는 canonical clock 교차 시험은 worker 소유로 배치하고 실제 publish/consume 경로를 유지합니다.

worker config 회수는 strict leaf 접근성만 선행하면 됩니다. alarmdispatch 배치와 alarmservice 회수는 서로 독립적입니다. worker `DeliveryExecutor`의 private `dispatchDeliveryRows` 때문에 claim/send를 기계적으로 별도 package로 나누는 안은 이 단계에서 제외합니다. 단순 export 확대 대신 상호 보관·호출 흐름을 별도로 설계해야 합니다.

### 5. observation의 완결된 소유권 이관

- [x] API `internal/youtube/sourceobservation`, collector `internal/runtime/sourceobservation`으로 실제 구현을 나눕니다. 46개 파일은 collector8/API34/분해4, private community/reducer7 package의 제품23·테스트21도 함께 이동합니다.
- [x] SQL85개는 collector13/API72 embed로 **내용 그대로** 보존합니다. 실제 literal 소비는 collector8/API71, test-only1, 미확정5이며 미확정 SQL을 삭제하지 않습니다.
- [x] collector는 JobContract/checkpoint/publish/fence/defer를, API는 supported set/claim/canonical/replay/retention을 소유합니다. 기존 shared envelope·lease·schema·hash·clock은 유지하고 혼합 Repository/inner facade를 제거합니다.
- [x] 직접 caller50파일과 혼합 테스트를 함수 단위로 이행합니다. 기존46개 테스트 파일 중 교차32·collector7·API5·helper 미확정2라는 수동 검토를 반영합니다. `claim_backlog_plan_test.go`, `shorts_claim_plan_test.go`는 publisher로 seed하지만 API claim SQL을 검사하므로 collector-only로 옮기지 않습니다.
- [x] 양 구현을 쓰는 회귀는 좁은 무태그 module-root testkit 또는 같은 검증력을 가진 방식으로 연결합니다. private fault hook을 공개하지 않고 기존 무태그 시험을 integration 태그 뒤로 숨기지 않습니다. publisher-only 잠금/FK 시험에는 API consume을 억지로 연결하지 않습니다.

완료 기준: 실제 양 구현의 DB 시험에서 publish/checkpoint/complete/defer, canonical/intent/receipt/offset의 원자성, 실패 우선순위, stale token·fencing, Shorts 순서, generation/replay epoch, payload dictionary/GC/FK/ACL을 보존합니다. 운영 replay나 epoch 활성화를 검증으로 실행하지 않습니다.

worker 회수와 observation 이관은 어느 쪽부터도 가능합니다. canonical clock 시험의 owner/import를 해당 단계에서 함께 바꾸면 됩니다. API 구성 전체나 성능 작업을 불필요한 선행 gate로 두지 않습니다. 상세 [Go·SQL·caller·테스트 이동표](2026-10-02-hololive-api-deep-analysis.md#observation-이관의-파일과-테스트-배치를-확정합니다)를 사용하되 자동 method hint보다 수동 claim 시험 검토를 우선합니다.

### 6. 동작 보존 범위부터 성능 개선

- [x] 정확 token으로 해결되지 않는 profile에만 본문을 정규화합니다. 방별 조회·clock·profile·validator·category·순서를 유지하고 전체 결과값·순서를 검증합니다.
- [x] member-news 기간 SQL은 날짜 우선순위·KST 경계·date/timestamptz·동일 now·non-finite 오류·동률/fallback 순서를 보존합니다. 병렬5와 선택률1/10/100%를 비교합니다.
- [x] run snapshot+prepared metadata는 뉴스·방 멤버·alias/validator의 관측 시점과 immutable 값·실패 단위를 정한 뒤 적용합니다. SQL·Go filter·요약에 같은 now를 전달합니다.
- [x] dispatch count(*)는 VACUUM 직후와 갱신 후 plan/heap fetch/pool wait를 모두 확인합니다. 보존 범위 축소나 stale cache는 추가하지 않습니다.
- [x] Holodex 오류 공유와 H3/local binding은 별도 비용·실패 계약 뒤 결정합니다. trigger caller30초/실행5분, major lock409/member-news skip200 및 기존 DTO/error/auth 의미를 보존합니다.

이 단계의 좁은 CPU·SQL 작업은 4·5단계 완료를 기다릴 필요가 없습니다. 성능 prototype 동등성이나 합성 개선율을 운영 성능·완료된 제품 수정으로 해석하지 않으며 여러 개선율을 합산하지 않습니다.

### 7. 선택적 namespace와 Node 디렉터리 개편

- [x] **A3 검토 완료·미채택:** 추가 namespace 이동의 제품 이득이 확인되지 않아 현재 경로를 유지합니다. 향후 필요할 때 bot `internal/bot/orchestration → internal/orchestration`과 부모 시험의 orchcmd 배치를 함께 옮깁니다. namespace 단축을 이유로 messaging/handlers/LLM service까지 평탄화하지 않습니다.
- [x] **Node 개편 검토 완료·미채택:** 현재 평면 구조와 시험·설치 경로를 유지합니다. 향후 Node 하위 폴더를 도입하면 recursive test discovery와 기존 순차 실행, strict 파일 목록과 relaxed 제외 목록, Go helper/protocol fixture 경로를 같은 변경에서 갱신합니다. 기존 테스트와 strict 검사 범위를 유지합니다.
- [x] **helper source 위치 유지:** 이번에 source 이동은 없습니다. 향후 helper source 이동 시 npm cache/audit/typecheck 선택, Dockerfile source/whitelist, AP rsync/native artifact 필터를 실제 consumer별로 수정합니다. collector의 재귀 test 제외는 이미 있으므로 worker/native flat prune 문제와 구분합니다.

기본안은 현재 helper source·설치 위치 유지입니다. `helpers/*` source 이동을 선택해도 `/app/youtubejs`, `/app/xspaces`, `/app/po-sandbox` 및 native `youtubejs` 설치 경로는 유지합니다. collector app/collection/providers 재배치도 조립 결합이 해소된 뒤 필요를 판단합니다. 이미 이관한 구현을 namespace 정리로 다시 옮기는 비용을 비교합니다.

### 8. 최상위 물리 경로는 마지막 조건부 선택

**결정: 최상위 경로는 유지합니다.** 현재 module 위치에서 소유권 분리가 완료됐고 추가 이동의 제품 이득이 확인되지 않았습니다. `apps/{api,alarm-worker,youtube-collector}`, `libs/hololive`, `tests/dbtest`는 기본 목표가 아닙니다. 필요가 남으면 module 위치 안정성의 현재 tree policy를 개정하고 파일 이동만의 별도 변경으로 수행합니다. module identity까지 동시에 바꾸거나 새 경로 registry를 만들지 않습니다.

정확한 `go.work` use·sibling replace·cmd package identity·build-id verifier·Docker COPY/ignore·AP manifest·CI test/integration/race·npm 경로 소비자를 함께 갱신합니다. 깊이가 같은 `hololive/hololive-api → apps/api` 이동에서 `../../.golangci.yml` 같은 상대 깊이 참조까지 일괄 치환하지 않습니다. collecterr import path가 바뀔 때만 부모 lint template의 해당 예외를 갱신합니다. [직접 검토한 경로 계약29개](evidence/2026-10-02-repository-layout/reviewed-path-contracts.tsv)를 사용하되 lexical 검토 집합의 모든 행을 수정 목록으로 취급하지 않습니다.

## 공통 검증과 완료 판정

- 변경 전 현재 코드·실제 caller·파일/SQL/embed 자산을 다시 확인합니다. 다른 조사 당시의 HEAD·dirty tree와 같다고 가정하지 않습니다. 제품·same-package/external test·integration tag를 함께 추적합니다.
- 각 실제 변경의 모듈 build, 기존 행동 시험·해당 race/NilAway/staticcheck, 기존 architecture 검사와 Stage3/prerequisites를 유지합니다. 단순 이름/파일수/removed-name grep나 checker 자체 시험을 새 gate로 만들지 않습니다.
- DB/fencing/replay/receipt 변경은 격리 PostgreSQL의 실제 구현 회귀로 검증합니다. dispatchops는 `-tags=integration`을 명시하고, query plan 시험은 반환값·읽은 행/page·동시성·갱신 상태를 함께 비교합니다.
- Node 이동은 실제 npm test·strict/relaxed 검사와 helper RPC 회귀를 실행합니다. syntax·test binary compile을 테스트 실행으로 보고하지 않습니다.
- 제품 build 조건을 보존합니다. collector production은 GOWORK=off·CGO_ENABLED=0·빈 tag·-pgo=off입니다. 새 codec/tag/dependency 업그레이드를 경로 이동에 섞지 않습니다. 필요 시 kapu에서 `./build-all.sh --build-only --no-bump`를 사용하며 배포 스크립트를 검증 명령으로 실행하지 않습니다.
- `PROJECT_MAP`, `SERVICE_OWNERSHIP`, 관련 API/alarm/membernews/observation 계약과 실제 build/deploy 경로 소비자를 이행합니다. 현재 없는 package 경로와 역사 runbook을 근거로 사용하지 않습니다.
- 원본 적용 뒤의 최종 결과로 완료를 판정합니다. 새 runtime dependency·공개 계약 파괴·운영 데이터/설정·게시·배포는 별도 권한 범위를 따릅니다. 게시가 요청된 경우에만 기존 pre-push 검증을 수행합니다.

## 결정 결과

0분은 승인된 400 거절로 확정했습니다. 결과불명은 기존 JSON의 `alarm_applied=false`와 명시적 unknown reason으로 구분하고 target 값을 생략합니다. run snapshot은 시작 시각·공통 후보만 고정하고 방 멤버·alias는 방별 관측을 유지합니다. 비용·오류 계약 검토에 따라 H3, `count(id)`, Holodex의 독립 caller 실패 예산, Node·최상위 경로는 유지합니다. private claim/send 분리는 처음부터 계획의 제외 범위입니다. 상세 근거와 수치는 위 구현 기록을 따릅니다.

## 최초 API 조사 근거

다음은 통합 전 API 조사와 당시 제안입니다. 코드 근거·최초 재현을 보존하며, 현재 작업 범위·목표 경로·순서는 위 통합 계획을 따릅니다. 추가 실행 결과는 두 심층 보고서가 소유합니다.

### 결론

현재 API는 단순 HTTP 서버가 아니라 bot ingress/reply, admin, LLM scheduler, YouTube observation consume을 함께 호스팅하는 **모듈형 모놀리스**입니다. plane 간 직접 Go import는 분리되어 있고, Fx가 프로세스 생명주기를 소유합니다. 문제는 통합 이후에도 남은 독립 서비스식 구성·내부 HTTP 호출, 지나치게 넓은 shared 패키지, 실패 결과를 충분히 표현하지 못하는 일부 경계입니다.

- 유지: 단일 프로세스, plane 경계, plane별 bounded DB pool, collector/API/worker의 데이터·발송 소유권, 현재 외부 HTTP·DB·설정 계약.
- 우선 수정: 설정 저장 실패의 메모리 선반영, 통계 조회 대기의 context 무시.
- 우선 정리: 테스트만 호출하는 wrapper/runner, bot/admin foundation 중복, API 전용 설정의 shared 배치, `domain → util → Valkey` 의존.
- 큰 구조 변경: observation publish/consume 구현의 소유 모듈 이관. 파일 이동보다 트랜잭션·lease·receipt·replay 경계 보존이 먼저입니다.
- 성능 개선: member-news 공통 후보 조회, Holodex cache-fill 실패 경로, 내부 H3 왕복을 각각 측정한 뒤 독립적으로 진행합니다. 처리량·운영 p99 개선율은 아직 주장하지 않습니다.

### 조사 범위와 실제 검증

`hololive-api`의 entrypoint·Fx·plane 조립, bot 의존성/영속 reply, admin 설정/통계/dispatch 조회, LLM member-news 생성·scheduler, YouTube runtime과 관련 `hololive-shared` 구현을 읽었습니다. API·shared·alarm-worker·collector 전체의 Go package metadata로 direct/transitive import를 조사했습니다. 모든 함수·SQL을 전수 검토한 보안 감사나 운영 부하 측정은 아닙니다.

`go1.27.1 linux/amd64`, 저장소 `go.work` 환경에서 `go list -json`을 실행했습니다. 숫자는 해당 플랫폼에서 선택되는 `GoFiles`, `TestGoFiles`, `XTestGoFiles`이며 command·migration·test helper package도 포함합니다. 코드 크기를 품질 판정이나 새 gate로 사용하지 않습니다.

| 모듈 | package | GoFiles | test Go files |
|---|---:|---:|---:|
| hololive-api | 72 | 348 | 328 |
| hololive-shared | 98 | 528 | 444 |
| hololive-alarm-worker | 21 | 141 | 152 |
| hololive-youtube-collector | 14 | 72 | 66 |

API의 plane별 GoFiles는 bot 159, admin 52, LLM 82, YouTube 16입니다. YouTube 파일이 적다는 것이 구현이 단순하다는 뜻은 아닙니다. 핵심 consume·canonical persist가 shared에 있습니다.

실행 결과:

```text
settings: update_error=true memory_minutes=10 expected_unchanged=5
stats: cancelled_waiter_returned_before_release=false elapsed_ms=200 error=<nil>
```

- 임시 Go 실행 프로그램에서 실제 `settings.Service`와 `system.Collector`를 호출했습니다. 설정 probe는 자신이 만든 임시 경로에서 rename 실패를 유발했습니다. 통계 probe는 로컬 HTTP 서버의 응답을 막은 뒤 취소된 후속 요청이 먼저 반환하는지 관찰했습니다.
- 명령: 저장소 루트에서 `go -C hololive/hololive-api run ./internal/planes/admin/cmd/architectureprobe`. 조사 후 임시 소스를 제거했습니다. DB·Valkey·Iris·외부 원천을 호출하지 않았습니다.
- gopls references로 LLM standalone `Run`과 bot bootstrap wrapper의 테스트 전용 참조를 확인했습니다. bot/admin `Run`에는 generic interface 호출 참조도 있으므로 이름 일치만으로 전부 미사용이라고 판정하지 않았습니다.
- 전체 API 서버 기동, DB EXPLAIN, benchmark suite, 전체 test/race/lint, 운영 조회는 수행하지 않았습니다. 기존 문서의 검증 결과를 이번 실행 결과로 계산하지 않습니다.

### 현재 구조와 유지할 계약

```text
cmd/hololive-api
  └─ fxapp: process signal / fatal / telemetry / resource owner
      └─ app.Runtime
          ├─ bot: webhook → durable inbox → command → durable reply → Iris
          ├─ admin: 관리 HTTP → 조회/설정/trigger adapter
          ├─ llm: 요약·구독·scheduler → notification_delivery_outbox
          └─ youtube: observation claim → reconcile → canonical / notification intent

collector → source_observation → API youtube
API llm/youtube → 영속 notification intent → alarm-worker → proactive Iris/Kakao egress
```

근거: [API runtime](../../hololive/hololive-api/internal/app/runtime.go#L63), [Fx composition](../../hololive/hololive-api/internal/fxapp/application.go#L116), [현재 소유권](../current/PROJECT_MAP.md#cross-runtime-contracts).

중요한 구분:

1. Bot reply의 Iris egress는 API 책임입니다. proactive notification egress만 alarm-worker 책임입니다. 모든 egress를 worker로 보내는 재설계는 이번 제안이 아닙니다.
2. LLM digest의 `notification_delivery_outbox`와 alarm dispatch ledger, bot reply outbox는 상태 전이와 멱등 identity가 다릅니다. 모두 outbox라는 이유로 통합하지 않습니다.
3. `!라이브`는 이미 DB snapshot을 사용합니다. Holodex 최적화 대상을 `!라이브`로 오인하지 않습니다. `!예정`·`!일정`·Stream HTTP는 별도 원천 계약을 가집니다.
4. [RuntimeConfig](../../hololive/hololive-shared/pkg/config/settings/apiplane/runtime.go#L26)는 plane별 DB pool을 explicit bulkhead로 명시합니다. bot/admin/LLM의 기본 max는 각각 4이며 YouTube에는 별도 pool이 있습니다. 실제 운영 connection 수를 읽은 것은 아닙니다.
5. [trust-domain 결정](../current/architecture/hololive-api-trust-domain.md)은 하나의 프로세스를 하나의 신뢰 영역으로 취급합니다. 디렉터리나 interface를 나누는 것으로 credential isolation이 생기지 않습니다.
6. 기동 순서 YouTube→LLM→admin→bot, 종료 순서 bot→admin→LLM→YouTube와 10초 plane-drain/30초 process-stop 계약을 유지합니다.

### 우선순위별 발견 사항

P1은 먼저 고칠 동작 결함, P2는 소유권·실패 경로·확인된 비용 구조, P3는 측정 후 진행할 최적화입니다. 운영 사고 발생이나 exploit 가능성을 뜻하는 등급은 아닙니다.

#### P1 — 설정 저장 실패와 메모리 상태가 불일치합니다

**확인:** [settings.Service.Update](../../hololive/hololive-shared/pkg/service/settings/service.go#L164)는 `s.cache`를 바꾼 뒤 `persistCache()`를 호출합니다. encode/write/sync/rename이 실패해도 cache를 원복하지 않습니다. admin [UpdateSettings](../../hololive/hololive-api/internal/planes/admin/internal/server/api/settings_handler.go#L208)는 오류 시 500을 반환하고 runtime 적용을 건너뜁니다.

**재현:** 초기 5분 설정을 만든 뒤 임시 목적 경로를 디렉터리로 바꿔 rename을 실패시켰습니다. Update는 오류지만 Get은 10분을 반환했습니다. 이 probe는 모든 disk 실패를 시뮬레이션한 것은 아니며, 실패 시 메모리 선반영이라는 원인을 확인합니다.

**영향:** 실패 응답 이후 관리 조회가 미저장 값을 보여줄 수 있습니다. 저장 값·admin 메모리·worker 적용 상태가 달라질 수 있습니다. 운영에서 발생했다는 증거는 없습니다.

**제안:** 검증·정규화한 next 값을 지역 변수로 준비하고, next를 temp file에 기록·sync·close·rename한 뒤에만 메모리 snapshot을 교체합니다. 기존 write serialization을 유지하고 이번 수정에서 비동기 저장이나 새 자동 재시도를 도입하지 않습니다. `persistCache`는 현재 cache를 암묵적으로 읽는 대신 저장 대상 값을 받게 합니다.

**완료 기준:** 각 저장 실패에서 이전 Get 값 유지, 성공 시 disk/Get 일치, 실패 시 worker 적용 호출 없음. 저장 성공 후 원격 worker 적용 실패는 별도 결과이며 로컬 저장 rollback으로 가장하지 않습니다.

#### P2 — 통계 refresh 대기가 요청 취소를 무시합니다

**확인:** [Collector.GetCurrentStats](../../hololive/hololive-api/internal/planes/admin/internal/service/system/stats.go#L120)는 cache miss에서 `refreshMu.Lock()`으로 기다립니다. Lock 대기는 context와 무관하고, 획득 뒤 cache가 생겼으면 ctx 확인 없이 성공 반환합니다.

**재현:** 선행 refresh의 로컬 HTTP 응답을 막은 상태에서 이미 취소된 ctx로 두 번째 호출을 넣었습니다. 후속 호출은 200ms 동안 반환하지 않았고, 선행 요청을 풀자 nil error로 끝났습니다.

**제안:** refresh 진행 상태와 완료 채널만 짧게 잠그고, 대기자는 `ctx.Done()`과 완료를 select합니다. 완료 후 기존 clone/cached TTL 의미를 유지합니다. 요청별 취소 때문에 다른 caller의 refresh를 취소하지 않습니다. 새 polling goroutine·stale fallback은 추가하지 않습니다.

**완료 기준:** 취소된 대기자는 선행 refresh 완료와 독립적으로 반환, 살아 있는 대기자는 동일 cache 갱신 이용, 공유 결과 변이 차단, refresh 실패 뒤 다음 caller의 동작 보존. 비용 개선율보다 cancellation 계약 복원이 목적입니다.

#### P2 — shared가 계약뿐 아니라 API·worker 구성 정책까지 소유합니다

**확인:** shared에서 API/worker/collector 모듈을 직접 import하는 역방향 edge는 0개였습니다. 따라서 실제 문제를 “Go import cycle”이라고 부르면 부정확합니다. 문제는 **책임의 역전과 패키지 단위 의존 오염**입니다.

| 경로 | 확인된 문제 | 목표 |
|---|---|---|
| `pkg/config/settings/apiplane` | API 전용 ports, loopback URL, plane pool, YouTube runtime 설정을 shared가 소유. 조사한 direct consumer 5개 모두 API | API `internal/config`로 이동 |
| `pkg/providers` | DB/Valkey factory와 Iris client/YouTube/Holodex 조립이 한 package | 소유 runtime의 composition으로 이동; 실제 공통 primitive만 남김 |
| `pkg/providers/modules` | `BuildInfraModule(*settings.Config)`가 DB·cache·member cache·cleanup을 묶음 | API 공통 조립과 worker 조립을 분리. 전역 Config를 하위 생성자까지 전파하지 않음 |
| `pkg/service/internalhttp` | transport 생성 시 shared runtime env loader 호출 | composition에서 해석한 H3 options를 전달; 전송 primitive와 앱 환경 정책 분리 |
| `pkg/service/settings` | 파일 저장 서비스가 `service/alarm/checker`의 target-minute 정책을 import | 순수 alarm 정책을 소유 도메인/계약으로 이동, 파일 adapter와 분리 |
| `pkg/domain → pkg/util → valkey-go` | Stream의 시간 계산 한 개 때문에 domain이 Valkey package graph를 가져옴 | 시간 연산을 의존 없는 leaf로 분리하거나 기존 순수 utility 재사용 |

`domain → util → valkey-go`는 [Stream.MinutesUntilStart](../../hololive/hololive-shared/pkg/domain/stream.go#L122)와 [util/valkey.go](../../hololive/hololive-shared/pkg/util/valkey.go#L23)에서 확인됩니다. 호출 시 Valkey 연결을 만든다는 뜻은 아닙니다. 빌드·변경 영향의 경계가 어긋났다는 뜻입니다.

실제 package graph에는 `collector/collectorruntime → shared/providers → shared/service/delivery`도 있습니다. [infra_providers.go](../../hololive/hololive-shared/pkg/providers/infra_providers.go#L23)가 DB/Valkey와 Iris client를 함께 import하기 때문입니다. collector에 발송 권한이 생겼다고 단정하지 않되, 발송을 소유하지 않는 모듈의 compile-time 의존으로는 부적합합니다.

**주의:** API만 직접 import하는 shared package가 18개였지만 이를 모두 API로 옮기면 안 됩니다. `service/settings`, `service/internalhttp`, `repository`, `poller/runtime/batchrepo` 등은 worker/collector가 간접 의존합니다. direct importer 수가 아니라 transitive 소비자와 실제 책임으로 판단합니다. wire contract 역시 Go 소비자가 하나여도 외부 HTTP 소비자가 있으면 공유 계약입니다.

#### P2 — observation publish/consume의 facade만 분리되어 있습니다

**확인:** [repository_roles.go](../../hololive/hololive-shared/pkg/service/youtube/sourceobservation/repository_roles.go#L18)의 `PublishRepository`와 `ConsumeRepository`는 동일 `Repository`를 감싼 facade입니다. 이 package에는 publish·checkpoint·claim·canonical persist·live finalizer·retention·replay가 함께 들어 있습니다. 조사 시 GoFiles 46개였습니다.

API [YouTube runtime](../../hololive/hololive-api/internal/planes/youtube/runtime/runtime.go#L171)은 shared Consumer와 canonical writer를 조립합니다. collector는 publish 경로 때문에 같은 package를 가져오며, API 전용 직접 caller만 있는 `batchrepo`까지 간접 의존합니다.

**제안:** 소유권 단위로 다음을 분리합니다.

- shared `contracts/sourceobservation`: envelope, kind/schema, identity/hash, status, 공유 검증 규칙.
- collector 내부: collection lease/checkpoint와 fenced publish/defer의 원자적 저장.
- API YouTube 내부: claim/consume/reconcile/canonical persist/notification intent, live finalizer, retention/replay.
- truly shared leaf: 양쪽이 실제 사용하는 payload/identity/SQL primitive만. 전체 Repository를 다른 shared 이름으로 옮기지 않습니다.

**이관 보존 조건:** publish와 checkpoint/defer의 동일 transaction, consume finalize와 canonical/intent의 동일 transaction, claim token·lease fencing, Shorts 채널별 순서, replay epoch, payload GC의 재확인, application receipt, DB role 분리. facade를 삭제할 때 모든 caller와 기존 DB 회귀를 같이 옮깁니다. 이전 package alias·forwarder를 남기는 영구 호환 계층은 만들지 않습니다.

동일 날짜 [collector 설계](2026-10-02-youtube-collector-shared-refactoring.md)는 이미 이 경계의 보존 조건을 다룹니다. 그 문서가 가리키는 `2026-10-02-alarm-worker-api-shared-refactoring.md`는 이번 로컬 체크아웃에 없었습니다. 미확인 계획의 승인·완료를 가정하지 않으며, 이관 착수 시 기존 작업과 범위를 합쳐 하나의 실행 순서로 정리해야 합니다.

#### P2 — bot/admin foundation 조립과 bot 의존성 전달 계층이 중복됩니다

**확인:** bot의 [InitScraperHolodexFoundation](../../hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/services_foundation.go#L15)과 admin의 [buildScraperHolodexFoundation](../../hololive/hololive-api/internal/planes/admin/app/build_runtime_foundation.go#L19)은 member adapter→YouTube rate limiter→official schedule scraper→Holodex provider의 같은 순서를 각각 구현합니다.

Bot는 foundation/stack→`BotDependencyModules`→`orchestration.Dependencies`→여섯 dependency view로 동일 필드를 여러 번 전달합니다. [runtime wrapper](../../hololive/hololive-api/internal/planes/bot/runtime/bootstrap_services_modules.go#L35)는 내부 bootstrap 함수를 그대로 전달하며 gopls 결과 호출자가 테스트 두 곳뿐입니다.

**제안:** 같은 construction 코드를 API 내부 한 곳으로 모으되 **인스턴스·DB pool까지 합치지는 않습니다**. plane별 config, member cache 수명, rate limit, cleanup을 그대로 전달합니다. bot composition은 하나의 typed dependency assembly로 줄이고 소비자는 필요한 좁은 계약만 받게 합니다. 모든 struct를 Fx provider로 바꾸는 방향은 채택하지 않습니다.

**완료 기준:** 전달 전용 wrapper와 그것만 고정하는 wiring 테스트 제거. 실제 명령·route·기동 실패·종료 동작 검증은 보존. fields forwarding 테스트를 새 이름으로 다시 만들지 않습니다.

#### P2 — 실패 중간 지점의 resource owner가 일관되지 않습니다

**정적 확인:** admin은 alarm client·trigger·bot-room client 등을 생성한 뒤 [build_runtime.go:143](../../hololive/hololive-api/internal/planes/admin/app/build_runtime.go#L143)에서 최종 cleanup closure를 묶습니다. 그 앞의 여러 실패 분기와 router/server 생성 실패는 `infra.Cleanup()`만 호출합니다. bot [ResolveLLMSchedulerClients](../../hololive/hololive-api/internal/planes/bot/internal/app/bootstrap/services_llm_clients.go#L38)는 첫 client 성공 후 두 번째 생성 실패 시 첫 client를 닫지 않습니다.

**위험:** 부분 조립 시 이미 획득한 non-infra 자원이 최종 cleanup 소유권을 얻기 전에 남을 수 있습니다. 모든 constructor가 즉시 socket/goroutine을 만드는 것은 아니므로 실제 QUIC 누수나 종료 지연을 재현했다고 주장하지 않습니다.

**제안:** 각 자원 획득 직후 rollback 소유권을 등록하고 성공 시 runtime에 이전합니다. 기존 lifecycle/resource-owner 방식을 재사용하고, 새 범용 DI/container를 만들지 않습니다. member cache·Holodex retry·H3 client는 의존하는 DB/Valkey보다 먼저 중지/해제합니다.

**완료 기준:** acquisition 단계별 실패 주입에서 실제 시작한 goroutine·연결·cache subscriber 종료, 성공 경로 exactly-once close, 정상 drain 중 요청 조기 취소 없음.

#### P2 — 사용되지 않는 독립 실행 진입점이 통합 lifecycle 옆에 남아 있습니다

**확인:** [LLMSchedulerRuntime.Run](../../hololive/hololive-api/internal/planes/llm/runtime/bootstrap_llm_scheduler.go#L72)은 별도 `lifecycle.Run(context.Background(), ...)` 경로입니다. gopls 참조는 선언과 lifecycle 테스트 하나입니다. 현재 production entrypoint는 Fx→aggregate Runtime입니다.

**제안:** API 내부의 standalone runner를 실제 call graph 기준으로 제거하고 `Start/Shutdown/Close`만 남깁니다. bot/admin Run은 generic interface 참조까지 확인한 후 판정합니다. worker/collector가 사용하는 shared lifecycle 자체를 일괄 삭제하지 않습니다.

**완료 기준:** production signal owner가 Fx 하나인 현재 계약 유지, fatal propagation·partial build cleanup·shutdown timeout 동작 보존. 단순히 unused method를 남기기 위한 테스트는 제거합니다.

#### P2 — AlarmCRUD가 command와 원격 관리 실패를 같은 큰 interface에 섞습니다

**확인:** [domain.AlarmCRUD](../../hololive/hololive-shared/pkg/domain/alarm_interfaces.go#L54)는 구독 read/write, cache warming, room name, runtime advance-minute state를 함께 요구합니다. `UpdateAlarmAdvanceMinutes(ctx, int) []int`는 error가 없습니다. [HTTP client](../../hololive/hololive-shared/pkg/service/alarm/client.go#L236)는 실패를 로그와 빈 slice로 바꾸고, [settings applier](../../hololive/hololive-api/internal/server/settings/settings_applier_local.go#L52)는 빈 slice로 적용 실패를 추측합니다.

**제안:** bot 구독 조회/변경, admin 설정 적용, worker 내부 cache/runtime 제어를 소비자 소유의 작은 port로 나눕니다. remote apply 결과는 typed result와 error를 돌려 확정 미적용·결과 불명을 보존합니다. 내부 Go API 전환 시 모든 구현·caller·테스트를 한 번에 변경하되, 공개 HTTP JSON/status 변경은 별도 승인 범위입니다. 자동 재시도는 추가하지 않습니다.

MemberDataProvider의 `WithContext`와 포괄 조회 interface도 장기 정리 후보이나, 이미 개선된 LoadAllMembers 오류 보존을 유지해야 합니다. 같은 PR에서 전체 member abstraction까지 재작성하지 않습니다.

### 성능 부채: 확인된 비용과 미측정 병목을 구분합니다

#### member-news: 방마다 공통 이벤트를 다시 읽습니다

[GenerateRoomDigest](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/service.go#L94)는 매번 room members와 전체 active major events를 조회합니다. [SQL](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/queries/repository_query_0129_04.sql)은 status/type/link status만 제한하고 기간·room을 제한하지 않습니다. scheduler는 [room별 실행](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/scheduler/digest_helper.go#L147)을 최대 5개 동시 수행합니다.

- 확인된 비용 구조: N개 유효 room, M개 active event라면 공통 event SQL N회와 최대 O(N×M) 후보 전달/필터링이 생깁니다. 전체 SQL 횟수·실제 wall time을 측정한 것은 아닙니다.
- 먼저 할 일: 합성 DB에서 room 수·active event 수를 늘려 query count, bytes, allocs, 시간 측정. LLM 비용과 DB 비용을 분리.
- 권고 개선: 기간/date 선택 규칙을 보존하는 SQL prefilter로 M을 먼저 줄입니다. `news`는 PubDate 우선, `event`는 EventStartDate 우선이며 weekly 범위는 단순 지난 7일이 아니라 KST -7일~+21일입니다([filter_period.go](../../hololive/hololive-api/internal/planes/llm/internal/service/membernews/filter/filter_period.go#L30)).
- 다음 후보: 한 scheduled run에서 immutable 후보 snapshot을 한 번 적재하고 room별 필터·LLM·enqueue는 유지. 이는 방별 조회 시점이 run snapshot으로 바뀌므로 명시적인 snapshot 의미 결정이 필요합니다. 수동 단일-room 호출은 같은 조회/필터 구현을 재사용합니다.
- 금지: 방별 LLM 결과를 무조건 공유, 후보 순서·멤버 귀속 변경, 동시성 무제한 확대, 실패 자동 재실행 추가. 5→큰 수로 늘리는 것은 DB 재조회 원인을 해결하지 않습니다.

#### Holodex: cache-fill 조정은 성공 stampede를 줄이지만 실패 대기를 직렬화합니다

[streamCacheFillGate](../../hololive/hololive-shared/pkg/service/holodex/provider/service_streams_cache_fill.go#L15)는 결과·오류가 아니라 완료 신호만 공유합니다. 실패가 캐시에 남지 않으면 다음 caller가 다시 owner가 되어 원천을 호출합니다. 인스턴스별 gate여서 bot/admin 사이까지 합쳐지지 않습니다.

[기존 서비스 문서](../current/services/hololive-api.md#live-query-behavior)는 2026-09-27의 caller 6개/원천 100ms 로컬 측정에서 성공 호출 1회, 실패 호출 6회와 마지막 caller 약 610ms를 기록합니다. **이번 재측정 결과가 아닙니다.** 실제 운영 병목 여부는 미확인입니다.

권고는 현재 동작을 먼저 재현하고 선택을 분리하는 것입니다. in-flight 실패도 함께 전달하는 변경은 caller 취소·오류 공유 계약을 바꿉니다. 공유 fill의 독립 유한 예산, 각 waiter 취소, last waiter 종료, 결과 소유권, 기존 retry scheduler를 설계한 뒤 승인된 동작으로만 바꿉니다. negative cache·stale result·추가 fallback을 성능 개선 명목으로 넣지 않습니다.

#### 내부 H3 왕복: 구조적 비용은 있지만 우선 병목으로 확정하지 않습니다

[configurePlanes](../../hololive/hololive-shared/pkg/config/settings/apiplane/runtime.go#L132)는 bot/admin의 LLM URL을 같은 프로세스 loopback으로 지정합니다. bot major-event/member-news, admin trigger·room 조회는 별도 client/transport를 만듭니다. JSON 변환·라우팅·transport 수명 비용은 있지만 실제 p95/p99/allocs는 미측정입니다.

우선 caller 소유 interface와 application method 경계를 정리하고 현재 H3 binding을 유지합니다. 이후 측정에서 의미 있는 비용이 확인되면 내부 호출만 typed in-process binding으로 바꾸는 것을 권고합니다. 외부 internal route·인증·수동 trigger·health는 필요한 계약대로 남깁니다. nested `internal` 경계를 깨서 다른 plane의 repository를 직접 import하지 않고, plane 바깥에 최소 application boundary를 노출합니다.

인프로세스 호출로 바꿔도 대상 plane의 pool·동시성 한도·timeout·오류 의미를 유지해야 합니다. `local 실패→HTTP 재시도` 같은 두 경로 fallback이나 runtime 선택 토글은 만들지 않습니다. 공개 설정 키 폐기·endpoint 제거가 필요하면 별도 계약 변경 승인 대상입니다.

#### 관리 dispatch 집계: 보존량에 비례하는 읽기입니다

[summary.sql](../../hololive/hololive-api/internal/planes/admin/internal/service/dispatchops/queries/summary.sql)은 보존 중인 전체 delivery의 count/min을 집계합니다. handler에 5초 timeout이 있고 list는 이미 ID cursor 기반입니다. 전체 집계가 크다는 사실만으로 느린 query라고 확정할 수는 없습니다.

보존량별 `EXPLAIN (ANALYZE, BUFFERS)`와 동시 admin 요청의 pool 대기를 테스트 DB에서 측정합니다. index/SQL 개선을 먼저 검토합니다. “최근 24시간만 집계”나 stale cache 도입은 정확성·응답 계약 변경이므로 무단 적용하지 않습니다. 이미 bounded group query와 cursor pagination이 있는 부분을 다시 구현하지 않습니다.

#### 낮은 우선순위

- `dispatchops.querySQL`은 요청마다 embed ReadFile/string 변환/ReplaceAll을 합니다. 고정 query 사전 준비로 줄일 수 있지만 DB 집계보다 먼저 최적화할 근거는 없습니다.
- member cache·template renderer가 plane마다 있다는 이유만으로 전역 singleton으로 만들지 않습니다. template cache에는 version key와 256-entry 상한이 이미 있습니다. member cache의 epoch/subscriber 종료 소유권도 이미 보강되어 있습니다.
- 전체 domain 타입을 value로 바꾸거나 모든 pointer/slice 복사를 없애지 않습니다. cache snapshot·payload alias 방지에 필요한 복사는 유지합니다.

### 중복으로 오인하면 안 되는 구현

| 현재 분리 | 판단 |
|---|---|
| bot와 LLM formatter | `internal/templateview`를 이미 재사용. 즉시 응답과 예약 알림의 render 실패 계약이 다르므로 하나의 formatter로 강제 통합하지 않음 |
| weekly/monthly, major-event/member-news scheduler | `schedulerkit`·digest dispatch helper가 이미 공통 제어를 담당. 기간·guard·fallback 차이는 남김 |
| member-news/major-event subscription clients | `internal/service/subscriptionclient`로 공통 CRUD가 이미 추출됨. 새 generic HTTP layer 불필요 |
| DB pool 네 개 | 명시적인 bulkhead. factory 구현 재사용과 connection pool 공유를 구분 |
| 세 outbox 계열 | reply uncertainty, v2 digest delivery, alarm ledger의 identity/receipt/state가 다름. 저장소 통합 제외 |
| source observation publish/consume wrapper | capability 노출 의도는 타당하지만 같은 package 구현까지 분리한 것은 아님. 실물 이관 시 facade 제거 |
| domain.Stream의 구 플랫폼 필드 | provider 제거와 달리 HTTP·저장 payload 호환 계약으로 남음. 이름만 보고 삭제하지 않음 |

### 목표 구조와 의존 방향

모듈 개수를 늘리지 않습니다. 기존 `internal/app`, `internal/fxapp`, `internal/planes`, `internal/templateview`를 활용합니다. 다음은 책임 위치이며 디렉터리 전체 rename을 요구하는 규격이 아닙니다.

```text
hololive-api/internal/
  config/                 API runtime 설정·검증 (shared/apiplane에서 회수)
  app/                    aggregate plane 조립·자원 소유권
  apifoundation/          bot/admin 공통 builder; app·planes·Fx에 의존하지 않음
  fxapp/                  프로세스 lifecycle만 유지
  planes/
    bot/                  ingress·명령·durable reply
    admin/                HTTP adapter·관리 use case
    llm/                  요약·구독·digest application/scheduler
    youtube/              YouTube plane lifecycle·supervisor
  youtube/
    sourceobservation/    claim·consume·canonical·retention/replay; 관리 cmd도 사용
    community/            community 처리 구현
    reconcile/            content/live/photo/profile/schedule/viewer reducer
  templateview/           API 내 순수 표시 데이터 공통화

hololive-shared/
  contracts/ 역할          wire·저장 identity·cross-runtime 계약
  domain/ 역할             순수 Hololive 규칙·값; cache/transport 비의존
  adapter 역할             실제 복수 runtime이 쓰는 좁은 구현만 유지

hololive-youtube-collector/internal/
  collection/publish 역할  lease·checkpoint·observation publish

hololive-alarm-worker/internal/
  dispatch 역할            proactive dispatch·delivery state machine

shared-go
  범용 Go primitive만 유지; Hololive 설정·사업 규칙을 내려보내지 않음
```

의존 방향은 `transport/runtime → application → domain/contracts`입니다. application에서 필요한 저장/조회/전송 port는 소비자가 정의하고 concrete adapter는 composition이 연결합니다. 실제 대체 구현이나 격리 필요가 없는 단순 함수까지 interface로 감싸지 않습니다. shared에 application registry나 전역 service locator를 만들지 않습니다.

`delivery`는 enqueue와 dispatch가 같은 package에 있는 별도 경계 정리 후보입니다. API에는 생산 계약, worker에는 claim/send/settle 구현을 노출하되 bot도 쓰는 Iris transport primitive는 분리 판단합니다. 이것을 sourceobservation 대이관과 동시에 바꾸지 않습니다.
