# YouTube 수집 경계 정리와 DB 보존 적체 해소 통합 계획

**상태: 1·2단계 완료. v12.1.0으로 collector a·b·c·d와 issuer를, v12.1.1로 c·b·hololive-api·alarm-worker를, v12.1.2로 collector a·b·c·d와 issuer를 배포했습니다(2026-10-25). 3단계는 v12.2.0(migration 341)으로 2026-10-25 운영에 적용했고 24시간·7일 관찰 중입니다. 0단계는 기준선만 확정했고, 4~6단계는 미실행입니다.**

[DB 증가와 보존 적체 해소 계획](2026-10-25-db-growth-and-retention-remediation.md)과 [YouTube collector Go·JS 경계 구조 리뷰](../../history/architecture/2026-10-25-youtube-collector-go-js-boundary-review.md)의 후속 작업을 하나의 순서로 묶습니다. 이 문서는 전체 순서, 단계 사이의 의존, collector 작업 목록을 소유합니다. DB 단계의 관측 근거, 보호 불변식, 세부 체크리스트는 DB 계획이 계속 소유하며 여기서 다시 쓰지 않습니다. 아래 `리뷰 #n`은 리뷰 기록의 발견 번호입니다.

## 목표

- 삭제 가능한 원본의 적체를 해소하고, 보호 이력의 장기 증가에 유한한 경계를 세우고, 적체 정리 뒤 필요한 인덱스만 공간을 회수합니다(DB 계획 1~3단계).
- collector가 상태를 잘못 판정하거나, 실패를 정상값으로 바꾸거나, 요청 예산을 우회하는 경계 결함을 없앱니다. 판정은 Go가 소유하고 helper는 사실 추출만 하는 방향으로 정리합니다.

## 두 작업을 묶는 이유

