# LiveQuery 구현·최적화 및 제공자 제거 검증 (2026-09-26)

**Decisions:** `DEC-20260926-hololive-live-query-read-model`, `DEC-20260926-youtube-only-stream-providers`

## 조회 대상

2026-09-26 KST에 Holodex `/api/v2/channels?org=Hololive&type=vtuber&limit=100`을 offset 0/100으로 읽었다(100+37개). 운영 인증값은 요청 헤더 생성에만 내부 사용했고 출력·파일에 기록하지 않았다. `hololive-osaka`의 PostgreSQL은 매 세션 `default_transaction_read_only=on`, `statement_timeout=5000`과 `SHOW transaction_read_only=on`을 확인했다. 공개 채널 ID/표시 이름/조직/졸업 여부만 비교했다.

| 범위 | 채널 수 |
|---|---:|
| Holodex Hololive vtuber 채널 | 137 |
| 목록 응답의 이름만으로 필터한 예비 집합 | 123 |
| 예비 집합 중 Holodex inactive=false | 93 |
| 채널 상세 suborg의 HOLOSTARS 6개를 제외한 활성 집합 | 87 |
| DB 활성 등록 Hololive 고유 채널 | 74 |
| 상세 suborg 보정 뒤 활성 Holodex에만 있는 채널 | 14 |
| DB 활성 Hololive에는 있으나 위 Holodex 집합에는 없는 채널 | 1 |

목록 응답은 suborg를 생략하므로 예비 차집합은 20개였다. 일본 HOLOSTARS 후보 6개의 `/channels/{id}` 상세를 추가 조회했고 모두 suborg에 HOLOSTARS가 있어 현행 필터 제외 대상임을 확인했다. 보정 뒤 활성 차집합은 **14개**다. 현행 명령은 inactive를 별도 필터링하지 않는다.

**한계:** 조직 채널 목록의 범위 비교이며 실제 방송 14개가 누락됐다는 뜻이 아니다. 실제 `/live?org=Hololive&status=live&type=stream&limit=50`도 한 번 대조했다. 응답 4개 중 HOLOSTARS 1개를 제외한 Kiara/Kronii/Raora 3개는 모두 DB 활성 Hololive에 등록돼 있고 실제 시작 시각이 있었다. 현재 LIVE snapshot의 대상 누락은 없었다. 비활성 차집합에는 상세 suborg를 추가 조회하지 않았으므로 아래 수는 예비 집합 기준이다.

### 활성 차집합

| Holodex 표시 이름 | YouTube 채널 ID | DB 등록 |
|---|---|---|
| UNIT B [Pre-Debut] | UC3OH5FKQ3qtl4uRme_vZTgA | mekPark |
| hololive Dreams | UCawGfU44CZxd2ZVf_VGijbA | 미등록 |
| Holoearth | UCfpWrWvbA34LmrZ9h4Lbwag | 미등록 |
| Akai Haato (Sub) | UCHj_mh57PVMXhAUDphUQDFA | 미등록 |
| ACHRORA | UChpRPsAeSZn5DistGacR3iA | mekPark |
| Aki Rosenthal (Sub) | UCLbtM3JZfRTg8v2KGag-RMw | 미등록 |
| mekPark | UCLGFMR7JOmPUuLXRh0hliTQ | 미등록 |
| hololive OFFICIAL CARD GAME | UCmms2EE02mK4zdECwrHBpaA | 미등록 |
| Odeholo | UCMyuwtApj--U6jm-NFzZMOw | 미등록 |
| Midnight Grand Orchestra | UCnVbtCwr-5LXxUlGxsgD7sQ | 미등록 |
| Choco Sub Channel | UCp3tgHXw_HI0QMk1K8qh3gQ | 미등록 |
| Takanashi Kiara SubCh. | UCq4ky2drohLT7W0DmDEw1dQ | 미등록 |
| holo indie | UCt4-iv7EP0UU_w733D_r_jA | 미등록 |
| YAGOO | UCu2DMOGLeR_DSStCyeQpi5Q | 미등록 |

