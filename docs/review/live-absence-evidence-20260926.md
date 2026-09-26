# !라이브 방송 없음 판정 증거 개편 — 실행 근거

- 계획: `PLN-20260926-live-absence-evidence`
- governing: `DEC-20260926-hololive-live-absence-evidence`
- constraints: `DEC-20260814-hololive-youtube-three-provider-convergence-v2`, `DEC-20260829-hololive-live-start-evidence-admission`, `DEC-20260926-youtube-only-stream-providers`, `DEC-20260926-hololive-list-reply-fold-default`
- 권한: 로컬 소스·테스트·문서·빌드, guard를 증명한 운영 DB 읽기, YouTube 공개 소표본. 커밋·푸시·배포·운영 migration·fleet·운영 데이터 쓰기·secret 조회는 수행하지 않는다.

## T01 관측 계약

`docs/current/architecture/youtube-three-provider-convergence-contract-v2-20260814.md` §3.4에 두 observation kind와 RPC, payload, identity, UNKNOWN, 가용성·종료 입장, canonical 시계, 2분 재확인·기본 270초 만료, 물리 요청 2/1회 상한, 7일 evidence retention, migration/grant/세대 전환 경계를 정의했다. 기존 live_snapshot 세대와 absence 종료 정책은 유지한다.

착수 시 코드 재확인(아래 구현 전 상태):

- `livequery/queries/snapshot.sql`은 현재 absence slot을 채널 coverage로 사용한다.
- `youtubejs/src/live-metadata.mjs`는 raw player의 channelId/isLiveNow/endTimestamp/isPrivate를 현재 반환하지 않는다.
- `reconcile/live/canend.go`의 explicit-end에는 LastLivePositiveAt과 LastLivePositiveSeenAt이 필요하다. `apply.go:endSession`은 EndedAt이 없으면 EffectiveAt으로 채우므로 새 영상 확인은 유효한 종료 시각 없이는 종료를 발행하지 않는다.
- `apply.go:settleDueCandidate`, `live_consumer.go`와 `live_evidence.go`를 확인했다. UNKNOWN·채널 확인을 reducer에 통과시키지 않으며 기존 pending 보존을 변경하지 않는다.
- `targetprojection/schedules.go`의 live cadence는 2분이다. projection의 video subject는 기존 target 구조로 표현할 수 있고 consumer가 canonical channel identity를 대조한다.
- 현재 helper transport는 player 실패를 재시도한다. 새 확인에는 단일 시도 transport가 필요하며 기존 feed retry 정책은 변경하지 않는다.

계약 조사 scout 2명과 가용성 독립 reviewer 1명을 사용했다. 'PUBLIC_UNAVAILABLE 근거 부재가 전체 작업을 차단한다'는 초기 후보는 AC05의 UNKNOWN 허용으로 반박됐다. 단, 익명 player에서 `isPrivate=true`와 identity가 함께 반환되는 실제 표본은 아직 없다. 원시 boolean 의미의 보수적 조건만 계약에 두고 status/messages/번역 문구로 private·삭제를 추정하지 않는다. PRIVATE/ERROR가 identity를 숨기면 UNKNOWN이며 stale LIVE 차단을 유지한다. `isPrivate=true` 익명 경로의 도달 가능성을 실측했다고 주장하지 않는다.

## 공개 요청 소표본

pinned `youtubei.js@18.1.0`의 `Innertube.create({retrieve_player:false,generate_session_locally:true,enable_session_cache:false})`와 raw `actions.execute('/navigation/resolve_url'|'/player', {parse:false,...})`를 실행했다. 쿠키·인증 없이 공개 요청만 사용했다. undici 응답 body의 압축 상태 바이트를 계수하고 br를 해제했다. HTML 요청은 없다.

