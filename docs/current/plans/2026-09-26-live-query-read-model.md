# 방송 조회 경로와 수집 상태 읽기 모델 개편

**Decisions:** `DEC-20260926-hololive-live-query-read-model` (governing), `DEC-20260926-youtube-only-stream-providers` (constraint), `DEC-20260814-hololive-youtube-three-provider-convergence-v2` (constraint), `DEC-20260829-hololive-live-start-evidence-admission` (constraint), `DEC-20260911-youtube-restricted-schedule-isolation` (constraint), `DEC-20260925-hololive-viewer-collection-retirement` (constraint)

## Execution capsule

**Goal:** `!라이브`를 canonical DB의 bounded read로 전환할 코드를 구현·검증하고 YouTube 중복 원천 조회를 없앤다.
**Context:** 사용자가 구현·최적화, 우이를 포함한 활성 등록 Hololive scope(확인 시점 74채널), LIVE coverage 시간 부분 인덱스 migration 코드 준비를 승인했다.
**Constraints:** 배포·운영 migration·데이터 정정·원천 coverage 확대는 제외한다. Chzzk/Twitch는 제거하며 fallback·새 poller·새 DB pool·신규 읽기 캐시를 만들지 않는다. 예정/일정·Stream API·canonical writer·알림·retention을 유지한다.
**Evidence:** `docs/review/live-query-read-model-20260926.md`의 실제 차집합, read-only DB 집계, 150만 slot·5만 종료 이력의 최종 query cost, 실제 DB/command·provider/consumer race 및 영향 gate 검증.
**Success:** 단일 snapshot의 roster/positive/coverage/pending을 읽어 complete/partial/unavailable을 구분한다. unknown을 방송 없음으로 표시하지 않는다. 실제 명령의 YouTube upstream 0회와 indexed read 비용을 검증한다.
**Output:** LiveQuery·SQL·migration·DI·formatter·실제 검증 및 운영 전환의 남은 조건. 실행 상태는 PLN, 선택과 delivery는 DEC가 소유한다.

## 제안 범위

이 계획은 사용자 지시에 따라 구현 대상으로 수용됐다. 초기 분석과 실측은 [검토 기록](../../review/live-query-read-model-20260926.md)에 있다. 이전 Chzzk adapter/cache 제안은 `DEC-20260926-youtube-only-stream-providers`로 폐기한다.

| 표면 | 변경 범위 |
|---|---|
| 무인자 `!라이브` | DB 활성 등록 Hololive 채널. 실측 74는 상수가 아니다. 우이 포함·Holodex에만 있는 활성 14채널 제외를 승인받았다. |
| 멤버 지정 `!라이브` | 기존 matcher가 해석한 활성 채널 identity를 직접 읽는다. 다른 조직 멤버도 지원하며 전체 목록/limit에서 찾지 않는다. |
| `!예정`, `!일정`, 일정 별칭 `!멤버` | 기존 Holodex 원천·시간 범위·파서 유지. |
| `!정보`, `!방송이력` | 기존 프로필·종료 이력 계약 유지. |
| 외부 Stream HTTP API | org·viewer_count·직렬화·원천 유지. |
| consumer/알림 | canonical state·시작/종료 증거·premiere admission·outbox·보존 소유권 유지. |

## 읽기 계약

`hololive-api/internal/planes/bot/internal/service/livequery`가 query와 typed result를 소유한다. `handlercore.Dependencies`는 좁은 조회 interface만 받으며 handler는 멤버 해석과 전송만 수행한다. formatter는 이미 판정된 결과를 표시한다.