반대 차집합은 Shigure Ui (`UCt30jJgChL8qeT9VPadidSw`) 1채널이다. DB가 Hololive로 등록했지만 위 Holodex Hololive 집합에는 없다. 따라서 `org='Hololive' AND NOT is_graduated`로 바꾸면 제외뿐 아니라 추가도 발생한다. 사용자는 차집합 확인 후 **우이를 포함한 활성 등록 Hololive 74채널로 변경**하도록 승인했다. 이 숫자는 실측값이며 코드 상수로 고정하지 않는다.

공유 채널은 전체 활성 roster에서 2개다. SQL join 결과를 멤버 수로 방송 중복 생성하지 않아야 한다. 기존 Hololive 공식·EN·ID·ReGLOSS/ASOBI 일부 그룹은 등록돼 있어 모든 공식/그룹 채널이 차집합인 것은 아니다.

보정된 14개는 공식·프로젝트 7개(hololive Dreams, Holoearth, OFFICIAL CARD GAME, Odeholo, Midnight Grand Orchestra, holo indie, YAGOO), 서브채널 4개(Haato/Aki/Choco/Kiara), mekPark 계열 3개(Unit B/ACHRORA/mekPark)다. 그룹 분류는 공개 채널 이름과 DB 조직을 해석한 것이며 DB 자동 변경에 사용하지 않는다.

### 비활성 차집합

현행 문자열 필터 뒤 활성 등록 Hololive 집합에 없는 비활성 채널은 30개다. Holodex inactive와 DB graduation은 다른 속성이므로 자동 동치화하지 않는다.

## 읽기 전환 조건

2026-09-26 00:58:56 UTC의 **단일 SQL snapshot**에서 유효 CURRENT projection의 live_snapshot target은 119채널(주기 120초), 그중 Hololive 74채널이다. 제안 freshness 270초 안의 consumed slot 중 `coverage.requested_channel_ids`에 채널이 있고 `filters.statuses`에 LIVE가 있는 것은 Hololive 3/74, 다른 조직 0/45채널이었다. effective/received/scheduled 미래 clock을 제외했다. 3/74는 최신 시도 성공률이나 실제 방송 수가 아니다.

직전 조회의 LIVE 관련 상태: session=ENDED/head=LIVE 24건(후속 조직 집계 Hololive 19, VSpo 3, mekPark 2), session=LIVE/head=LIVE 20건, session=LIVE/head 없음 9건(활성 roster에 없음). canonical writer나 데이터 정정을 이 작업에서 수행하지 않았다.

`youtube_live_absence_slots`는 통계상 약 1,467,138행/heap 571,711,488바이트였다. 확인된 운영 인덱스는 observation_id PK와 requested_channel_ids GIN이다. 이후 격리 EXPLAIN과 최종 repository 비용은 아래에 기록했다. 운영 인덱스를 변경한 것은 아니다.

`youtubejscollector.liveSnapshotPayload`는 결과에 나타난 status만 coverage로 만든다(빈 sessions에만 LIVE+UPCOMING). 따라서 ENDED/UPCOMING만 있는 COMPLETE를 읽기 모델에서 LIVE complete-empty로 바꿔 해석할 수 없다. `PARTIAL`, MissingTab, stale positive, head/session 불일치도 부재 증명이 아니다. 대상 승인을 반영한 P0와 격리 query cost의 P1은 후속 검증을 마쳤으며, 운영 coverage·상태 정합성 및 실제 부하에 대한 전환 조건은 여전히 별도다.

## 실제 검증

고정된 설치 Go 1.27.1 바이너리와 `GOEXPERIMENT=jsonv2 GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`, `-mod=readonly -race -count=1 -timeout=120s`로 다음 기존 작업을 재검증했다.