| 표본 | 관측 사실 | 요청 | 압축 바이트(br) |
|---|---|---:|---:|
| `UCGzTVXqMQHa4AgJVJIVvtDQ/live` → `wnjd9XuuXg0` | 요청 채널 일치, isUpcoming=true, isLiveNow=false, 2027-12-31 예정, LIVE_STREAM_OFFLINE/offline slate | 2 | 487 + 6056 = 6543 |
| `UC1DCedRgGHBdm81E1llLhOQ/live` → `AHko7cC9LuA` | 요청 채널 일치, isLive/isLiveNow=true, UNPLAYABLE/reload renderer | 2 | 487 + 5821 = 6308 |
| `maPfvoIK-MU` player | 위 첫 채널에서 isLive/isLiveNow=true, 멤버 offer renderer. /live가 예정 영상을 고르는 반례 재확인 | 1 | 6463 |
| `UC1CfXB_kRs3C-zaeTG3oGyg/live` | WEB_PAGE_TYPE_CHANNEL, browseId 정확히 일치, /channel/동일ID/live, browse apiUrl | 1 | 546 |
| `H9Sutl-r6YY` player | 종료 최초공개, isLiveContent=false, isLiveNow=false, start/endTimestamp | 1 | 4658 |
| `P_mClWKeOOU` player | 동일 종료 최초공개 사실 | 1 | 4671 |
| `v6Y1HIFKVac` player | 동일 종료 최초공개 사실 | 1 | 4698 |
| `w669NEAkl5s` player | 동일 종료 최초공개 사실 | 1 | 4670 |

종료 최초공개 4건 모두 정확한 영상·채널 identity, 원시 `isPrivate=false`, UNPLAYABLE/reload renderer가 있었다. 처음 발견한 별도 채널 `UCdn5BQ06XqgXoAxIhbqw5Rg/live`는 WATCH endpoint만 확인(1회, 485 bytes)했고 player를 읽지 않았으므로 방송 상태 판정 근거에는 쓰지 않는다.

종료 표본은 `hololive-osaka`의 `holo-postgres`/`hololive`에서 `PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=3000'`로 `SHOW transaction_read_only = on`을 먼저 증명한 뒤 선택했다. 같은 guard로 session=LIVE, head positive가 5분보다 오래된 행을 `LIMIT 4`로 읽었다. 공개 영상·채널 식별자만 조회했으며 운영 DB 변경은 없다. 4건은 표본일 뿐 전체 stale LIVE 해소를 입증하지 않는다.

## 검증 상태