1. **수집 판정 오류가 LIVE·UPCOMING 보호를 늘릴 수 있습니다.**
   - 경로: helper가 `CANCELLED` 행을 내보냄 → generation 3 query proof가 `ENDED`·`LIVE`·`UPCOMING`만 허용(shared `payloads_decode.go`의 `validateLiveSnapshotQueryProof`) → 그 채널 snapshot 전체가 `parser_drift`로 실패(리뷰 #2) → 취소 종료 근거가 API에 도달하지 못함 → 세션이 UPCOMING으로 남아 과거 관측을 계속 보호.
   - API reducer는 `CANCELLED`를 명시 취소 종료 근거로 처리하므로(`hololive-api/internal/youtube/reconcile/live/reduce.go`), 막혀 있는 곳은 수집 계약입니다.
   - 반대 방향 위험: 모르는 badge를 `ENDED`로 채우면(리뷰 #3) 조기 종료 근거가 생길 수 있습니다.
   - DB 계획이 관측한 7일 이상 미관측 UPCOMING 9,869행·LIVE 2,777행 중 수집 실패에서 비롯된 비율은 아직 모릅니다. 이 비율이 DB 2단계 설계의 전제입니다.
2. **관측량을 늘리는 수집 변경은 보존 처리량이 확보된 뒤에 둡니다.** 예정 영상 확인을 별도 Go RPC로 옮기면(리뷰 #1의 선택지 b) 원본 INSERT가 늘어납니다.
3. **같은 계약을 거칩니다.** 두 작업 모두 shared `sourceobservation` 계약과 API YouTube plane을 지나므로, 세대 cutover가 필요한 변경은 한 점검 창으로 모읍니다.

## 공통 제약

- 운영 DB 변경, migration 적용, 배포, `REINDEX`, 보호 계약 변경, 관측 세대 cutover는 각각 대상과 효과를 확인하고 명시 승인을 받은 뒤 실행합니다. Hololive 런타임과 배포는 `hololive-bot-ops`, PostgreSQL 조회와 변경은 `stack-platform-ops` 절차를 따릅니다.
- DB 계획의 보호 범위, 잠금 순서, 원자적 커밋을 유지합니다. 이미 적용된 migration은 수정하지 않고 새 migration으로 변경합니다([규약](../../../hololive/hololive-api/scripts/migrations/CONVENTIONS.md)).
- collector는 exact-key, fail-closed 경계를 유지합니다. 새 재시도·fallback·기본값 대체를 추가하지 않고, fleet rate·worker·retry 상한을 늘리지 않습니다.
- 새 검사는 제품 동작을 실행하는 테스트로 한정합니다. 문서·스크립트 텍스트 검사, 계약 해시 게이트는 만들지 않습니다.
- 구현은 hololive-bot 소유 worktree에서 합니다. collector 이미지는 issuer와 짝으로 배포합니다([runbook](../runbooks/youtube-collector.md#isolated-po-token-lifecycle)).

## 단계

### 0. 운영 기준선과 원인 분리 (읽기 전용)

- [x] DB 계획의 처리량 상향 전 기준선을 확정합니다: foreground 지연, queue age, DB 슬롯 대기, WAL, 디스크 여유와 각 허용 범위. 결과는 DB 계획의 "1단계 운영 적용"에 있습니다. 원본 적체 나이는 시계열이 없어 `unknown`입니다.
- [ ] 7일 이상 미관측 UPCOMING·LIVE 연결의 영상과 채널이 다음 실패와 겹치는지 셉니다.
  - `youtubejs_channel_live`의 최근 `parser_drift`·DATA_CONTRACT 실패. 특히 coverage 밖 상태 거부.
  - 실패가 반복되는 채널 목록.
  - 겹치는 비율은 5단계의 입력입니다.
- [ ] collector 기준값을 AP별로 기록합니다.
  - helper `/health`의 proof 상태
  - 채널당 시각 없는 UPCOMING 행 수의 분포(리뷰 #1의 실제 fan-out 규모)
  - snapshot 실패율
  - helper 재시작·OOM 횟수
- 업무 payload와 운영 SQL 원문은 출력하지 않습니다. 제한 조회 때문에 확정하지 못한 값은 `unknown`으로 남깁니다.

### 1. 계약 검증 기반 (로컬)

- [x] `youtubejs/testdata`에 endpoint별 유효·무효 응답 본문과 실패 tuple fixture를 둡니다. Go(`decodeHelperSuccess`, `helperStatusError`, envelope 생성)와 JS(응답 검증기, `rpcErrorResult`)가 같은 fixture로 실제 동작을 검증하게 합니다(리뷰 #9).
- [x] stub fetcher로 실제 `server.mjs`를 띄우고 Go 클라이언트와 왕복하는 오프라인 테스트를 하나 둡니다.
- [x] 최소 성공 응답 크기를 Go가 최소 struct를 marshal해 계산하게 바꿉니다. `/v1/channel`의 171과 160 불일치가 해소되어야 합니다.
- 이후 wire 계약을 바꾸는 작업(2·4단계)은 이 fixture에 사례를 추가해 검증합니다.

**1단계 결과 (로컬)**

- fixture: `youtubejs/testdata/wire-contract.json`. 실패 tuple 13개(code·class·status·retry kind), 잘못된 오류 본문 5개, endpoint 5개의 최소·유효·무효 본문, 왕복 실패 사례 6개를 둡니다. 무효 본문은 Go가 거부하는 단계(`decode` 또는 `envelope`)를 표시합니다.
- JS: `src/wire-contract.test.mjs`가 `failureTuples` code 집합, `rpcErrorResult`의 status·class 수락 범위, `rpcErrorResultFor`가 내보내는 retry kind, endpoint별 최소 크기와 검증기 수락·거부를 fixture로 확인합니다.
- Go: `internal/runtime/youtubejs/wire_contract_test.go`가 `helperFailureMap`·`mapHelperFailure`·`validRetryKind`·`helperStatusError`와 `decodeHelperSuccess`를 같은 fixture로 확인합니다. `TestWireRPC`는 `youtubejs/testdata/wire-roundtrip-helper.mjs`(실제 `listenUnix`에 fixture fetcher만 주입)를 `Start`로 띄워 유효 본문 왕복, helper 최소 크기 요청, 실패 tuple·retry hint 매핑을 확인합니다. `youtubejscollector/wire_contract_test.go`는 `/v1/channel` live 본문을 실제 RPC decode와 live snapshot envelope 생성에 통과시키고, envelope 단계 거부 사례가 출력 없이 `parser_drift`인지 확인합니다.
- 최소 크기: Go `minimumSuccessResponseBytes`가 DTO 최소값을 marshal해 계산합니다. 이를 위해 wire에서 선택인 `missing_tab`과 profile 필드에 `omitzero`를 붙였습니다(decode 동작은 같음). 결과는 community·content 124, channel 160(기존 171), channel_live_check 97, video_live_check 123입니다. video의 확인된 MEMBERS_ONLY 하한은 DTO가 빈 `channel_id`를 생략해 helper 하한 139보다 작게 나오며, 그 사이 요청은 helper가 같은 `response_too_large/RESOURCE_LIMIT`로 거부합니다. 테스트는 Go 하한이 helper 하한을 넘지 않는지 확인합니다.
- 실패 테스트 선확인: 수정 전 `TestWireMinimumSuccessBytesDoNotExceedHelperMinimum`이 `minimumSuccessResponseBytes(/v1/channel) = 171, helper minimum = 160`으로, `TestWireRPC`가 `/v1/channel at helper minimum 160`의 로컬 거부로 실패했습니다.
- 발견: Go 요청 DTO는 `max_results`·`max_pages`를 0이어도 보내고 helper는 0을 거부합니다. 운영 호출자는 항상 양수를 넣으므로 결함으로 보지 않았고 바꾸지 않았습니다.

### 2. 관측 계약을 바꾸지 않는 collector 수정

1단계 뒤에 진행합니다. 하나의 collector·issuer 짝 배포로 묶을 수 있습니다.

- [x] **채널 live 숨은 fan-out(리뷰 #1, 선택지 a).**
  - 한 RPC 안의 실제 upstream 호출이 Go `RateLimiter` 간격과 `jobMaxUpstreamCalls` 예산에 반영되게 합니다. 예정 영상 확인 상한 초과는 `parser_drift`가 아니라 RESOURCE_LIMIT으로 분류합니다.
  - 관측 계약은 바꾸지 않고, helper wire 변경은 같은 이미지 안의 짝 변경으로 처리합니다.
  - 선택지 b(예정 영상 확인을 Go 소유 RPC로 이동)는 snapshot의 `scheduled_at`과 이를 쓰는 API 경로를 다시 설계해야 하고 관측량도 늘어나므로 보류합니다. 3단계 운영 효과를 확인한 뒤 5단계와 함께 다시 평가합니다.
- [x] **badge 판정(리뷰 #3).** 인식하지 못한 badge는 `ENDED` 대신 `parser_drift`로 거부합니다. 배포 뒤 snapshot 실패율이 늘면 badge 구조를 조사하며, 기본값으로 되돌리지 않습니다.
- [x] **예정 최초공개 판정(리뷰 #4).** `classifyPublication`이 `WaitingStateConfirmed`를 직접 요구하게 하고, helper 응답 검증에 "`scheduled_at`이 있으면 대기 상태 확인" 관계를 추가합니다.
- [x] **continuation 일시 실패(리뷰 #6).** network, 5xx, timeout으로 한정하고, 그 밖의 SDK 예외는 `parser_drift`로 둡니다.
- [x] **공유 Innertube 초기화(리뷰 #7).** 요청 컨텍스트 밖(bootstrap)에서 생성하고, config 조회는 명시적으로 끄거나 실패를 분류된 오류로 드러냅니다.
- [x] **PO 상태 노출(리뷰 #5).** `proof.state`, `last_error`, 발급 누계를 Go 메트릭으로 내보냅니다.
- [x] **PO mint 취소 의미(리뷰 #8).** broker와 controller 중 한쪽 의미로 맞추고, JS fake broker를 실제 동작에 맞춥니다. broker를 바꾸면 issuer 이미지도 함께 바뀝니다.
- [x] **Node heap 상한(리뷰 #10).** helper spawn 인자에 heap 상한을 명시합니다. 값은 0단계 관측과 기존 Node 몫 192MiB 예산에서 정합니다.
- 리뷰 #11의 낮은 항목은 같은 파일을 수정할 때만 함께 처리합니다.

**2단계 결과 (로컬, 미배포)**

커밋 SHA는 2026-10-25 main `48bac3831` 위로 옮긴 뒤의 값입니다(#1 lint 정리 `c3895af98`, AP rsync manifest `24d2d5842`, 리뷰 반영 `eb484f722`). 각 항목에 바뀐 동작을 실행하는 테스트를 추가하거나 기존 기대값을 새 의미로 바꿨습니다. #1의 호출 예산 테스트는 변경 전 값(수집 시한 39초)에서 실패하는 것을 확인했습니다. 리뷰 주장이 틀려 고치지 않은 항목은 없습니다. 리뷰 #11은 해당 파일을 수정하지 않아 처리하지 않았습니다. 커밋은 모두 로컬이며 push하지 않았습니다.

| 항목 | 커밋 | 바꾼 동작 |
| --- | --- | --- |
| #4 | `7f94658af` | `classifyPublication`이 `WaitingStateConfirmed == true`를 요구합니다. helper 검증기는 `scheduled_at`이 있으면 `waiting_state_confirmed === true`를 요구합니다. fixture에 classification 사례를 추가했습니다. |
| #3 | main `48bac3831`(#695) | rebase 때 같은 결함을 더 넓게 고친 main 변경으로 대체해 이 작업의 커밋을 뺐습니다. streams 상태는 LIVE badge style, `Upcoming`·재생 시간 배지 등 구조화된 표시로만 정하고, 표시가 없는 행도 `parser_drift`입니다. |
| #6 | `4670e61c5` | continuation 부분 결과(`continuation_transient`)는 `collection_timeout`, transport가 분류한 `collection_failed`, 원인 사슬(깊이 4까지)의 network 오류에만 남깁니다. HTTP 4xx는 기존 분류로 다시 던지고, 그 밖의 SDK 예외는 `parser_drift`입니다. continuation에서 생길 수 없는 `helper_busy`는 목록에서 뺐습니다. |
| #10 | `f2dfe1e50` | helper spawn에 `--max-old-space-size=192`를 붙입니다. 768M cgroup에서 기본 `heap_size_limit`는 408MiB, 플래그 적용 뒤 288MiB입니다(old space 192MiB와 그 밖의 V8 영역). |
| #5 | `bf97617f7` | scrape마다 helper `/health`를 읽는 collector로 `youtubejs_helper_health_scrape_success`, `youtubejs_po_proof_state{state}`, `youtubejs_po_proof_last_error{code}`와 bootstrap·upstream·mint·attach 누계를 냅니다. 모르는 state는 `UNRECOGNIZED`, 형식 밖 code는 `unrecognized`로 냅니다. native AP가 `/ready`를 주기적으로 부르지 않을 수 있어 `Healthy` 호출 시점에 기록하지 않습니다. |
| #7 | `266dc4be9` | 수집 Innertube는 `retrieve_innertube_config: false`로 bootstrap 중 READY 전에 `FetcherSet.prepare()`가 만듭니다. 실패하면 helper는 FAULTED입니다. |
| #8 | `45b270c95` | broker(issuer 이미지)는 바꾸지 않고 controller를 broker 의미에 맞췄습니다. 취소된 mint 뒤에는 session을 버리고 `UNAVAILABLE`, `last_error=mint_aborted`로 둡니다. JS fake broker도 중단된 mint의 세대를 퇴역시킵니다. |
| #1 | `b7790aeb9` | live 채널 요청이 조회 상한 `max_schedule_lookups`(32, protocol 최대값)를 보냅니다. 후보가 상한을 넘으면 player 요청 전에 `response_too_large/RESOURCE_LIMIT`입니다. helper는 성공(`live_query`와 함께)·실패 응답에 실제 조회 수 `schedule_lookups`를 싣고, Go는 RPC 뒤 그 수만큼 limiter 예약을 하나씩 기다립니다. 상한을 넘는 보고나 `/v1/channel` 밖의 보고는 protocol mismatch입니다. `youtubejs_channel_live` 호출 예산은 1+32입니다. |

- #1 수집 시한: 실행 프로필이 호출마다 요청 시한을 곱하므로, 기본 설정(요청 30초, 간격 2초, inflight 4, overhead 1초 가정)에서 `youtubejs_channel_live` 수집 시한은 39초에서 약 21분(33×30초 + 33×4×2초 + overhead)으로 늘어납니다. RPC 하나는 HTTP 시한 30초 하나가 묶으므로 실제 최악 경로는 약 5분입니다. 더 좁히려면 실행 프로필에서 RPC 수와 limiter 예약 수를 나눠야 하며 이번 범위에서는 하지 않았습니다.
- #1 분류 변경의 효과: `parser_drift/DATA_CONTRACT`와 `response_too_large/RESOURCE_LIMIT`는 모두 DEFER이므로 재시도 동작은 같습니다. 진단 코드와 attempt 메트릭 결과값(`parser_drift` → `failed`)이 바뀝니다.
- 배포 묶음: #1의 요청·응답 필드와 #4의 검증 관계는 같은 이미지 안의 collector·helper 짝 변경입니다. 두 쪽이 exact-key로 decode하므로 섞인 조합은 protocol mismatch로 실패합니다. issuer 이미지는 바뀌지 않았지만 기존 runbook대로 collector와 짝 배포합니다.

검증(마지막 수정 뒤, 로컬)

- `youtubejs`: `npm test` 397/397 통과, `npm run typecheck` exit 0.
- `go test -race -count=1` `internal/runtime/youtubejs/...`, `youtubejscollector/...`, `collectorruntime/...` 통과. collector 모듈 `go test -count=1 ./...` 통과.
- golangci-lint(`-c ../../.golangci.yml`, 같은 세 패키지) 0 issues. NilAway(`scripts/ci/nilaway-models/build.sh` 도구, 같은 세 패키지) exit 0.
- `TestWireRPC`가 실제 `server.mjs`로 조회 수 보고와 limiter 지불(실패 전 조회 3건)을 왕복 확인합니다.
- 실행하지 않은 검사: `scripts/ci/pre-push-gate.sh`(발행 전용). shared 계약을 바꾸지 않아 shared·API 모듈 테스트는 실행하지 않았습니다.

남은 위험

- #3을 대체한 main #695는 shelf·안내 renderer가 있거나 배지 없는 streams lockup을 `parser_drift`로 거부하므로, 배포 뒤 channel live의 `parser_drift`(DATA_CONTRACT) 비율을 확인해야 합니다.
- #10의 값은 0단계 관측 없이 compose 주석의 Node 몫 192MiB와 기존 관측 최대 RSS 176MiB를 근거로 정했습니다. 배포 뒤 helper 재시작·OOM과 RSS를 확인해야 합니다.
- #7에서 upstream config 조회를 끈 것이 실제 upstream 응답에 영향을 주는지는 운영에서 확인하지 않았습니다.
- #8: dispatch 전에 취소되어 broker가 세대를 퇴역시키지 않은 경우에도 session을 버리므로, 다음 발급에서 한 번 불필요하게 재발급할 수 있습니다.
- video live check의 Go 최소 크기(123)는 helper 하한(139)보다 작습니다. 그 사이 요청은 helper가 같은 RESOURCE_LIMIT로 거부합니다.
- #1: 채널당 시각 없는 UPCOMING 수의 실제 분포(0단계)를 모르므로, 상한 32의 운영 적합성과 limiter 지불로 늘어나는 channel_live 지연은 배포 뒤 확인해야 합니다.
- #1 지불 누락(리뷰 확인, v12.1.2에서 보완): 지불 대기 중 수집 문맥이 끝나면 남은 조회를 기다리지 않고 limiter에 예약합니다. helper에 요청을 다 보냈는데 검증된 helper 본문을 받지 못한 RPC(Go HTTP 시한 30초 초과·취소·연결 끊김·잘못된 본문)는 요청한 상한(32)만큼 같은 방식으로 예약합니다. helper가 요청이 끊기면 조회를 중단하므로 상한은 실제 조회 수의 상한이며, 과다 지불 쪽으로만 어긋납니다. 전송 전 실패와 조회 수를 생략한 helper 오류 본문(0건)은 예약하지 않습니다. 예약은 기다리지 않으므로 수집 시한 계산은 바뀌지 않고, 다음 RPC들이 그만큼 늦게 시작합니다. transport의 player 1회 재전송(5xx·network)은 조회 1건으로 세며 browse의 기존 회계와 같습니다.

**2단계 운영 반영 결과 (2026-10-25)**

- 릴리스: PR #696을 squash 병합해 `a1e713dbbd86b56e36aa917f744453dde77a80e3`이 됐습니다. 이 커밋에 annotated tag `v12.1.0`을 달았습니다. main의 미출시 #694(의존성)·#695(youtubejs 판정)와 같은 배포 단위입니다. #695는 이 계획의 #3을 대체했습니다. 병합 트리는 rebase 뒤 검증한 HEAD `002978930`과 같습니다.
- 검증(`002978930`, 마지막 코드 편집 뒤):
  - `scripts/ci/pre-push-gate.sh` 통과. push 때 hook으로 두 번 더 재실행했고 모두 통과했습니다.
  - helper 테스트 406/406, typecheck 통과.
  - PR CI 11개 검사 모두 통과(`fast-gate` 포함).
  - 배포 정적 계약 8종 통과.
- central c(hololive-seoul, 09:13Z 시작): `po-central-cutover.sh deploy`로 issuer부터 교체했습니다.
  - collector-c(`e5ffe1a71c64`)와 po-c(`a179959bd2c1`) 모두 arm64, revision `a1e713dbb`, version 12.1.0입니다.
  - `/health`·`/ready` rc=0, 새 로그에 오류 표지가 없고 PostgreSQL 연결 표지가 있습니다.
  - po-c의 재시작 1회는 exit 0(`reason=session_closed`)으로 runbook 수용 기준을 충족합니다.
  - rollback 기준점: `backups/po-c-20261025T091310Z`, `rollback-20261025T091310Z` 태그(v12.0.0 `f3d240006`).
- AP b(iris-seoul): `ap-deploy.sh seoul --apply` 뒤 completion check를 통과했습니다.
  - collector-b·po-b 모두 `a1e713dbb`, 12.1.0, healthy입니다. po-b의 재시작 1회는 `session_closed` exit 0입니다.
  - rollback 기준점: `backups/seoul-collector-20261025T091509Z`(검증 통과).
- AP a(osaka1)·d(osaka2): `HOLO_BOT_VERSION=12.1.0 ap-host-native-deploy.sh <ap> --apply` 뒤 두 곳 모두 completion check를 통과했습니다.
  - current는 `20261025T091627Z-a1e713dbbd86-osaka`·`20261025T092035Z-a1e713dbbd86-osaka2`, previous는 `…-f3d240006b25-…`입니다.
  - issuer는 `Result=success`, `ExecMainStatus=0`, active이고 journal의 실패·오류 표지는 0건입니다.
- 수용 뒤 retention:
  - 지운 것: central과 iris-seoul의 v12.0.0 배포 이전 rollback 태그(`rollback-20261022T024300Z`/`rollback-20261022T024445Z`)와 그 backup·staging, osaka1·osaka2의 `20261021-node-26.12.1-108ecb2e60c5` release.
  - 남긴 것: 이번 rollback 세트, 다른 작업의 `emudhra` 태그, v12.0.1 hololive-api rollback 자료.
- 재배포하지 않은 것: hololive-api(12.0.1)와 alarm-worker(12.0.0)는 다시 만들지 않았습니다. 중앙 `current`의 `hololive/hololive-api/VERSION`은 12.1.0이므로, 이후 전체 `compose up`이 돌면 API는 이전 image로 `APP_VERSION=12.1.0`이 됩니다(릴리스 runbook에 적힌 식별자 차이). #694 의존성 갱신의 api·alarm-worker 반영은 별도 배포가 필요합니다.
- 확인하지 못한 것: 배포 뒤 channel live의 `parser_drift`·`response_too_large` 비율과 새 PO 메트릭 값은 Prometheus 조회 경로가 이 작업에서 확인되지 않아 보지 못했습니다. 다음 운영 점검에서 `youtube_collection_attempts_total{job_kind="youtubejs_channel_live"}`의 결과별 증가와 `youtubejs_po_proof_state`를 AP별로 확인합니다. 새 로그 기준으로는 네 곳 모두 오류 표지가 없습니다.

**v12.1.1 운영 반영 (2026-10-25)**

- **발견한 결함:** v12.1.0 배포 뒤 확인해 보니, collector-c·b 이미지에 2026-10-22 eMudhra CA 차단이 빠져 있었습니다. 차단은 운영 이미지에 덧입힌 파생 bundle 방식이었습니다. 그래서 저장소 배포 진입점이 다시 빌드하면 사라졌습니다. native a·d는 컨테이너 bundle을 쓰지 않아 영향이 없습니다.
- **수정:** #698(v12.1.1, main `80a584f827061f33702c5b13e1bf466519a06f51`)에서 collector·hololive-api·alarm-worker 빌드 단계가 `scripts/build/ca-distrust.sh`로 차단 루트를 빼게 했습니다. 대상은 `emSign ECC Root CA - G3`, `AAA Certificate Services`입니다.
  - 로컬 arm64 빌드의 bundle은 2026-10-22에 검증한 파생 bundle과 인증서 순서까지 같습니다.
  - PR CI 11개 검사가 모두 통과했습니다. tag `v12.1.1`과 GitHub Release를 게시했습니다.
- **central c·Seoul b:** 기존 진입점으로 다시 배포했고, 둘 다 revision `80a584f82`, 12.1.1, arm64입니다. 실행 중인 collector의 CA bundle SHA-256은 검증값 `64e7c24b0536b525…`과 같습니다.
  - c: `/health`·`/ready` rc=0. 새 po-c는 `session_closed` exit 0으로 1회 재시작했습니다. exit 143은 recreate 때 멈춘 이전 컨테이너의 기록입니다. rollback 기준점은 `backups/po-c-20261025T110948Z`입니다.
  - b: completion check를 통과했습니다. rollback 기준점은 `backups/seoul-collector-20261025T111138Z`입니다.
- **hololive-api·alarm-worker:** release runbook의 no-build pair 절차를 따랐습니다(v12.0.1 api 배포와 같은 순서).
  1. kapu에서 `--provenance=mode=max --sbom=true`로 빌드하고 전송했습니다. 원격 Id가 로컬과 같은지 확인했습니다.
  2. 배포 트리를 `backups/app-80a584f82-20261025T111246Z`에 백업하고 root `VERSION`을 12.1.1로 설치했습니다.
  3. 서비스별 pair를 `artifacts/worker-profile/releases/app-80a584f82-20261025T111246Z/<service>`에 만들었습니다. 이전 pair capture, 후보 `--check-worker-profile`, `verify-source`, `compose-rollback-service.sh --check`를 모두 통과했습니다.
  4. `hololive-db-migrate`를 실행했습니다(applied=0, skipped=201).
  5. alarm-worker, api 순서로 `up -d --no-build --no-deps`를 실행했습니다.
  - 결과: 두 서비스 모두 healthy, 재시작 0, `verify-live` 통과, 새 로그 표지 0입니다. CA bundle은 검증값 `e9c7b63ee2623386…`과 같고, api `APP_VERSION`은 12.1.1입니다. Alertmanager 활성 경보는 0건입니다.
  - 이전 이미지 `hololive-api:rollback-20261025T111246Z`(v12.0.1 `dc0b57be0a81`)와 `hololive-alarm-worker:rollback-20261025T111246Z`(v12.0.0 `b48066b8e06e`)는 둘 다 CA 차단 파생 이미지입니다.
- **retention:**
  - collector의 직전 rollback인 v12.1.0 세트에는 CA 차단이 없습니다. 그래서 그 앞의 차단된 v12.0.0 세트도 함께 보존합니다. central은 `po-c-20261025T091310Z`와 그 staging, Seoul은 `seoul-collector-20261025T091509Z`입니다.
  - v12.0.1 api rollback 자료(`rollback-20261025T070228Z`, pgfix pair·backup)는 해당 작업의 요청대로 보존합니다.
  - 삭제한 것은 로컬 검증 이미지(`*:ca-verify`)뿐입니다.
- **재배포하지 않은 것:** native a·d는 바이너리와 CA 경로가 그대로라 v12.1.0 release로 둡니다.

**v12.1.2 운영 반영 (2026-10-25)**

- 릴리스: #1 지불 누락 보완(#700)을 main `220c8a0ec`에 squash하고 tag `v12.1.2`와 GitHub Release를 게시했습니다. collector·helper만 바뀌었고 issuer 이미지는 기존 runbook대로 짝 배포했습니다.
- c(central)·b(Seoul): 기존 진입점으로 배포했고 revision `220c8a0ec`, 12.1.2, arm64입니다. 실행 중인 collector의 CA bundle은 검증값 `64e7c24b0536b525…`과 같습니다. b는 completion check를 통과했습니다. rollback 기준점은 c `backups/po-c-20261025T125658Z`, b `/home/ubuntu/hololive-bot/backups/seoul-collector-20261025T125846Z`입니다.
- a(osaka)·d(osaka2): host-native 배포로 v12.1.0에서 v12.1.2로 맞췄습니다(v12.1.1은 a·d 변경이 없었습니다). 둘 다 completion check를 통과했고 collector는 `Result=success`, 재시작 0입니다. 이전 release는 a `20261025T091627Z-a1e713dbbd86-osaka`, d `20261025T092035Z-a1e713dbbd86-osaka2`입니다.
- issuer 재시작: po-c·po-b는 recreate 직후 `session_closed`로 1회, d issuer는 `worker_timeout`으로 1회 재시작한 뒤 회복했습니다.
- 관측(Prometheus): c·b·a·d 모두 `youtubejs_po_proof_state` READY, `youtubejs_helper_health_scrape_success` 1이고, 실패 지표의 30분 증가는 0입니다.
- retention: v12.1.1 배포 때 만든 CA 차단 누락(v12.1.0) rollback 세트를 지웠습니다. central `collector`·`po` `rollback-20261025T110948Z` 태그, 출처가 확인된 pair 태그, `backups/po-c-20261025T110948Z`와 그 staging, Seoul `rollback-20261025T111138Z` 태그 2개와 pair 태그, `backups/seoul-collector-20261025T111138Z`입니다. a·d에서는 그보다 이전 release(`84e54081071f`)와 남은 staging을 지웠습니다. 출처를 확인하지 못한 pair 태그(central `pair-e7030a9…`, Seoul `pair-78999…`)와 위 v12.0.0 세트는 남겼습니다.

### 3. DB 1단계: 삭제 가능 적체 처리량 (API)

- [x] DB 계획의 "1 삭제 가능한 적체의 처리량 개선" 체크리스트를 수행합니다(종류별 커서 후속안 제외). 결과는 DB 계획의 "1단계 구현 결과"에 있습니다.
- 2단계와는 런타임이 달라 독립적이며 병렬로 진행할 수 있습니다. 운영 적용 전에 0단계 기준선이 있어야 합니다.
- 운영 적용 뒤 DB 계획의 24시간·7일 관찰을 수행합니다.
- [x] 운영 적용(v12.2.0, 2026-10-25): migration 341과 hololive-api pair를 적용했습니다. 기준선·허용 범위·첫 관찰은 DB 계획의 "1단계 운영 적용"에 있습니다.

### 4. 세대 cutover가 필요한 수집 계약 변경

- [ ] **`CANCELLED`(리뷰 #2).** 권장안은 generation 3 query proof와 collector coverage가 `CANCELLED`를 허용하게 하는 것입니다. shared 계약, API, collector를 함께 바꾸고 승인된 점검 창에서 cutover합니다.
  - 대안은 helper가 `CANCELLED` 행을 sessions에서 빼는 것이지만, 취소 종료 근거를 잃으므로 택하지 않습니다.
  - 0단계에서 실제 발생이 확인되면 우선순위를 올립니다. 발생하지 않더라도 이 변경 전까지는 한 행이 채널 전체를 실패시키는 위험이 남습니다.
- [ ] 과거 generation의 의미는 API가 계속 보존합니다. collector와 API를 혼합 운용하거나 image만 되돌리지 않는다는 서비스 문서의 기존 cutover 규칙을 따릅니다.
- 5단계의 보호 계약 변경이 같은 시기에 확정되면 같은 점검 창에서 적용하는 방안을 검토합니다. 확정되지 않았다면 기다리지 않습니다.

### 5. DB 2단계: 보호 이력의 유한한 경계 (승인 대상)

- [ ] 입력: 0단계의 원인 분리 결과, 2·4단계 적용 뒤 장기 UPCOMING·LIVE 연결 증가 추세.
  - 수집 실패에서 비롯된 몫이 해소된 뒤에도 남는 증가만 보호 경계 설계의 대상으로 봅니다.
- [ ] 이후는 DB 계획의 "2 보호 이력의 장기 증가에 유한한 경계 설계"를 따릅니다. 이 결정을 보류하면 보호 데이터의 무기한 증가 위험은 미해결로 보고합니다.

### 6. DB 3단계: 필요한 인덱스만 공간 회수 (승인 대상)

- [ ] 전제: 3단계 운영 적용과 7일 관찰이 끝나야 합니다.
- [ ] DB 계획의 "3 적체 정리 후 필요한 인덱스만 공간 회수"를 따르며, 한 번에 한 인덱스만 처리합니다.

### 의존 요약

```text
0 ──▶ 3 운영 적용, 5 입력, 2·4 효과 비교
1 ──▶ 2 ──▶ 5 입력
1 ──▶ 4 ──▶ 5 입력
3 ──(운영 7일)──▶ 6, 리뷰 #1 선택지 b 재평가
2 ∥ 3  (독립)
```

## 검증

- **로컬 collector**
  - `youtubejs`에서 `npm test`와 `npm run typecheck`
  - 영향받는 Go 모듈 테스트(저장소 규칙상 적용되는 race·NilAway 포함)
  - 1단계의 공유 fixture·왕복 테스트
  - shared 계약을 바꾸면 shared, API, collector 모듈 테스트를 모두 실행합니다.
  - 빌드는 `./build-all.sh --build-only --no-bump`, 발행 전에는 `scripts/ci/pre-push-gate.sh`를 실행합니다.
- **로컬 DB:** DB 계획의 "검증과 완료 기준" 중 로컬 fixture, 제품 테스트 확장, migration runner 재실행, 대표 부하 비교 항목을 따릅니다.
- **운영 2단계:** AP별로 0단계 기준선과 비교합니다.
  - snapshot `parser_drift`·DATA_CONTRACT 비율
  - proof 상태 메트릭 값
  - helper 재시작·OOM 0
  - channel_live RPC 지연
  - 실제 upstream 호출이 예산 안에 있는지
- **운영 3·6단계:** DB 계획의 운영 관찰 기준과 중단 조건을 따릅니다.
- **운영 4단계**
  - `CANCELLED` 행이 취소 종료로 반영되는지
  - 해당 채널의 snapshot 실패가 사라지는지
  - 장기 UPCOMING 연결 증가가 줄어드는지
- 완료 여부는 체크박스가 아니라 위 결과로 판단합니다. 오류 0, 삭제량 0, 지표 부재를 정상이나 적체 0으로 바꾸지 않습니다.

## 미결정과 승인

- 리뷰 #2 `CANCELLED` 계약은 권장안으로 진행하려면 shared 계약 변경과 세대 cutover 승인이 필요합니다.
- 리뷰 #1 선택지 b는 보류 중이며, 3단계 운영 효과를 확인한 뒤 결정합니다.
- DB 2단계 보호 계약 변경은 계약 확정과 명시 승인이 필요하며, 실제 복원 가능한 복구점과 손실 범위를 함께 준비합니다.
- 모든 운영 적용(배포, migration, `REINDEX`, cutover)은 해당 ops 절차에서 대상별로 승인을 받습니다. 이 계획은 승인이 아닙니다.