- shared: `./pkg/service/holodex/provider`, `./internal/service/youtube/reconcile/live`, `./internal/service/youtube/reconcile/schedule` 통과.
- API: `./internal/planes/bot/internal/command/handlers -run 'Test.*(Live|Chzzk)'` 통과.
- 처음 PATH의 Go 1.27.0은 모듈의 1.27.1 요구로 시작하지 못했다. 이미 설치된 1.27.1 바이너리로 재실행했으며 toolchain/의존성을 변경하지 않았다.

이는 제공자 제거 전 baseline이다. 제거 후 검증은 아래에 기록한다. 운영 배포·데이터/secret/queue 변경·Git 발행 없음.


## P1 격리 query cost

`hololive-dbtest.NewPool`로 production migration을 재생한 PostgreSQL 18.6에 합성 absence slot 1,500,000행을 넣었다. 119개 채널에 2분 간격 이력을 균등 분배했고 6개 채널은 LIVE+UPCOMING, 나머지는 ENDED+UPCOMING coverage로 만들었다. 실제 데이터 복제 없이 약 17일 이력과 소수의 유효 LIVE coverage를 재현했다. 74채널을 한 SQL의 lateral 조회로 읽었고 최근 270초 및 미래 clock 배제 조건을 적용했다.

| 인덱스 | 3회 execution ms | shared hit blocks | shared read blocks | 반환 행 |
|---|---|---|---|---:|
| 기존 channel GIN + PK | 4018.396 / 3253.952 / 3262.065 | 867942 / 876947 / 877025 | 66535 / 57530 / 57452 | 74 |
| LIVE coverage 시각 부분 인덱스 | 1.116 / 1.113 / 1.091 | 1278 / 1280 / 1280 | 2 / 0 / 0 | 74 |

검증 후보:

```sql
CREATE INDEX probe_live_coverage_time
ON youtube_live_absence_slots (effective_at DESC)
WHERE (coverage -> 'filters' -> 'statuses') ? 'LIVE';
```

조회는 각 채널에 대해 `coverage->'requested_channel_ids' ? channel_id`, LIVE status 포함, `effective_at BETWEEN as_of - interval '270 seconds' AND as_of`, `received_at <= as_of`, `scheduled_for <= as_of`를 적용한 `ORDER BY effective_at DESC LIMIT 1`이다. 기존 계획은 채널별 과거 slot을 GIN으로 넓게 읽고 시각/typed status로 버린다. 후보는 시간 범위의 eligible slot을 읽는다. 이 실험은 운영 p50/p95, pool wait, writer 비용, 실제 consumer 부하 측정이 아니다. 전체 채널 LIVE와 canonical 종료 이력은 아래 최종 repository benchmark에서 추가 검증했다.

`TestLiveQueryCostProbe`는 29.05초에 통과했고 일회성 측정 코드는 실행 후 제거했다. 후보 인덱스도 해당 격리 DB와 함께 회수됐다. 사용자의 인덱스 migration 코드 준비 승인을 받아 `211_live_query_coverage_time_index.sql`, manifest 072, schema golden을 추가했다. `CREATE INDEX CONCURRENTLY IF NOT EXISTS`이며 새 테이블·compact projection·writer가 없다. 운영 schema/원천 coverage/상태는 바꾸지 않았다.

## 1차 구현과 비용

bot-plane `service/livequery`가 기존 PostgreSQL pool로 한 번의 read-only SQL statement를 실행한다. roster·유효 projection/target·canonical session/head·pending end·eligible LIVE slot을 같은 snapshot에서 읽는다. query deadline 1초는 pool 및 lock 대기까지 포함한다. `!라이브`는 typed 결과를 표시하며 unknown·partial을 방송 없음으로 바꾸지 않는다. 무인자 범위는 승인된 동적 활성 Hololive roster이고 멤버 지정은 해석된 채널을 직접 조회한다. 외부 이름 보강이나 명령의 YouTube 원천 호출은 없다.