- 요청은 전체/해석된 채널을 구분하고 limit은 1..100으로 제한한다. 잘못된 요청을 기본 전체 조회로 바꾸지 않는다.
- 결과는 items, DB as-of, completeness, 채널별 reason, truncated다. Count는 실제 표시 수다. limit은 scope 필터 뒤에 적용한다.
- item은 YouTube video/channel identity, 표시명·제목·링크, nullable 실제 시작 시각, positive 관측 시각을 가진다. 공유 채널은 한 번 읽고 이름은 안정적으로 일괄 결합한다.
- freshness는 `min(5분, 2 × 대상 poll interval + 30초)`를 로컬 구현의 명시적 읽기 예산으로 사용한다. 현행 120초 주기에서는 270초다. 운영 SLO 달성이나 30초 최신성을 주장하지 않는다.
- `last_seen_at`/metadata 수정 시각/처리 시각은 positive freshness가 아니다. 미래 effective/received/scheduled clock, 지연 소비, 오래된 replay가 freshness를 늘리지 않는다.
- roster, CURRENT projection validity, canonical session/head, pending end와 coverage를 한 SQL snapshot으로 읽는다. DB 시각을 기준으로 판정하며 projection 부재를 inner join으로 숨기지 않는다.
- session=head=LIVE와 fresh positive가 맞을 때만 현재 방송으로 표시한다. stale LIVE, head/session 불일치, head 없는 LIVE, 종료 확인 중은 누락 이유를 보존한다. 조회가 ENDED 전이를 만들지 않는다.
- confirmed positive와 채널 전체 completeness는 별개다. PARTIAL의 positive는 표시할 수 있지만 부재를 증명하지 않는다.
- `youtube_live_absence_slots`의 원래 effective/received/scheduled/scope를 재사용한다. `filters.statuses`에 LIVE가 있는 consumed eligible COMPLETE만 as-of coverage다. UPCOMING-only COMPLETE, 접근 제한, timeout, MissingTab은 LIVE 부재 근거가 아니다.
- 최근 eligible COMPLETE를 유효 기간 안에서 사용하는 as-of 계약이다. 최신 모든 시도의 성공을 주장하지 않는다. 더 최신 PARTIAL 자체가 eligible COMPLETE를 무효화하거나 새 부재 증거가 되지 않는다.
- canonical LIVE·미해결 pending이 없고 모든 대상에 fresh eligible coverage가 있을 때만 빈 결과를 complete로 표시한다. 0 fresh item은 방송 없음의 증거가 아니다.
- DB 오류·deadline은 조회 오류다. 원천 재조회, stale-on-error, 기본값 성공, 재시도·selector·shadow writer는 추가하지 않는다. 사용자 취소를 보존한다.

## 비용과 schema

150만 합성 slot의 기존 GIN 조회는 3.25~4.02초로 1초 후보 예산을 넘었다. 사용자가 승인한 추가 schema는 `youtube_live_absence_slots(effective_at DESC)`에 typed LIVE coverage predicate를 적용한 작은 부분 인덱스다. 새 테이블·compact projection·새 writer가 아니다. 격리 측정 1.09~1.12ms는 운영 성능이 아니다.

migration은 새 번호, `CREATE INDEX CONCURRENTLY IF NOT EXISTS`, manifest·schema golden을 함께 준비한다. 운영 적용은 별도다. 같은 command 안에서 채널별 DB 왕복이나 이름 외부 보강을 금지한다. 기존 bot-plane pool과 1초 query deadline을 사용한다. bounded roster/active-state/최근 coverage를 일괄 읽고 이력 크기 및 모든 채널 LIVE인 반례도 측정한다.

준비된 migration은 211이다. 1차 74채널/150만 slot/5만 종료 이력 측정은 LIVE 4개 p95 3.894ms, 74개 p95 6.240ms였다. 후속 적대적 리뷰에서 보존 pending 해석을 수정하고 종결 pending 5만 및 미관측 pending 5만까지 보강한 측정은 p95 64.90~123.4ms였다. coverage JSON은 최근 slot에서 한 번 전개하지만 pending scan 비용은 이력 수에 비례한다. 상세 rows/buffers와 측정 한계는 검토 기록이 소유한다.

## 작업

### T01 범위와 선행 근거 확정

Holodex pagination·상세 suborg와 read-only DB roster 차집합, 활성/졸업/공유 채널, 사용자 scope·비유튜브 제거 승인을 기록한다. P0에 대응한다. AC01/V01.

### T02 coverage 인덱스와 격리 비용 검증

T01 뒤 migration/manifest/golden을 준비한다. durable evidence의 LIVE scope·replay·retention·partial 의미를 확인하고 대표 장기 이력에서 query plan·rows/buffers를 측정한다. P1에 대응한다. AC02/V02.

### T03 LiveQuery 구현

T02 뒤 bot service와 SQL에 단일 snapshot read, typed state/coverage, scope/limit·이름 일괄 결합을 구현한다. invalid/unknown/partial을 보존하고 state write가 없도록 한다. P2에 대응한다. AC02/AC03/V02.