- 초기 plans gate: exit 0, strict_validation=passed, gate_passed=true.
- plans start: ready → in_progress, changed=true.
- 초기 T01 checkpoint는 설계·소표본만 입증했다. 최종 구현·검증 결과와 marker별 근거는 아래 T07 절에 기록한다.
- 전체 catalog의 사용자 보고 결함(#532 → 존재하지 않는 DEC)은 확인 재실행하거나 범위 밖 수정하지 않는다. 최종 판정에서 별도로 보고한다.

## T02 저장소 준비

- `218_live_absence_evidence_contract.sql`과 manifest: kind CHECK 세 곳을 idempotent NOT VALID/VALIDATE/swap으로 확장하고 youtubejs 두 kind의 schema 1/generation 1을 seed한다. 기존 generation 증가는 rerun으로 되돌리지 않는다.
- `youtube_channel_live_checks`, `youtube_video_availability`는 typed vocab/shape/clock 제약과 observation FK(ON DELETE SET NULL), hash·시각을 보존한다. observation 한 개는 subject 한 개의 최신 사실이므로 nullable observation_id의 UNIQUE 인덱스가 FK 탐색도 지원한다. runtime에는 SELECT/INSERT/UPDATE만 부여하고 scraper에는 canonical 권한을 주지 않는다.
- Go typed payload·canonical scope·provider/generation/clock/completeness 검증과 exact-subject job 계약을 추가했다. `availability_unclassified`만 신뢰 가능한 수명 사실을 보존한다. 종료 판정은 isLive 생략 또는 false를 허용하되 LIVE·UPCOMING과 종료 시각의 모순을 거부한다.
- 로컬 실행: `GOEXPERIMENT=jsonv2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off SCHEMA_SNAPSHOT_UPDATE=1 go test -mod=readonly -count=1 -run '^TestSchemaSnapshotGolden$' -v ./hololive/hololive-dbtest` → PASS, PostgreSQL 18.6 testcontainer의 전체 migration replay로 golden 146,939 bytes 생성. 운영 migration은 실행하지 않았다.
- 추가한 DB/Go 동작 테스트는 아직 전체 실행 전이다. migration 211 인덱스 제거는 계획대로 T05의 소비자 전환·잔여 사용처 확인 후 준비하며, 그때 T02 checkpoint를 닫는다.

## T03 수집 경로와 실제 helper smoke

- flat RPC `/v1/channel_live_check`, `/v1/video_live_check`와 각각의 DTO·boundary·raw parser·Go runner를 구현했다. 기존 live_snapshot과 채널 확인은 독립 envelope이며 한쪽 실패를 다른 쪽의 completeness로 복제하지 않는다. video job은 새 exact-subject `youtubejs_video_live`다.
- 새 확인 transport는 기존 proxy agent·취소 신호를 공유하되 재시도하지 않고 자동 redirect도 거부한다. 구 feed의 재시도 정책은 유지한다. Go collector는 해석 불가/응답 계약 오류를 UNKNOWN으로 발행해 이전 음성·공개 불가 사실을 갱신한다. 취소·lease 기한·설정/내부 불변식 오류는 publish를 하지 않는다.
- 실제 변경된 `createRealFetchers` → `handleChannelLiveCheckRequest`/`handleVideoLiveCheckRequest`를 단일 시도 transport와 공개 WEB 요청으로 실행했다(exit 0). fixture를 반환한 테스트가 아니다. 초기화부터 물리 요청을 계수했으며 별도 config 조회는 없었다.

| 실제 신규 RPC 경로 | 실제 결과 | 요청 | 압축 바이트(br) |
|---|---|---:|---:|
| 채널 `UCGzTVXqMQHa4AgJVJIVvtDQ` | UPCOMING_VIDEO, `wnjd9XuuXg0`, identity=true | 2 | 486+6055=6541 |
| 채널 `UC1CfXB_kRs3C-zaeTG3oGyg` | CHANNEL_PAGE, identity=true | 1 | 543 |
| 영상 `maPfvoIK-MU` | is_live/is_live_now=true, MEMBERS_ONLY, is_private=false | 1 | 6265 |
| 영상 `H9Sutl-r6YY` | PUBLIC, 최초공개(is_live_content=false), ended_at=2026-08-29T10:05:31Z | 1 | 4655 |

- Node/Go 회귀·typecheck·실패 경로 테스트는 결합 검증 단계에서 실행한다. PUBLIC_UNAVAILABLE의 익명 isPrivate=true 경로와 실제 로봇 확인 응답은 실측하지 않았으며 해당 fixture는 합성 경계 사례다.
- 절차 이탈: 두 T03 subagent가 일부 helper 파일 쓰기와 Go metrics label 편집 직전에 gate를 재실행하지 않았다고 보고했다. 실행 전·후 계획 gate는 통과했으나 누락을 사후 실행으로 충족했다고 주장하지 않는다. 운영/권한 변경은 없었고 후속 작업에는 매 side effect 직전 gate를 재강조했다.

## T02 종결·T04~T06 구현 근거

- LiveQuery의 production SQL에서 absence slot 참조를 제거했다. 남은 `repository_live_absence_slots.sql`은 채널 GIN 및 scheduled_for 조건만 사용하며 211의 LIVE-status 부분 predicate가 없다. 해당 index 이름의 잔여 참조는 과거 migration·schema golden뿐임을 확인한 뒤 `219_drop_live_query_coverage_time_index.sql`을 추가했다. 212 인덱스·absence 이력은 그대로다.
- `220_live_query_confirmed_empty_message.sql`은 217의 표준 전역 CMD_LIVE_STREAMS 본문이 정확히 일치할 때만 Count 0 문구를 바꾼다. 사용자 지정·채널 override·CMD_MEMBER_NOT_LIVE 보존 회귀를 추가했다. formatter의 기존 complete/unknown/member 분기는 의도적으로 유지했다.
- 최종 manifest(218~220)를 PostgreSQL 18.6 testcontainer에서 다시 replay하고 schema golden을 생성했다. T02와 같은 `SCHEMA_SNAPSHOT_UPDATE=1 ... -run '^TestSchemaSnapshotGolden$'` 명령 → PASS, 146,733 bytes. 이 재실행은 219의 schema 변경에 따른 갱신이다.
- T04: 채널 확인 consumer는 최신값만 upsert한다. 영상 consumer는 영상 한 개의 canonical identity와 수명 사실을 검사하고 정확한 ended_at만 PARTIAL 명시적 종료로 전달한다. nullable canonical 시작 시각을 추가 중단 조건으로 만들지 않으며, 오래된 관측이나 더 새로운 pending 때문에 슬롯 시각으로 끝내는 경로를 차단했다. pending 보존 테스트는 수정하지 않았다.
- T04: 활성 roster의 stale LIVE만 영상 확인 target으로 생성하며 UPCOMING·ENDED를 제외한다. 두 새 kind의 API claim·7일 retention·기존 작업 수요 계측을 갱신했다. 시간 경계/다음 refresh 생성·제거 DB 회귀는 작성했으며 실행 결과는 다음 검증 절에 기록한다.
- T05: 단일 snapshot/기존 bot pool/1초 예산을 유지하고 채널 최신 음성 및 영상 공개 불가의 네 시각을 검사한다. 신선한 positive가 우선하고 D1/D2는 nonblocking_diagnostics로만 남는다. 보존 증거만 있거나 session=ENDED이면 empty 확정이 가능하지만 LIVE/head 없음·미해결 후보·UNKNOWN·만료는 계속 차단한다.
- T06: API·collector 서비스, collector runbook, MESSAGE_STYLE_GUIDE와 관측 계약을 갱신했다. 승인된 reader 교체 창, API/migration-before-collector, 기존 kind 세대 보존, rollback decoder/인덱스 조건 및 별도 운영 비교 관측을 명시했다.
- 독립 검토 2명은 실제 `opencodex/gpt-6-astra` 사용을 보고했다. canonical 경로의 추가 finding은 없었다. collection 검토의 private/member 모순, 미지 playability status 입장, endpoint discriminant 타입 widening 후보를 반영했다. 앞의 두 건은 UNKNOWN으로 차단하는 회귀를 추가했고 타입 검증은 아래 실행 결과로 판정한다.
- T04 consumer subagent도 모든 편집 직전이 아니라 시작/종료에만 gate를 실행한 절차 이탈을 보고했다. projection subagent는 무변경으로 거부된 편집의 재시도에서 gate를 다시 실행하지 않았다. 사후 통과를 소급 준수로 기록하지 않는다.

## T07 최종 로컬 검증 — 2026-09-27

검증은 통합 담당자가 foreground에서 수행했다. 운영 배포·migration 적용·fleet 반영·데이터 변경·커밋·푸시는 수행하지 않았다.

### 실행 명령과 결과

- 영향 14개 패키지: `GOEXPERIMENT=jsonv2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off go test -mod=readonly -race -count=1 <packages>` → 모두 PASS(72.671초). 대상은 shared의 contracts/sourceobservation, service/youtube/sourceobservation, reconcile/live, config/settings/apiplane; API의 youtube/runtime, targetprojection, bot/livequery, handlers, formatter; collector의 youtubejs, youtubejscollector, collectorruntime, joblease; hololive-dbtest다.
- 최종 전체 빌드: 같은 Go 환경과 `COMPOSE_ENV_FILE=$PWD/deploy/compose/build-only.env.sample`에서 `./build-all.sh --build-only --no-bump` → exit 0(991.642초), `[LOCAL CI] Passed`, `[DONE] Image build complete`. production secret 파일 대신 committed build-only sample을 사용했다. 전체 workspace vet·staticcheck·golangci-lint·pinned NilAway·일반 테스트·race(`-p 4 -count=1`)·architecture·SQL ownership·hard structure·AP manifest·production build·performance budget gate를 통과했다. 선택적 `RUN_INTEGRATION_TESTS`는 활성화하지 않았으며, 이 작업의 DB/command smoke는 별도로 실행했다.
- 빌드 중 발견한 원인 수정: SQL `COUNT(*)` 두 곳을 non-null video key 집계로 교체; ChannelLiveRunner의 독립 snapshot/check 처리 분리; AP rsync manifest에 새 Go/embedded SQL 의존성 6개 추가; migration 208/217의 최신 seed 본문 동일성·고정 개수 검사와 superseded LIVE 빈 문구 case 삭제. 과거 migration의 렌더 전후 동작·사용자 지정 보존 검증은 유지했다. gate를 완화하거나 우회하지 않았다.
- `youtubejs/`의 `npm test` → 207/207 PASS, fail/skip 0(3.281초). `npm run typecheck` → exit 0(1.352초).
- `go test -mod=readonly -race -count=1 -run '^TestLiveCommandDatabaseSmoke$' -v ./hololive/hololive-api/internal/planes/bot/internal/command/handlers` → PASS. 실제 command→repository→DB template 경로 출력: `현재 방송 상태를 확인할 수 없습니다.`; 음성 `/live`와 positive 공존 시 `cmdlive0001` 방송 카드; D1/D2 보존 진단이 있어도 `현재 방송 중인 멤버가 없습니다.`. cold/warm upstream=0, cancellation 전파·DB 오류 응답도 PASS.
- `go test -mod=readonly -race -count=1 -run '^TestProjectionRefreshTracksStaleLiveVideoTransitions$' -v ./hololive/hololive-api/internal/planes/youtube/runtime` → PASS. 논리 시계 기준 stale 경계에서 다음 refresh까지 5초, ENDED→target 제거까지 5초. 실제 refresh transaction wall 8.0303~27.079932ms(6단계). 이는 로컬 DB와 제어된 시계의 측정이며 운영 end-to-end 지연을 실측한 것이 아니다.
- workspace `check-stack-db-access-policy`, `check-stack-retry-contract`, `check-stack-projection-tables`, `check-stack-worker-contract` → 각각 exit 0.
- 사용자 지정 보호 파일 5개의 SHA-256을 전체 빌드 전후 비교해 동일함을 확인했다. 기존 meta 변경과 다른 작업의 변경을 되돌리지 않았다.

### V02 query budget

`go test -mod=readonly -run '^$' -bench '^BenchmarkRepository(LongHistory|UnobservedPending)$' -benchtime=3x -count=1 ./hololive/hololive-api/internal/planes/bot/internal/service/livequery` → PASS. fixture는 74채널, 종료 이력 50,000건, absence slot 1,500,000건이며 unobserved case는 orphan pending 50,000건을 추가한다. 표본은 각 3회이므로 p95는 통계적 운영 SLO가 아니라 해당 소표본 최댓값이다.

| case | wall p50 ms | wall p95 ms | EXPLAIN ANALYZE ms | shared hit/read |
|---|---:|---:|---:|---:|
| sparse LIVE | 125.6 | 155.0 | 150.9 | 3870/0 |
| all LIVE | 218.8 | 233.1 | 220.1 | 4718/0 |
| orphan pending / all | 240.0 | 244.2 | 386.2 | 6216/0 |
| orphan pending / member | 109.8 | 113.1 | 118.6 | 9320/0 |

단일 snapshot·기존 pool·1초 예산 안에서 관측했다. 운영 DB의 실제 실행 계획이나 fleet 부하를 대체하지 않는다.

### Marker별 근거

| marker | 입증 결과 |
|---|---|
| T01 | §T01 관측 계약 및 공개 요청 소표본. 2분 재확인, 기본 270초 만료, 요청 2/1회 상한. |
| T02 | migrations 218~220, manifest, schema golden replay, hololive-dbtest 전체 PASS. |
| T03 / V01 | 실제 helper smoke의 1~2/1회·압축 전송량, Node 207개·typecheck·Go contract/collector race PASS. |
| T04 | consumer persist 및 projection race, 실제 refresh transaction과 5초 다음-refresh 생성/제거 관측. |
| T05 / V02 | repository evidence/pending 경계, reducer pending 보존, migration/consumer 전체 race, 위 EXPLAIN/BUFFERS. |
| T06 / V03 | migration 표준 본문만 교체·custom/override/member 보존 검증, 실제 command/repository/template smoke 및 upstream=0. |
| T07 | 전체 build-only/local CI PASS, release activation/rollback/운영 비교 조건은 collector runbook에 기록. |
| AC01 | 음성 확인 유무·UNKNOWN·만료·미해결 후보 DB 경계와 실제 두 안내 문구 관측. |
| AC02 | fresh positive 우선, 공개·멤버 한정·최초공개 fixture, `/live` 예정 연결과 positive 공존 명령 smoke. |
| AC03 | D1/D2 diagnostic-only, missing LIVE head 차단, 보존 pending 회귀 PASS. pending 삭제 정책 변경 없음. |
| AC04 | identity/시각·새 positive 경합·pending 경계 consumer persist PASS. 정확한 종료 시각, no absence slot, UNKNOWN/공개 불가 수명 유지 및 만료 재차단. |
| AC05 | UNPLAYABLE 사실 필드, isLive 생략 종료, status-only/robot/identity/모순 UNKNOWN 경계 테스트 PASS. 실제 robot·익명 isPrivate=true는 미실측. |
| AC06 | channel consumer 격리, 정상 channel endpoint·예정 대기 상태/identity 검증과 실패 교체 테스트 PASS. |
| AC07 | 실제 새 RPC 물리 요청 채널 1~2회·영상 1회, 자동 redirect/retry 없음, ENDED target 제거 및 2분 재확인 계약. |
| V04 | lint·NilAway·race 및 selected plans gate PASS. 전체 catalog는 아래 사전 고지된 외부 결함 때문에 passed로 기록하지 않는다. |

### 전체 catalog 예외와 배포 전 조건

사용자가 사전에 보고한 전체 catalog 결함은 hololive-bot #532의 `docs/review/infra-optimization-release-20260926.md`가 존재하지 않는 DEC를 참조하는 문제다. 재현 확인을 위해 전체 catalog를 다시 실행하지 않았고 해당 문서/결정을 임의 수정하지 않았다. V04의 full-catalog 부분은 면제 사유를 명시한 예외이며, 기능 AC01~AC07·로컬 build/race/lint/NilAway의 성공과 구분한다. 선택된 계획 gate는 strict_validation=passed, gate_passed=true다.

배포 전에 별도 승인된 릴리스 계획으로 (1) 구 reader drain과 migration 218~220/새 API 교체 창, (2) API decoder·claim·target 준비 후 collector binary/helper bundle 순서, (3) 기존 live_snapshot 세대 보존과 새 kind generation 1, (4) backlog가 있을 때 decoder 유지 및 구 API rollback용 index 재준비, (5) 멤버 한정·동시 방송·최초공개·실제 robot·positive와 음성 충돌·부하/지연 운영 비교를 확정해야 한다. 로컬 미커밋 빌드 이미지는 배포 승인된 release artifact가 아니다.

Fallback delta: 새 확인에는 compatibility fallback·재시도·HTML 경로를 추가하지 않았다. 해석 실패는 UNKNOWN이고 기존 feed retry 및 pending 보존 계약을 유지했다.

### Ultracode 실행 이력

조사 2명 → 가용성 독립 검토 1명 → 계약/DB 2명 → helper/Go collector 2명 → consumer/projection 2명 → collection/canonical 검토 2명 → query/reply 검토 2명, 총 13개 subagent 실행을 사용했다. 마지막 query 검토에서 발견한 retained-pending fixture의 observation_id 누락은 수정 후 repository race로 검증했다. 통합·테스트·빌드는 본 세션이 수행했다. 이후 사용자 요청에 따라 새 subagent는 Opus 5.5만 사용하도록 하되, 모델 override 변경 승인 시간 초과로 실제 설정은 바뀌지 않았으므로 추가 subagent를 실행하지 않았다.