최종 SQL은 부분 인덱스로 최근 5분 LIVE slot만 읽고 `coverage_channels AS MATERIALIZED`에서 JSON 채널 배열을 한 번 펼친다. 채널별 freshness는 더 좁은 `min(5분, 2×poll interval+30초)`로 적용한다. 중간 구현의 채널별 반복 JSON 전개를 제거해 all-live p95 28.24ms였던 병목을 해소했다. 기존 active-state 인덱스로 종료 이력도 제외한다. 정렬/limit은 scope 뒤에 적용하고 limit+1로 잘림을 판정한다.

재현 가능한 `BenchmarkRepositoryLongHistory`는 합성 absence slot 1,500,000행, 종료 session/head 50,000건, 요청 roster 74채널과 실제 LIVE 4개/74개를 사용한다. production migration을 재생한 격리 PostgreSQL 18.6, Go 1.27.1, AMD Ryzen 5 5600G에서 각 30회 측정했다. 실행 인자는 `go test -mod=readonly -run '^$' -bench '^BenchmarkRepositoryLongHistory$' -benchtime=30x -benchmem ./internal/planes/bot/internal/service/livequery`이며 API 모듈에서 실행한다.

| 최종 조회 | wall p50 | wall p95 | pool acquire 평균 | EXPLAIN execution | shared hit/read | coverage index rows/loops |
|---|---:|---:|---:|---:|---:|---:|
| LIVE 4개 | 1.874ms | 3.894ms | 0.1720µs | 1.748ms | 78 / 0 | 18 / 1 |
| LIVE 74개 | 3.829ms | 6.240ms | 0.2192µs | 3.501ms | 923 / 0 | 357 / 1 |

active session index의 실제 조회 행도 4/74건이며 all-live에서는 active-head bitmap index를 사용했다. 최종 benchmark 전체가 24.060초에 통과했다. 직렬 격리 측정으로, 운영 동시 요청·consumer 지연·writer 비용이나 배포 후 SLO를 증명하지 않는다. 원천 요청 감소와 indexed read 비용을 검증한 것이며 운영 전에는 실제 부하를 별도로 확인해야 한다.

## 비유튜브 fadeout과 남은 계약

Chzzk/Twitch client·설정 소비·API/worker DI·scheduler loop·platform mapping·명령 및 알림 링크 병합을 제거했다. 참조가 없어진 streamfeed/streamschedule/streamcommon, 관련 helper와 cache prefix도 제거했다. 새 대체 provider, fallback, poller, pool, 조회 캐시 또는 runtime dependency를 추가하지 않았다. `.env.example`과 Compose의 퇴역 provider 설정도 정리했다.

기존 Stream JSON 필드와 DB 멤버 플랫폼 식별자는 외부 계약/저장 데이터로 남는다. 퇴역 mapping key의 예약 이름은 운영 캐시에 남은 키를 방 구독으로 오인하지 않도록 유지한다. 구형 비유튜브 알림 envelope는 전송 경계에서 거부하고 기존 유한 retry/DLQ 계약으로 종료한다. 별도의 retry/fallback 경로는 없다. X Spaces·기념일·digest와 YouTube writer/시작·종료 admission/outbox/retention은 유지한다. Fallback delta: none.

남은 Holodex 조회의 Service/key별 cache-fill은 완료 신호만 공유한다. 성공한 캐시를 각 호출자가 다시 읽고 결과 소유권을 보존한다. owner 취소·upstream 오류·cache read/write 실패를 성공이나 캐시 빈 목록으로 합치지 않는다. 실패 시 각 caller의 기존 호출 계약을 유지하며 실패까지 단일 upstream 호출로 보장하지 않는다. 기존 TTL, org/status/hours와 예정/일정·외부 Stream API 원천은 유지한다.

## 최종 검증과 한계

고정된 기존 Go 1.27.1, jsonv2, `-mod=readonly`와 offline dependency 설정을 사용했다. 기존 캐시의 고정 uv 0.12.18·golangci-lint 2.13.2·NilAway를 사용했으며 프로젝트 pin/전역 도구/의존성을 갱신하지 않았다.