### T04 명령과 표시 연결

T03 뒤 handlercore/command DI, `handler_live.go`, formatter를 연결하고 대체한 provider 판단·필터 helper만 제거한다. 파서·동명이인·졸업 처리는 유지한다. 원천 호출 없는 실제 command 실행으로 확인한다. P4에 대응한다. AC03/V03.

### T05 남은 제공자 cache-fill과 통합 검증

기존 Holodex cache-fill 병합의 caller 취소·오류 비캐시·결과 소유권을 유지한다. source diff와 영향 build/race/NilAway/lint/architecture/catalog를 검증하고 비용과 배포 전 조건을 남긴다. P5/P6의 로컬 준비에 대응한다. AC04/V03/V04.

## 수용 기준

### AC01 활성 등록 scope

우이를 포함한 동적 Hololive roster, 멤버 지정의 타 조직, 졸업/무효/공유 채널을 검증한다. 기존 Holodex와 달라지는 14채널은 승인된 범위 변경으로 기록한다.

### AC02 정확한 as-of 상태

fresh positive, metadata-only/future last_seen, complete-empty, UPCOMING-only, partial, stale/pending/head mismatch, future clock, late arrival, replay, 신규 대상/만료 projection을 검증한다. raw retention 후에도 slot을 읽는다. 일관된 snapshot·read-only와 미확인 결과의 원인을 보존한다.

### AC03 명령 동작과 비용

cold/warm 모두 YouTube upstream 0회. invalid 요청·취소·DB 실패를 빈 목록으로 바꾸지 않는다. 전체/멤버 동일 predicate, 0/1/limit/limit+1의 표시 수·정렬·truncated를 확인한다. fresh 목록이 빈 partial/unavailable에는 방송 없음 문구가 없다.

### AC04 기존 계약과 fadeout

비유튜브 실행 코드 제거를 유지한다. 예정/일정·Stream API와 canonical writer/알림/retention 불변. 새 fallback·retry·조회 캐시·pool·runtime dependency가 없다. 기존 cache-fill은 한 Service/key 안에서 성공한 cache write 후에만 합쳐지며 upstream/cache 실패까지 한 번으로 합친다고 보장하지 않는다.

## 검증

### V01 범위 실측

검토 기록의 bounded Holodex/DB 비교와 승인된 scope를 확인한다. 대상 수를 고정하지 않는다.

### V02 격리 PostgreSQL과 query plan

소유 `hololive-dbtest`의 실제 migration, schema golden, repository 상태 반례·snapshot/cancellation 검사와 대표 데이터 EXPLAIN/ANALYZE/BUFFERS. 운영 rows나 secret을 복제하지 않는다. query-only 비용과 운영 consumer/pool 지연을 구분한다.

### V03 명령·제공자 회귀

고정된 Go 1.27.1과 jsonv2, `-mod=readonly`로 LiveQuery/handlers/formatter/orchestration/provider/consumer와 비유튜브 제거 영향 패키지 focused 검사 및 race/NilAway/lint. 실제 command smoke에서 원천 호출이 없음을 검증한다. source 문자열이나 배선 재고정 테스트로 대체하지 않는다.

### V04 결합 gate와 전달

`git diff --check`, migration/SQL/DB/architecture와 decision catalog gate. 영향 스택 projection/DB gate는 금지된 Iris 경로를 읽지 않는 경우 실행한다. 검증 코드·근거·PLN/DEC를 대조한다. 실행하지 않은 운영 측정은 명확히 남긴다.

## 전환과 복구

배포·운영 migration·방 전송은 이 구현 요청에 포함되지 않는다. 운영 전환 전에 인덱스 적용, 필요한 fresh positive/eligible coverage, head/session 불일치 및 예산을 owning ops 절차로 다시 확인해야 한다. 현재 관측된 3/74 coverage를 전체 수집 완료로 해석하지 않는다. 잘못된 방송 없음·설명되지 않은 누락·canonical/outbox 변화·DB 예산 초과는 전환 차단/이전 검증 바이너리 복구 조건이다. 명령의 일반 request fallback으로 원천을 다시 부르지 않는다. additive index는 rollback 때 유지하며 destructive down migration을 하지 않는다.