| 검사 | 실제 결과 |
|---|---|
| LiveQuery repository 실제 PostgreSQL race | fresh LIVE, complete-empty(raw observation 없음), UPCOMING-only, 부분 positive, stale/future/replay, 미수집·만료 projection, head 불일치·누락, 종료 확인, 시작보다 앞선 종료 증거, nullable start, scoped 진단, 공유 채널, 타 조직, 졸업, limit 1/2/3 및 100/101, committed snapshot, read-only, 취소와 lock deadline 통과 |
| 실제 command + repository + template smoke | cold/warm upstream 0회, unknown/partial/complete-empty 표시, 취소·DB 오류 통과. 실제 Kakao 방 전송은 수행하지 않음 |
| API 영향 race | livequery·handlers·formatter·bootstrap·orchestration·bot/runtime·admin/app 통과. 최종 SQL 변경 뒤 livequery/handlers 재검증 통과 |
| worker/shared 영향 race | workerapp·scheduler·dispatchrun, settings·alarmservice·alarm·domain·keys 통과. 퇴역 provider 무전송·기존 DLQ 종료 포함 |
| Holodex provider race | 병합·취소·오류 비캐시·결과 소유권·cache read/write 실패 검사 포함 통과 |
| canonical 회귀 | live/schedule reducer race 및 sourceobservation의 LiveConsumer/End/Evidence/Steady/Start/Confirmed/Unconfirmed 집중 race 통과 |
| 외부 API 회귀 | admin Stream API race 통과. shared httpserver Stream 패턴은 일치 테스트가 없어 compile만 확인했으며 런타임 smoke 통과로 계산하지 않음 |
| migration/schema | migration 211 manifest·실제 migration replay·schema snapshot golden 재생 및 비갱신 검증 통과. golden diff는 새 부분 인덱스 1개 |
| build | hololive-api, alarm-worker, youtube-collector 실제 cmd entrypoint 3개 통과 |
| 정적 검사 | 영향 17개 package의 golangci-lint 0 issues, NilAway 통과, `git diff --check` 통과 |
| 소유 경계 | 전체 `scripts/architecture/ci-boundary-gate.sh` 통과: SQL ownership·DB access·migration manifest·hard structure·notification egress·Compose 등 포함. import graph 갱신 |
| 최종 문서·결정 | 최종 SQL ownership·migration manifest·current docs·local path 재검사 및 `check-decision-catalog.sh check --submodules` 통과: 339 DEC, 11개 생성 색인 current, pending 0 |

stack 전체 `check-stack-db-access-policy`, `check-stack-retry-contract`, `check-stack-projection-tables`는 실행하지 않았다. 상위 작업 지침이 `Iris/native/**`·`Iris/tools/**` 스캔을 금지하는데, 스크립트 또는 하위 검사에서 해당 경로를 읽기 때문이다. 소유 Hololive 검사를 통과한 것을 stack 전체 통과로 확대하지 않는다. hard structure의 기존 advisory는 남으며 hard 실패는 없다.

두 구현 계획의 T/AC/V 근거는 위 범위 조사, 상태/명령 반례, 실제 query plan, provider 제거와 영향 검사다. 이 단계의 완료/검증은 **로컬 코드 및 검증 산출물**에 한정한다. 당시 운영 migration·배포·DB/secret/queue 정리·실제 방 전송·Git commit/push는 수행하지 않았다. 후속 사용자 승인에 따른 독립 리뷰·출판·운영 반영은 `PLN-20260926-live-query-release`가 소유한다. 앞서 관측한 3/74 coverage를 임의로 74/74로 확대하거나 canonical 상태를 수정하지 않았다.

## 독립 적대적 리뷰와 수정

사용자가 서브에이전트의 적대적 리뷰 및 수정·커밋·푸시·운영 반영을 명시했다. DB 읽기와 provider/cache를 서로 다른 reviewer가 읽기 전용으로 검토했다. provider/cache에는 추가 결함이 없었으며 reviewer가 cache-fill race(1.191초), retired provider·고정 egress dispatch race(5.062초)를 독립 실행했다.

DB reviewer는 **P1: 보존 pending을 현재 종료로 오인**하는 결함을 찾았다. canonical reducer는 이미 ENDED인 영상의 반복 종료, 최신 LIVE positive보다 오래되거나 같은 시각의 종료, 최신 UPCOMING positive가 이긴 취소도 pending에 보존할 수 있다. 행 존재만으로 LIVE를 숨기거나 complete-empty를 막으면 안 된다. 2026-09-26 02:24:25 UTC의 guarded 운영 집계에서도 ENDED/ENDED pending 2370건을 확인했다. 이는 운영 데이터를 수정할 근거가 아니라 읽기 predicate를 수정할 근거다.

`unresolved_pending` CTE가 같은 채널의 일치한 terminal 상태 또는 positive가 이긴 종료·취소를 제외한다. 더 새로운 종료, NULL 상태, 시작 전에 도착한 종료, 채널/상태 불일치는 보존한다. roster에 session 채널 또는 pending 채널이 포함되면 검사하여 다른 채널의 pending이 불일치 증거를 숨기지 않는다. canonical state가 전혀 없는 pending은 채널 진단을 유지하면서 영상별 상태 join을 생략한다. writer·retention·운영 데이터는 바꾸지 않았다.

repository의 새 8개 반례 중 4개가 수정 전 SQL에서 실패했다. 수정 후 기존 repository/command 전체 race와 실제 reducer의 보존 반례 3개가 통과했다. 세션 없는 pending의 head만 존재하는 불일치 반례도 추가했다. 최종 repository/handler race는 10.492/8.422초, reducer race는 1.019초였다. reviewer가 predicate·NULL·scope·우선순위를 다시 검토해 기존 P1 해소와 추가 correctness 결함 없음을 확인했다. 영향 lint는 0 issues, SQL ownership도 통과했다.

### 보존 pending을 포함한 최종 비용

1차 benchmark의 3.894~6.240ms에는 pending 이력이 없었다. 아래는 같은 150만 slot·5만 종료 session/head에 **종결 pending 5만 건**을 추가하고, 마지막 두 행에는 **canonical state 없는 pending 5만 건**을 더 넣은 최종 30회 측정이다. `BenchmarkRepositoryLongHistory`와 `BenchmarkRepositoryUnobservedPending`으로 재현한다.

| 조건 | wall p50 | wall p95 | EXPLAIN execution | shared hit/read |
|---|---:|---:|---:|---:|
| 종결 pending 5만, LIVE 4개 | 61.86ms | 66.60ms | 74.92ms | 3135 / 0 |
| 종결 pending 5만, LIVE 74개 | 66.99ms | 72.91ms | 79.25ms | 3424 / 0 |
| 위 조건 + 미관측 pending 5만, 전체 | 108.6ms | 123.4ms | 132.9ms | 4173 / 0 |
| 위 조건 + 미관측 pending 5만, 멤버 | 60.40ms | 64.90ms | 75.34ms | 4171 / 0 |

미관측 pending의 영상별 상태 join을 생략해 같은 전체 조회 p95를 272.2ms에서 123.4ms로 줄였다. partial/미확인 진단은 유지한다. reviewer가 이 마지막 최적화도 재검토해 추가 correctness 결함 없음을 확인했으며 최종 영향 NilAway도 통과했다. pending에는 channel index가 없으므로 이 비용은 이력과 무관하지 않다. 새 인덱스나 writer 변경 없이 위 분포에서 1초 query deadline을 만족했으며 운영 부하나 무제한 이력의 성능을 보장하지 않는다.

후속 전체 publish gate에서 collector가 공유하는 cache-fill 소스의 AP rsync manifest 누락을 발견해 `ap-rsync-files.txt`에 추가했다. AP 실행 코드나 운영 AP를 변경하는 작업은 아니며, 기존 전송 목록의 실제 Go dependency 검사가 이 누락을 차단했다.
