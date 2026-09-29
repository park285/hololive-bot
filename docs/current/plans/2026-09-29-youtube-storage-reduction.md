# Hololive DB 보관·적재량 축소와 성능 개선

## 목표와 범위

보관 기간, 관측당 저장량, 신규 적재량, 물리 공간을 함께 줄인다. 알림 누락·중복, 방송 시작/종료 판정, 유지할 기능의 조회 정확도와 속도를 희생하지 않는다. 사용자 추가 결정으로 채널 통계·증감률은 비활성화가 아닌 **코드·계약·스키마·기존 데이터 완전 제거**이며, 운영 DB에 빈 전용 테이블·과거 통계 원장·압축 archive도 남기지 않는다.

2026-09-29 계획 작성 뒤 사용자가 구현과 Sol 병렬 작업을 승인하여 로컬 구현·검증을 진행했다. 운영 migration·배포·데이터/백업 삭제·새 런타임 의존성 도입은 별도 승인 경계다. 기존 [보존 정책 적용 계획](2026-09-28-youtube-retention-capacity.md)과 [운영 적용 기록](../../review/2026-09-28-youtube-retention-rollout.md)을 기준선으로 사용하며 과거 적용 기록은 다시 쓰지 않는다.

- 유지할 기능의 제품 품질과 **새로 정한 보관 기간 안의** replay 결과는 보존한다. 채널 통계는 기능·과거 replay·저장 데이터까지 완전히 제거한다. 나머지 감사의 상세도·기간은 아래 정책에 따라 줄이되 남기기로 한 기록은 정확하게 보존한다. 이미 삭제한 데이터는 설정 원복으로 돌아오지 않는다.
- 채널 통계 외의 수집 주기·대상·알림 빈도를 낮추거나 같은 payload의 다음 슬롯 관측을 생략해서 용량을 줄이지 않는다. UNKNOWN/PARTIAL을 성공·완전한 부재로 바꾸지 않는다.
- 유지할 기능의 canonical 최신값, 채널 기본정보·알림 구독, 알림 intent/dispatch/dedup 원장, active queue·pending replay·live head/end candidate 보호 조건은 유지한다. 통계 전용 명령·템플릿은 완전 제거 대상이며 알림 구독 명령과 구분한다. 보관 기간은 삭제 후보 조건이지 보호된 행의 강제 수명 상한이 아니다.
- 수치 목표는 **동일 입력·새 정책의 안정 상태에서 전체 relation 용량 40% 이상, 신규 DB 적재 바이트/일 30% 이상 감소**다. 현재 약 26 GiB에서 15~16 GiB 수준은 검증 목표이지 예측·약속이 아니다. retention 효과와 구조 개선 효과를 따로 측정하고 중복 합산하지 않는다.

## 확인한 근거와 한계

2026-09-29 hololive-osaka/holo-postgres/hololive, PG 18.6, 읽기 전용 세션에서 확인했다.

| 항목 | 확인값 / 설계 의미 |
| --- | --- |
| 전체 / 원본 / application | 25.9 / 14.4 / 6.9 GiB. 원본 TOAST 약 7.0 GiB, application 인덱스 약 2.6 GiB |
| payload 반복 | 서로 다른 0.2% 블록 표본에서 live 저장 payload 바이트의 57.5~58.5%, community 45.2~47.8%, video 86.5~86.6%, shorts 64.9~66.0%가 표본 내부 중복 |
| 개별 payload 압축 | 318행의 JSON 직렬화본 gzip-6은 주요 kind에서 현 payload 저장 바이트보다 약 24~31% 작음. 개별 Zstd-3가 항상 더 작지는 않음 |
| orphan 감사 압축 | 288행에서 개별 행 압축은 오히려 커짐. kind별 48행 Zstd 묶음은 행 데이터 바이트 약 67~91% 감소 |
| application fan-out | 별도 표본에서 관측당 community 약 6.8행, video/shorts 약 11행, live/viewer 약 1행. 모든 kind에 헤더 테이블을 더하는 설계는 손해일 수 있음 |
| 불필요한 index entry 후보 | application UNIQUE 인덱스 1.62 GiB. 35,355행 표본 중 observation_id=NULL 34.7%. NULL을 제외한 부분 UNIQUE 후보 |
| projection | CURRENT 1세대/660 targets, RETIRED 3,271세대/약 308만 targets. targets+reasons 약 1.9 GiB |
| 기존 정리 속도 | 245초 관측에서 원본·application 각각 2,000행 삭제. 120초/1,000행 상한에 도달 |
| 채널 통계 퇴역 실측 | snapshots 65 MiB/80,645 추정 행, legacy history 7,200 KiB/32,978행, changes 2,480 KiB/14,440행. evidence/heads는 통계상 0행, channel_stats queue join 집계도 0행. 재확인 전 운영 삭제의 근거로 재사용하지 않음 |

압축 실험은 호스트 메모리 안에서 직렬화 바이트 복원만 검증했다. 원문을 파일·채팅으로 내보내지 않았다. 새 codec의 canonical hash·실제 replay, 전체 DB 압축률·CPU/지연은 미검증이다. 블록 표본 편향, 신규 인덱스·메타데이터, 이미 할당된 빈 공간을 포함하지 않는 절감률을 전체 DB에 외삽하지 않는다.

## 보관 정책 후보 — 압축보다 먼저 결정

다음은 운영 적용 전 검증할 제안값이다. 현재값은 앞선 운영 조회와 loader 기본값에 근거하며 적용 직전 실제 container 설정으로 다시 확인한다.

| 데이터 | 현재 → 제안 | 유지할 정보 / 줄어드는 능력 |
| --- | --- | --- |
| live/community/video/shorts 원본 | 14 → **7일** | 슬롯 증거·replay는 7일, 이후 원본 조사 종료. 제품 canonical은 별도 유지 |
| schedule 원본 | 30 → **14일** | 미래 일정 canonical을 지우는 정책이 아님 |
| channel/video live-check 원본 | 기본 7 → **2일** | 최신 확인값·시각·판정 provenance 유지. 늦은 재처리 경계 검증 |
| channel_stats 전체 | 180일 원본·제품 이력 → **기능·코드·스키마·관련 데이터 완전 제거** | 구독자/총 조회수/영상 수·증감률·추이 및 과거 통계 replay를 제공하지 않음. TTL=0·빈 테이블·비활성 flag·호환 응답·archive로 남기지 않음 |
| channel profile/photo 원본 | 180 → **30일** | 채널 ID·이름·프로필·사진 등 유지 기능과 통계 수치를 분리. stats와 공용인 metadata fetch를 통째로 끄지 않음 |
| 퇴역 viewer 원본 | 30 → **7일** | 신규 발행은 이미 퇴역. 과거 replay 범위 축소, FK dependent evidence 정리. 제품 samples의 별도 보존·삭제 소비자도 조사 |
| application 추가 감사 유예 | 14 → **3일** | 현 식은 applied_at < now-(원본 kind 기간+유예), observation_id=NULL. 일반 28→10일, schedule 44→17일, profile/photo 194→33일의 후보 경계이며 원본 보호에 따라 더 남음. channel_stats 관련 이력은 TTL 대기가 아니라 완전 제거 절차로 정리 |
| PROCESSED 관측 queue | 7 → **1일** | terminal만 삭제. raw evidence가 남으면 기존 replay가 queue를 재생성하며 중복 알림을 막아야 함. dispatch 원장에는 적용하지 않음 |
| 관측 DEAD_LETTER queue | 90 → **14일** | terminal 조사 기간 축소. active/PENDING/PROCESSING은 age만으로 삭제하지 않음 |
| collision / terminal replay audit | 365 → **30일** | 실패·충돌을 정상 집계로 덮지 않음. replay audit >= 최장 raw evidence 30일 검증 유지 |
| 과거 checkpoint | 7 → **2일** | 기존 최신 checkpoint 보호 유지, 오래된 scope 이력만 제거 |
| live absence slot | 30 → **14일** | 가장 긴 관련 일정 원본과 정렬. old positive·일정 merge·pending end·replay를 함께 검증하고 기존 migration 221 보호 유지 |
| RETIRED projection | 30 → **7일** | lease 참조·CURRENT/STAGING 보호. 과거 7일 정리 시간 초과의 원인 경로 수정 후 적용 |

기존 정책의 최대 age에 맞춰 소비자·정본이 동작하는지 확인하지 않고 TTL만 낮추지 않는다. 원본 만료 후 재처리 요청은 만료를 명확히 보고하고, 재수집·빈 결과·성공으로 대체하지 않는다. 개별 감사 이력 소멸이 의도된 정책 변경임을 문서·운영 복구 계약에 반영한다.

## 실행 순서와 설계 선택

### 1. 동등성·성능 기준선을 먼저 고정

- [ ] 최신 실제 age와 canonical/제품 시계열/dependent evidence의 수명을 분리해 목록화한다. viewer 제품 samples 등 원본 TTL로 사라지지 않는 데이터의 실제 읽기 경로·보존 필요를 확인하고, 미사용이라고 입증되기 전 임의 삭제하지 않는다.
- [ ] kapu의 실제 PostgreSQL과 기존 dbtest fixture로 현재 구조/후보 구조에 **같은 관측 집합·보호 상태·수명 정책**을 넣는다. 기존 정책 대비 절감과 동일 신규 정책에서의 구조 절감을 분리한다. 과거 데이터 원문 복사 없이 kind/크기/중복률/fan-out 분포를 반영한 fixture를 쓴다.
- [ ] 이미 있는 `sourceobservation/performance_test.go`와 `livequery/repository_benchmark_test.go`를 활용한다. publish→consume→canonical/intent까지, claim/replay, 현재 LIVE 조회, 감사 단건·기간 조회, retention과 동시 부하를 측정한다. baseline/candidate를 교대로 반복하고 cold/warm cache, 반복률 0%·표본 수준·높은 수준, 정상 및 2배 입력, fleet 동시 publish를 포함한다.
- [ ] 행 수가 아니라 relation/TOAST/index 총 bytes, 신규 영속 bytes·WAL/관측, CPU·alloc·pool/lock wait, p50/p95/p99, 처리량·queue age를 비교한다. 계측용 임시 smoke는 완료 후 제거하며 benchmark를 위한 별도 영구 게이트 체계는 만들지 않는다.

### 2. 채널 통계 코드·스키마·데이터를 먼저 완전히 제거

- [ ] 표시와 조회를 함께 제거한다. 채널 구독자 수·총 조회수·영상 수·증감률·추이의 명령/화면/API/템플릿/도움말을 조사해 모든 실제 소비자를 전환한다. 확인된 `SubscriberCommand`는 snapshot DB가 아니라 `Holodex.GetChannel`을 직접 조회하므로 DB 삭제만으로 없어지지 않는다. 이 통계 전용 명령과 formatter·전용 템플릿도 제거하며 0·빈 통계·비활성 호환 응답으로 남기지 않는다. 공용 채널 조회·채널 기본정보·알림 구독과 개별 영상의 다른 기능은 유지한다.
- [ ] YouTube.js와 Holodex의 `channel_stats` payload 생성·target·job emission·publish, API consumer·reconcile/persist, 통계 설정·metric·공개 계약·문서·관련 테스트를 함께 제거한다. `youtubejs_channel_metadata`와 `holodex_metadata`는 profile/photo를 함께 처리하므로 필요한 fetch·오류 경계·lease/checkpoint는 유지한다. 중첩 batch의 통계만 제거한 뒤 프로필·사진·방송 관측이 계속 발행되는지 확인한다. 외부 요청 수까지 감소한다고 미리 주장하지 않는다.
- [ ] 새 발행 중단→fleet in-flight drain/확인→통계 queue/replay의 실제 잔여 처리 정책 확정→소비자 제거→전용 데이터·schema 제거 순서로 전환한다. 미처리를 PROCESSED/APPLIED로 꾸미지 않고 기능 제거 대상으로 처리한다. 전환 사실은 비밀 없는 배포 결과에 기록하며 최종 운영 DB에 폐기 작업 원장·tombstone을 새로 남기지 않는다. kind 관련 코드·SQL vocabulary/FK·등록행은 데이터가 남아 있는 상태에서 먼저 제거하지 않는다. 전환 종료 후 구버전 전용 decoder·retention loop·alias·비활성 설정을 남기지 않는다.
- [ ] `youtube_channel_stats_snapshots`, `youtube_channel_stats_evidence`, `youtube_channel_stats_heads`, `youtube_stats_history`, `youtube_stats_changes`와 전용 index/sequence/view/function/trigger/grant를 새 migration으로 제거한다. 공유 source 테이블의 `channel_stats` 원본/application/checkpoint/replay/collision/conflict/offset/target/reason 및 contract generation 등록행은 실제 kind·entity 조건과 FK를 확인하여 bounded 정리한다. 공유 테이블·모델에 남는 통계 전용 열도 실제 소비자를 확인해 제거한다. 이름이 비슷한 `youtube_stream_stats`·viewer 데이터·profile/photo 및 다른 kind의 오류/이력은 이 단계에서 함께 지우지 않는다. DROP CASCADE나 공용 테이블 TRUNCATE로 범위를 넓히지 않는다.
- [ ] live DB의 실제 참조·행 수·유입 0·정확한 삭제 대상·필요 복구점을 운영 실행 전에 다시 제시한다. 운영 DB에 별도 보존용 통계 테이블·압축 archive·복원용 사본을 만들지 않는다. 삭제 이후 통계 복원은 구 이미지 기동으로 불가능함을 명시한다. 기존 백업·보관용 WAL은 아래 정리 단계에 포함하되 활성 pg_wal을 파일 삭제로 처리하지 않는다. DB 삭제·배포·백업 제거는 이 계획 수정과 별개의 승인 경계를 유지한다.
- [ ] 제거할 통계의 출력 동등성은 요구하지 않는다. 대신 남길 멤버/프로필·사진/일정/방송·알림 구독 흐름의 실제 smoke와 회귀로 품질을 확인한다. 대시보드 전체나 시스템 운영 지표를 통계라는 이름만으로 삭제하지 않는다. 기능 제거로 줄어든 저장량과 계속 남는 서비스의 성능 개선을 분리 측정한다. 확인된 전용 테이블은 약 75 MiB이므로 이것만으로 수 GiB 절감을 주장하지 않는다.
- [ ] 완료 조건은 비활성 flag가 아니라 **전용 DB 객체 없음, 공유 테이블의 통계 업무 행 없음, 통계 신규 적재 경로 없음, 유지 기능 정상**이다. 읽기 전용 catalog/정확한 조건 집계와 실제 publish smoke로 확인한다. 영구적인 이름 재등장 grep guard나 검사기 self-test는 만들지 않는다.
- 적용 완료 migration 파일과 `schema_migrations` 행은 schema 재생·무결성을 위한 이력이지 잉여 통계 데이터가 아니므로 보존한다. 과거 migration에 통계 DDL이 있다는 이유로 이력을 삭제·수정하지 않는다. `DELETE` 뒤 공유 테이블의 빈 페이지와 백업/WAL 흔적은 업무 행 잔류와 구분하며, 물리 공간 회수와 백업 처분은 아래 별도 단계에서 판단한다. 이 계획은 매체상의 복구 불가능한 보안 소거를 보장하지 않는다.

### 3. 기간 축소를 버틸 정리 경로와 작은 저장 구조 개선

- [ ] 원본/queue/application의 기존 SKIP LOCKED·보호·kind cutoff를 유지한다. projection은 `LIMIT 세대 수` 뒤의 무제한 FK cascade를 **실제 reasons/targets 행 수와 실행 시간으로 제한**한다. RETIRED·lease 비참조를 트랜잭션 안에서 재검증하고 bounded reasons→targets→빈 generation 순서로 정리한다. 외부 lease INSERT와의 경쟁도 FK·잠금으로 막고 CURRENT를 부분 삭제하지 않는다.
- [ ] `observation_id IS NOT NULL` 부분 UNIQUE를 만들고 `repository_application_insert_0014_14.sql`의 ON CONFLICT 추론 조건을 함께 전환한다. NULL 행 의미, community CANONICALIZED 조회, concurrent duplicate를 검증한다. 기존 UNIQUE는 constraint-owned이므로 DROP INDEX CONCURRENTLY로 단독 제거할 수 없다. 새 인덱스·호환 writer 검증 후 짧은 lock budget의 constraint 교체를 별도 migration으로 처리한다.
- [ ] 유지될 hash는 DB 내부 32바이트 값과 길이 제약으로 전환하되 외부 hex/canonical 계약은 유지한다. 이후 payload 분리로 없앨 중복 열을 먼저 대형 backfill하지 않는다. kind/entity 이름의 소형 ID화는 실제 index byte 이득과 JOIN 비용이 검증될 때만 포함한다.
- [ ] source/application 및 필요 TOAST의 vacuum을 해당 테이블에만 튜닝한다. 60초/1,000행은 처리량 목표 후보이며 WAL·지연·vacuum 여력 확인 뒤 적용한다. batch 상한을 키우거나 timeout을 늘려 projection 실패를 숨기지 않는다.
- [ ] 정리 경로 검증 뒤 위 TTL 변경을 별도 승인된 운영 변경으로 적용한다. 압축 이전의 잔존량·신규 유입을 다시 측정해 다음 단계의 실제 이득을 계산한다.

### 4. 새로 쌓이는 바이트·행·쓰기 증폭 감소

- [ ] **payload 공유 저장:** 관측 헤더의 슬롯·identity·scope·evidence hash는 그대로 남기고 동일 schema/kind/canonical-profile의 payload만 공유한다. 8바이트 참조와 고유 payload 저장소를 우선 시제품으로 삼으며, 첫 구현은 JSONB/pglz를 유지한다. 전체 문서를 다시 압축하는 것보다 반복 TOAST INSERT를 없애는 것이 목적이다.
- [ ] 이미 배치화된 `repository_publish.go`/`repository_publish_batch.go`/`repository_publish_set_0032_32.sql`을 그대로 활용한다. batch 안의 payload를 미리 dedup하고 서버 측 set 처리로 해결한다. 행별 SELECT/INSERT, DB 왕복 N배 증가, ID 획득용 무의미한 UPSERT UPDATE, 공유 payload의 매 관측 refcount UPDATE를 금지한다. 동시 insert 가시성·정렬된 lock 순서·GC와 publish 경쟁을 실제 PG에서 검증한다.
- [ ] claim은 같은 조회에서 payload를 JOIN해 읽는다. payload miss·해시 불일치는 오류이며 재수집이나 옛 저장 경로 fallback을 만들지 않는다. unreferenced payload GC는 참조 인덱스와 bounded 삭제로 처리하고 살아 있는 참조를 보호한다.
- [ ] **application 신규 적재량:** 반복 APPLIED를 모두 상세 행으로 쌓는 대신 관측 단위 공통 receipt와 필요한 entity 결과로 줄이는 시제품을 비교한다. 먼저 raw 수명 동안의 `(observation_id, entity_kind, entity_key)` 최소 멱등 정보와 CANONICALIZED 조회를 보존한다. 같은 값의 반복 확인도 freshness/clock 의미가 있으므로 관측·정본 시각 갱신 자체를 생략하지 않는다.
- [ ] 이후 정상 반복 확인은 receipt에 개수·결정 종류·근거 hash를 묶고, 실제 값/상태 전이·실패·충돌·UNKNOWN 및 community 결정에 필요한 상세를 남기는 정책을 검증한다. 상세 entity별 감사가 사라지는 부분은 **정보량 축소**로 명시한다. 단순 payload equality·RowsAffected·APPLIED 문자열만으로 no-op을 판정하지 않는다. 추가 canonical SELECT로 절감분을 소비하지 않고 기존 reconciliation 결과를 이용한다. entity별 중복 방지와 canonical/알림 원장의 동일 결과를 입증하지 못한 종류는 일괄 생략하지 않는다.
- [ ] projection은 이미 same-hash generation 생성을 억제한다. 수집 의미·reason·lease fence를 유지하면서 실제 변경 없는 쓰기만 줄인다. heartbeat/last-seen을 없애거나 generation 전환을 늦춰 용량을 절감하지 않는다.

### 5. 줄인 뒤에도 남는 데이터에만 압축 적용

- [ ] 개별 작은 application JSON 압축은 제외한다. 새 3일 유예 뒤 곧 지워질 이력을 다시 복잡하게 archive하지 않는다. 남은 감사량·읽기 비용상 이득이 있을 때만 원본과 분리된 이력을 kind/subject/만료 경계로 bounded block화한다. 단건/기간 조회용 위치·메타 인덱스 비용까지 포함하고 감사 조회 p99도 비교한다.
- [ ] 고유 payload는 기존 pglz JSONB, PG LZ4 JSONB, gzip 및 고정된 기존 `klauspost/compress` Zstd를 실제 read/publish 경로에서 비교한다. PostgreSQL codec만 바꾸는 안과 Go 압축 bytea를 구분한다. codec/version·길이·무결성·압축 해제 상한을 두고, 최대 크기·손상·hash 동일성·확장 비율을 검증한다. 다중 reader fallback 없이 측정으로 선택한 한 표현으로 전환한다.
- [ ] 높은 압축 레벨, 긴 delta chain, 상시 캐시로 성능 저하를 가리는 방식은 제외한다. compression/GC 작업은 foreground pool·CPU·I/O 예산을 침범하지 않도록 bounded 실행한다. 압축 후 단건 조회나 replay가 느려지면 해당 압축 후보는 채택하지 않는다.
- [ ] 7일 projection 잔존량이 여전히 크면 lease 비참조 RETIRED snapshot을 압축 묶음으로 보존하는 안을 비교한다. reason은 projection hash 입력에 없으므로 hash만 남겨 복원 가능하다고 주장하지 않는다. 증거가 작은 퇴역 viewer나 곧 만료될 데이터용 전용 압축 구조는 만들지 않는다.

### 6. 이행·운영 수용·공간 반환

- [ ] 새 번호 migration·manifest·schema snapshot·최소 grants·소비자 SQL·운영 진단/복구 문서를 함께 바꾼다. 적용된 migration은 수정하지 않고 `db-migrate`만 사용한다. collector AP 전체와 중앙 API의 writer/reader 호환성을 확인한다. `PublishBatch` LSP references에 collector runtime caller가 있음을 확인했으며 배포를 API 단독 변경으로 가정하지 않는다.
- [ ] 원본 권위를 하나로 둔 준비/backfill→검증→cutover로 이행한다. keyset 배치에 동시 DELETE/SET NULL 반영, 마지막 delta 검증, 멱등 재시작, 예상 최대 공간·WAL·lock budget을 포함한다. 순차 단계마다 모든 해당 caller를 전환하고 최종적으로 obsolete 열·함수·경로를 제거한다. 호환 shim·상시 dual-write·shadow 비교용 production 쓰기는 남기지 않는다.
- [ ] 운영 시작 전 정확한 migration·서비스·동시 버전·예상 지연/중단·복구점을 제시해 승인받는다. 무중단을 가정하지 않는다. 짧은 writer quiescence가 필요한 설계라면 이를 숨기지 않고 별도 승인받거나 설계를 바꾼다. 새 구조로 쓴 데이터가 있으면 구 이미지 재기동만으로 rollback되지 않는다는 점과 역변환/복원 절차를 리허설한다.
- [ ] TTL 축소는 이행 rollback과 분리한다. 삭제 전 복구가 필요하면 승인된 보존/백업 대상을 확정한다. 기존 백업을 최신 복구점으로 추정하지 않는다. 기존 자동/off-host 백업 취소 결정을 유지하며, 일회성 새 복구본·off-host 이전·기존 사본 삭제는 각 대상과 영향의 승인 범위대로 처리한다.
- [ ] 기존 중단 기준인 호스트 여유 20 GiB 미만, 6시간에 5 GiB 이상 감소, 반복 timeout을 유지하고 foreground p95/p99·queue age 증가 시 새 backfill/compaction을 중지한다. 서비스 오류·결과 불일치는 즉시 중단 사유다.
- [ ] 기존 backlog 정리 후 최소 7일간 유입·정리·실제 저장량·WAL·vacuum과 알림 품질을 관찰한다. bytes는 live data와 할당된 빈 공간을 구분한다. 파일 재작성은 실제 회수 이득이 클 때만 별도 점검 창/공간/승인으로 시행한다. VACUUM FULL의 강한 잠금을 숨기지 않고 pg_repack·시간 파티셔닝·외부 저장소를 기본 의존성으로 도입하지 않는다.

### 7. 기존 Hololive 백업과 불필요한 WAL 보존 정리

2026-09-29 06:13 UTC 읽기 전용 확인: primary `pg_wal` 832 MiB/52파일, `archive_mode=off`, 복제 slot/sender 0개, `wal_keep_size=0`, `min_wal_size=80 MiB`, `max_wal_size=1 GiB`, `checkpoint_timeout=15분`이다. slot 보존이나 archiving 실패로 WAL이 쌓인 증거는 없다. `max_wal_size`는 절대 상한이 아니며 현재 사용량 전부가 삭제 가능한 과거 이력이 아니다.

kapu `hololive-db-backup.timer`는 disabled/inactive다. `~/.local/share/hololive-db-backup/archive/`는 비어 있고 기존 09-05 static dump는 조회한 root에 없다. `daily/`에는 09-27의 특정 delivery ledger state 암호화 dump 약 1.4 KiB와 메타데이터가 남아 있다. `w4-publication-20260928/private-drop-backups/hololive-ledger-state-before-drop.dump`는 약 4.6 KiB다. 둘 다 최신 전체 DB 백업이 아니다. 중앙 `deploy-backups/`에서 확인한 두 세트는 배포 tree/image 보관 구조이며 DB dump로 분류하지 않는다. 확인한 경로 밖의 사본 존재·용량이나 전체 복원 가능성은 입증하지 않았다.

- [ ] Hololive 소유 백업·dump·임시 복원 DB/volume·보관용 WAL·배포 복구 사본의 정확한 경로, 크기, 생성 시각, 참조, 다른 서비스 포함 여부를 구분한다. 암호문은 이름·크기·manifest 메타데이터만 확인하고 비밀을 출력하지 않는다. 혼합 디렉터리 전체를 지우거나 현재 PGDATA/volume을 백업으로 취급하지 않는다. `iris-ops-backups`는 별도 명시적 승인 전 읽기 전용이며, 그 안의 Iris DB/SQLite WAL과 다른 봇 백업은 Hololive 정리 대상이 아니다.
- [ ] 폐기 기능만의 사본과 수용 완료한 변경의 불필요한 복구본은 정확한 목록·회수량·잃는 복구 시점을 제시한 뒤 승인 범위대로 삭제한다. 전체 DB가 포함된 백업은 통계만의 백업과 구분한다. 최신 정상 상태로의 복구 경로가 없다면 이를 먼저 알리고, 일회성 복구본을 승인받거나 **백업 없는 복구 불가 위험을 명시적으로 수용받기 전** 파괴적 단계를 시작하지 않는다. 취소된 자동 백업을 재활성화하거나 새 보관 주기·timer를 임의로 만들지 않는다.
- [ ] 물리 base backup/증분/PITR용 WAL archive가 추가 발견되면 세대별 의존 체인·보존할 복구 범위를 검증한다. 보존할 base backup 이후 필요한 연속 WAL을 날짜만으로 골라 지우지 않는다. 단독 논리 dump에는 같은 방식의 물리 WAL 체인을 요구하지 않는다. 마지막 사본 삭제 후에는 그 과거 데이터의 복구 수단이 없어짐을 기록한다.
- [ ] 활성 `pg_wal`은 PostgreSQL checkpoint와 recycle/removal에 맡긴다. 파일 직접 삭제, `pg_resetwal`, `fsync`/`full_page_writes`/내구성 비활성화, 검증 없는 replication slot 삭제를 공간 정리 수단으로 사용하지 않는다. 현 832 MiB를 줄이기 위해 checkpoint 주기를 짧게 하거나 WAL 상한을 무조건 낮추지 않는다. 강제 CHECKPOINT도 I/O 영향이 있는 별도 운영 변경이며 기본 작업이 아니다.
- [ ] 이행·대량 삭제가 끝난 뒤 정상 checkpoint 구간의 WAL 크기/생성률·지연을 관찰한다. 줄어든 source/application 쓰기와 인덱스 수가 WAL 발생량을 줄이는지 검증한다. 남은 파일은 정상 재사용 reserve일 수 있으므로 WAL 0바이트를 완료 조건으로 삼지 않는다. 필요 시 원인에 한정해 보존 설정을 조정하고 속도·복구 품질을 재검증한다.
- [ ] 백업 파일 제거 뒤 exact 대상 부재·filesystem 사용량·현행 서비스 정상·선택한 복구 범위를 확인한다. 운영 업무 데이터 0, 사본 정리, 디스크 회수, 포렌식 소거를 구분해 보고한다. `DEPLOYMENT_BASELINE.md`/복제 runbook의 과거 enabled·일일 7세대 설명은 최신 취소 기록·현재 상태와 대조해 구현 시 정합화하며, 문서만 보고 백업이 존재한다고 주장하지 않는다.

PostgreSQL WAL의 자동 회수·재사용 및 checkpoint 비용 근거: [PG 18 WAL Configuration](https://www.postgresql.org/docs/18/wal-configuration.html). 백업 자동화 취소의 최근 근거: `iris-stack/docs/agent-workflows/evidence/2026-09-28-expanded-closeout.md`.

## 채택·완료 기준

- **품질:** 채널 통계의 코드·스키마·데이터 완전 제거를 명시적인 제품 변경으로 수용하고, 유지할 기능은 같은 신규 보관 정책 안에서 canonical 결과·알림 intent·중복 방지·community window·실패 분류·보호된 evidence가 기존과 같아야 한다. 신규 policy 경계 안/밖, late positive/schedule replay, active/pending 보호, concurrent publish/GC, archive 이동 실패·재시작·손상에 대한 소비자 가시 회귀를 기존 테스트 패턴으로 검증한다. 채널 통계 완전 제거, 의도적으로 생략한 상세 감사와 만료 뒤 replay 불가만 승인된 차이로 기록한다.
- **속도:** 대표 부하의 publish/consume·알림 처리 지연·현재 조회·replay·감사 조회 p95/p99와 처리량에 재현되는 퇴보가 없어야 한다. 허용 오차를 성능 저하 허가로 쓰지 않는다. 분산이 커 결론이 안 나면 채택 근거로 삼지 않고 측정을 보완한다. 추가 목표는 주요 DB 경로 2개 이상 p95 또는 CPU/관측 15% 개선이다. 모든 부하에서의 무조건적 가속을 약속하지 않는다.
- **용량:** 새 TTL 정착 효과, 동일 TTL에서 구조 개선, 최종 물리 회수량을 따로 제시한다. 40%/30%는 목표이며 미달을 성공으로 포장하지 않고 실측과 남은 비용을 보고한다. 일회성 디스크 감소뿐 아니라 신규 logical/WAL bytes·행 수와 정상 상태 증가율을 확인한다.
- **검증 소유:** kapu에서 영향 모듈 Go/DB/race와 기존 local-ci/build-only 검증을 수행한다. 실제 API/collector publish→consume→canonical/intent 시나리오는 disposable DB와 외부 발송 없는 기존 경계로 실행한다. 테스트만으로 운영 성공을 주장하지 않는다. 게시 요청 시 기존 pre-push gate, DB/retry/projection 계약의 영향 검증을 적용하며 checker self-test·문서 해시·별도 성능 gate 시스템을 추가하지 않는다.

## 소유 파일과 의존성

- 설정: `hololive/hololive-shared/pkg/config/settings/apiplane/youtube_plane_retention.go`, `deploy/compose/docker-compose.prod.yml`; 실제 host 설정은 stack-platform-ops, runtime은 hololive-bot-ops.
- 발행/조회/보존: `hololive/hololive-shared/pkg/service/youtube/sourceobservation/{repository_publish*,repository_retention*,queries/*}` 및 기존 consumer/persist 경로. 공개 Go/JSON 계약을 변경하기 전 LSP 참조 전수 확인.
- projection: `hololive/hololive-api/internal/planes/youtube/targetprojection/{retention.go,queries/*}`. 스키마: `hololive/hololive-api/scripts/migrations/`, `hololive/hololive-dbtest/`.
- 채널 통계: collector의 `youtubejscollector/{channel.go,mapper.go}`·`holodexcollector/{runner.go,mapper.go}`, 공용 `channel_stats_consumer.go`·`channel_stats_persist.go`·`reconcile/stats/`·kind/payload/job 계약, API target/runtime/settings, `handler_subscriber.go`·`formatter_stats.go`와 명령/템플릿 소비자. 실제 Console 소비자가 발견되면 iris-console owner에서 같은 계약으로 제거한다.
- 기준선이 공통 선행 조건이다. 이후 projection bounded cleanup·부분 인덱스, payload 공유 시제품, application 적재 정책 시제품은 설계상 독립 평가가 가능하나 통합 schema writer는 하나로 둔다. 별도 위임은 사용자 승인 없이 실행하지 않는다.

## 실행 기록 — 2026-09-29

### 구현 및 선택

- 로컬 변경: migration 234–242, 통계 producer/consumer·명령/템플릿·DTO/도메인·등록 경로 제거, 보관 기본값 변경, bounded projection cleanup, application 부분 UNIQUE와 batch INSERT, payload 공유 저장/GC. 과거 migration은 수정하지 않았다. 유지할 채널 정보·사진·방송·일정·알림 구독은 남겼다.
- payload는 kind/schema/canonical profile과 전체 32바이트 digest로 공유한다. 슬롯별 관측·scope/evidence hex 계약은 그대로다. 사전의 JSONB compression은 측정 후 LZ4를 선택했다. 그 밖의 기존 scope/evidence TEXT 열은 이번 구현에서 bytea로 바꾸지 않았다. 큰 테이블의 별도 타입 변환·재작성 효과는 아래 사전 이득에 포함하지 않는다.
- 238은 사전/참조를 준비하고 239–240은 참조 index와 임시 `payload_id IS NULL` 부분 index를 각각 생성한다. backfill마다 처리된 PK 접두사를 재탐색하지 않는다. 241은 구 열·backfill 함수를, 242는 임시 index를 제거한다. 사전 ID 참조 index는 GC/FK를 위해 유지한다. CONCURRENTLY DDL을 파일당 한 문장으로 분리하는 기존 규약을 따랐다. 2,501행의 중단/재시작과 cutover 재실행을 실제 PG에서 검증했다.
- GC 초기안의 직접 `FOR UPDATE`는 runtime의 SELECT/DELETE 권한으로 실패했다. 운영 권한을 넓히지 않고 SECURITY DEFINER 함수로 수정했으며, 잠금 뒤 새 statement snapshot에서 참조를 재확인한다. 동시 insert의 대기 후 가시성, GC/publish 경쟁, NULL/삭제 거부와 손상 경계는 DB 회귀로 검증한다.
- 첫 payload 후보는 작은 publish→consume benchmark가 기준선보다 느렸다. 불필요한 identity payload JOIN을 제거하고 기존 fence pipeline에 contract 확인을 합쳤다. 발행 본문 문장 수는 그대로이고 네 왕복을 세 왕복으로 줄였다. callback·canonical·intent·PROCESSED 원자성은 유지한다.
- 정상 application을 임의로 생략하지 않았다. 반복 확인도 freshness·멱등·CANONICALIZED 의미가 있어 entity별 기록을 유지하고 N번 INSERT를 하나로 묶었다. 저장량 축소는 3일 감사 유예와 orphan 제외 UNIQUE를 포함한다. 별도 receipt/detail 구조는 아래 조회 지연 때문에 채택하지 않았다.
- 영구 codec/cache/fallback/dual writer·재수집 경로와 새 런타임 의존성을 추가하지 않았다. 테스트에서 소스/문구/정확한 왕복 수를 고정하던 검사·자체 parser는 제거하고 실제 DB 상태·권한·동시성 검증을 유지했다.

### 동일 입력 비교

kapu, Go 1.27.1, PostgreSQL 18.6, `GOMAXPROCS=2`. 기준선 `8b847e683`의 별도 detached worktree와 후보를 교대로 실행했다. 아래는 로컬 합성 입력이며 운영 26 GiB 전체 또는 일일 유입 절감률로 외삽하지 않는다.

| 경로 | 기준선 | 후보 | 해석 |
| --- | --- | --- | --- |
| 작은 community publish→consume, 100회×3 | 7.35–8.54 ms/op | 7.01–7.37 ms/op | 평균 지연 개선; allocation은 약 281 KB/2320 → 294 KB/2376으로 증가 |
| 8 entity 신규, 100회×3 | 9.69–9.96 ms/op; p99 19.05–20.79 ms | 9.16–9.55 ms/op; p99 18.54–19.62 ms | batch INSERT 이득, 15% 목표는 미달 |
| 8 entity 반복, 100회×3 | 7.52–8.53 ms/op; p99 11.75–12.87 ms | 7.16–7.49 ms/op; p99 12.17–12.28 ms | 평균 개선, tail 범위 겹침 |
| 긴 LIVE 이력, 30회×3 | sparse 227–233 ms/op, all 228–233 ms/op | sparse 229–245 ms/op, all 230–241 ms/op | 쿼리는 변경하지 않았으며 개선 입증 없음 |

16 KiB 수준의 8-post payload, 동일 신규 정책, 각 200 관측의 실제 publish→consume을 두 번씩 교대했다. source·queue·application·canonical 등 public relation의 증가량과 WAL을 함께 측정했다. setup 이후 lease 갱신도 두 표현에 동일하게 포함했다.

| payload 반복률 | relation bytes/관측: 전 → 후 | WAL bytes/관측: 전 → 후 | 의미 |
| --- | ---: | ---: | --- |
| 0% | 82,452 → 81,183 | 89,233 → 88,390 | 거의 절감되지 않음 |
| 50% | 53,535 → 43,745 | 58,932–58,933 → 49,140–49,142 | relation 약 18.3%, WAL 약 16.6% 감소 |
| 90% | 31,048 → 14,172 | 35,354 → 18,332–18,333 | relation 약 54.4%, WAL 약 48.1% 감소 |

평균 처리 시간은 후보가 작았으나 90% 반복의 후보 p99는 31.42 ms 이상치와 다음 회차 15.18 ms가 함께 나왔다(기준선 16.30–17.77 ms). 모든 부하의 tail 무퇴보·CPU/관측 15% 개선을 입증했다고 주장하지 않는다. 운영 활성화 전 대표 fleet 부하/2배 입력과 장기 관측이 남는다. 전체 안정 상태 40%·신규 적재/일 30%·주요 경로 두 개 15% 개선은 **미입증**이다. TTL 정착이나 물리 파일 회수 효과를 위 숫자에 더하지 않는다.

### 후보를 채택하지 않은 근거

- 같은 400개 합성 문서의 relation 크기: pglz 11,706,368 B, LZ4 10,051,584 B, gzip 5,906,432 B, Zstd 5,636,096 B. 실제 PG 읽기+canonical hash 비교 720회/codec에서 hash는 전부 일치했다. p95/p99(ns)는 pglz 656,590/753,574, LZ4 587,388/660,307, gzip 673,853/791,216, Zstd 559,314/627,174였다.
- gzip은 조회 tail이 나빠 제외했다. Zstd는 합성 codec probe에서 유망하지만 별도 bytea reader·무결성/해제 상한·replay 구현 전환을 포함한 검증은 하지 않았다. 현행 JSONB SQL 비교와 계약을 그대로 유지하는 LZ4를 선택했고 Zstd production codec이나 archive는 추가하지 않았다.
- lossless receipt/detail 시제품은 fan-out 1에서 1,466,368→1,482,752 B로 오히려 커졌고, 8에서는 11,386,880→6,127,616 B였다. 단건 감사 조회 p99는 각각 153,432→190,824 ns, 166,587→185,413 ns로 느려졌다. 저장량만으로 정당화하지 않고 미채택했다. 실제 상태 전이와 반복 확인을 구분하지 않은 상세 감사 생략도 하지 않았다.

### 실제 경로와 운영 경계

- disposable PG에 최종 실제 `db-migrate` 103개 manifest 적용 뒤 Go 공개 repository/consumer로 publish→consume→canonical→intent→replay를 실행했다. 관측 3개, payload 2개, canonical 2개, intent 1개, PROCESSED 3개였다. 실제 외부 발송은 0건이다.
- 같은 smoke에서 retention→payload GC 뒤 원본/payload는 0개, canonical 2개와 intent 1개는 보존됐다. 운영 데이터는 이 smoke에 쓰지 않았다.
- authoritative DB read-only guard `on`: 10:19 UTC DB 약 26 GB, MILESTONE outbox/event/collision와 통계 PROCESSING 모두 0. 10:51 UTC 정확한 count는 stats snapshots 80,645행, history 32,978행, changes 14,440행, milestones 14행·approaching 10행이고 stats source/evidence/heads는 0행이다. 표준 제거문과 다른 구독자 명령 안내를 가진 CMD_HELP도 0건이다. 이 데이터는 **운영에 아직 남아 있다**. Docker root(`/var/lib/docker`)가 놓인 root filesystem 여유는 55,217,979,392 bytes였으며 backfill 중 최대 여유 보장은 아니다.
- WAL 864 MB/54파일, archive_mode=off, slot/sender 0, wal_keep_size=0, min/max WAL 80/1024 MB, checkpoint 900초. 불필요한 archive/slot 보존의 증거가 없어 WAL 설정이나 active 파일을 변경하지 않았다.
- backup timer disabled/inactive. 삭제 후보는 `~/.local/share/hololive-db-backup/daily/20260927T150006Z-sql-w4-held-dsz7cbp9/`의 암호화 dump 1,413 B+manifest 372 B+checksum 118 B와 `/home/kapu/work/w4-publication-20260928/private-drop-backups/hololive-ledger-state-before-drop.dump` 4,733 B, 총 논리 6,636 B다. 둘 다 이미 제거한 delivery ledger state의 부분 복구본이며 최신 전체 DB 백업이 아니다. 삭제하면 해당 과거 객체의 복구 수단이 줄어든다. 승인 없이 삭제하지 않았다.
- `private-drop-backups/`의 ChatBotGo 자료와 `iris-ops-backups/*/db`의 Iris DB/SQLite WAL은 대상에서 제외했다. 후자는 읽기 전용으로 유지했다. 배포 tree/image 보관본도 DB dump로 분류하거나 삭제하지 않았다.
- [API runbook](../runbooks/hololive-api.md#youtube-관측-저장-구조-전환)에 정지 대상·234–242 순서·전체 backfill downtime·복구/물리 회수 경계를 작성했다. 이 manifest의 backfill은 writer 정지 상태에서 실행한다. 운영 migration/배포/TTL 적용·복구본 생성/삭제·파일 재작성은 실행하지 않았다.

### 최종 로컬 검증과 남은 승인 경계

- `GOMAXPROCS=2 RACE_TEST_PARALLEL=2 bash scripts/ci/local-ci.sh`가 최종 `Passed`로 종료했다. architecture/SQL ownership, canonical vet·integration-tag vet, staticcheck, golangci-lint(0 issues), NilAway, Go/production collector build, 기본 Go/DB 테스트와 race, X Spaces helper, AP rsync manifest, PostgreSQL capacity와 YouTube plane performance budget을 통과했다.
- 마무리 검증에서 기존 integration provisioner로 별도의 일회성 PostgreSQL/Valkey를 만들고 `INTEGRATION_TEST=true`로 dispatchoutbox·batchrepo의 integration-tag 테스트와 youtubedispatch·joblease의 integration 그룹을 실행했다. 네 패키지 모두 통과했고 임시 서비스는 제거했다. 외부 CLIProxy/LLM 호출을 하는 두 summarizer 그룹은 이번 저장 구조 변경 범위 밖이므로 실행하지 않았다. 별도의 실제 migration→publish→consume→canonical/intent→replay→GC smoke와 함께 로컬 검증 근거로 삼는다.
- 앞서 실행한 stack DB access/retry/projection/worker 계약 검사도 통과했다. 로컬 smoke DB/container·임시 Go probe와 성능 기준선 worktree는 제거했다.
- 승인된 로컬 구현·검증과 운영 전환 범위 문서화는 완료했다. 운영 migration/배포·TTL 활성화·복구본 생성/삭제·물리 재작성은 미실행이다. 백업 후보 삭제는 정확한 대상과 복구 손실에 대한 승인을 기다리며, 40%/30% 용량 목표·두 주요 경로 15% 개선·대표 부하 tail 무퇴보는 미입증 상태로 남긴다.

### 마무리 동시 부하 검증

- 동일 기준선과 후보를 `baseline→candidate→candidate→baseline→baseline→candidate` 순서로 비교했다. 합성 community payload는 8개 post·본문 약 16 KiB이며, 4개 collector 소유자에 동시 작업 4개/8개를 배치했다. 이는 동시성과 총 입력량을 2배로 늘린 로컬 closed-loop 실험이며 운영 fleet의 고정 도착률 2배나 cold-cache 검증은 아니다.
- 작업자당 100개 관측, 반복률 0/50/90%, 표현별 3회: 총 21,600개 관측에서 terminal queue, canonical 본문 digest·행 수, application·알림 intent 수가 대응 조건별로 일치했다. 외부 발송은 하지 않았다. 아래는 각 실행의 percentile을 다시 중앙값으로 요약한 ms이며 합친 표본의 percentile이 아니다.

| 동시 작업 / 반복률 | p95 기준선 → 후보 | p99 기준선 → 후보 |
| --- | ---: | ---: |
| 4 / 0% | 19.737 → 20.256 | 30.076 → 33.536 |
| 4 / 50% | 18.745 → 20.824 | 35.293 → 38.466 |
| 4 / 90% | 17.153 → 17.472 | 35.879 → 41.587 |
| 8 / 0% | 32.938 → 30.191 | 56.507 → 46.153 |
| 8 / 50% | 26.066 → 25.316 | 40.964 → 41.732 |
| 8 / 90% | 24.376 → 23.543 | 41.718 → 45.309 |

- 후보 4/90%의 한 회차 p99 205.250 ms도 관측했다. 작은 표본의 일부 tail은 나빠졌으므로 이 결과를 무퇴보 통과로 판정하지 않는다.
- 작업자당 500개로 확대한 첫 비동기 probe는 **기준선부터** 모든 과거 post가 canonical에 남는다는 기대값에 실패했다(4/0% 15,992 대 16,000, 8/0% 31,992 대 32,000). 기존 `reconcileCommunity`는 최신 head보다 늦게 처리된 관측을 `STALE_SKIPPED`로 끝내므로 그 기대값은 일반 비동기 실행의 계약이 아니다. 제품 코드를 바꾸거나 실패 결과를 성능 성공으로 사용하지 않았다. 동일 처리 순서를 전제로 비교하는 확대 실험은 슬롯별 동시 작업을 drain한 뒤 다음 슬롯을 넣도록 별도로 구분했다.

#### 확대된 동일 순서 비교 결과

작업자당 500개, 동시 작업 4/8개, 반복률 0/50/90%, 표현별 3회를 완료했다. 총 **108,000개 관측**을 발행·처리했으며 각 조건의 최종 canonical 본문 digest·행 수, application·알림 intent 수가 기준선과 후보에서 일치했다. 모든 관측은 PROCESSED였고 비terminal queue는 0이었다. 초기 비동기 측정과 달리 슬롯 사이 drain을 포함한 결과이므로 둘을 같은 부하로 합산하지 않는다.

| 동시 작업 / 반복률 | 기준선 p99 ms: 3회 | 후보 p99 ms: 3회 |
| --- | --- | --- |
| 4 / 0% | 39.484 / 46.102 / 108.061 | 25.235 / 28.580 / 103.102 |
| 4 / 50% | 20.420 / 24.295 / 105.575 | 21.059 / 19.814 / 80.569 |
| 4 / 90% | 18.074 / 22.750 / 96.494 | 28.241 / 42.742 / 72.418 |
| 8 / 0% | 37.111 / 182.515 / 134.436 | 72.781 / 39.091 / 108.079 |
| 8 / 50% | 30.653 / 195.602 / 133.640 | 60.995 / 70.129 / 63.477 |
| 8 / 90% | 26.385 / 126.871 / 121.620 | 56.803 / 80.068 / 31.057 |

- 각 4/8 동시 조건의 relation 증가량은 반복률 50%에서 관측당 약 53 KiB가 아니라 **53,067–53,293 bytes → 43,495–43,708 bytes**, 90%에서 **29,655–29,767 bytes → 12,797–13,049 bytes**였다. 이는 이 합성 입력의 할당 증가량이며 전체 운영 DB 안정 상태나 하루 적재량이 아니다.
- 처리량도 기준선 4/0%에서 164.93→148.88→55.78 ops/s로 크게 변했다. 확대 실험 직전 호스트 load average는 28.78/32.85/27.59였지만, 부하 변동의 원인을 특정했다고 주장하지 않는다. CPU는 Go 클라이언트 프로세스만 측정했으며 PostgreSQL CPU나 cold-cache 비용을 포함하지 않았다.
- **판정:** 기능·DB 통합·동시 처리 결과 보존은 확인했다. 반면 표본을 늘려도 실행 간 변동이 커 tail 무퇴보나 주요 두 경로 15% 개선을 확정할 수 없다. 좋은 회차만 골라 성능 통과로 처리하지 않으며 운영 성능 수용은 보류한다. 안정된 측정 환경의 대표 부하와 운영 정착 관측 없이 목표 달성을 선언하지 않는다.
- 마무리 probe 두 파일과 전용 baseline worktree를 제거했다. 영구 성능 gate·새 production 코드·fallback은 추가하지 않았고 앞선 통과한 제품 코드의 전체 local CI 입력은 변경하지 않았다. 운영 서비스·DB·TTL·백업·WAL 및 Git 게시 상태는 변경하지 않았다.

### 운영 전환 요청 후 사전 확인

- 사용자의 후속 진행 요청에 따라 운영 전환 준비를 시작했다. 배포 provenance를 위해 검증한 변경의 로컬 commit을 준비하며 Git 원격 게시 요청으로 해석하지 않는다. 새 복구본 생성·기존 백업 삭제와 성능 미확정 위험의 수용 범위는 실제 파괴적 변경 전에 분리하여 확정한다.
- 중앙 API/worker와 collector b/c는 healthy이며 a/d native unit은 active/running, NRestarts=0이다. b 및 a/d release는 `3b5e3dd15255ef772ee60c4c9dee9c2efc1293f3` 계열이다. 중앙은 aarch64, a/d는 x86_64, b는 aarch64다.
- authoritative DB guard `on`: PG 18.6, DB 27,874,940,607 bytes, 적용 manifest 94개·마지막 233. 통계 source 0, queue PENDING 21/PROCESSING 4/PROCESSED 1,202,603, metadata lease IDLE 120. 이 수치는 live drain 완료가 아니며 정지 직전 재확인이 필요하다. 중앙 filesystem 여유 55,183,527,936 bytes, WAL 864 MiB/54파일, replication slot/sender 0이다.
- 자동 backup timer는 disabled/inactive다. 기존 backup 실행기는 Google Drive 전송과 7세대 삭제를 포함하므로 일회성 로컬 복구본에 그대로 실행하지 않는다. 복구본을 만들 경우 기존 암호화 수단을 재사용하되 별도 경로·외부 전송 없음·자동 백업 재활성화 없음·기존 사본 삭제 없음으로 범위를 제한한다.

### 승인된 복구점과 운영에서 발견한 이행 결함

- 사용자는 **일회성 백업 후 전환**(중단·통계 삭제·새 TTL·성능 미확정 위험 포함)과 **기존 부분 백업 보존**을 선택했다. 자동 백업·Drive 전송·기존 사본 삭제는 하지 않는다.
- 최초 배포 후보는 로컬 commit `873a4c8fc6d19e20d66b94fc46be237a599a5ab6`이다. 중앙/Seoul ARM64 이미지와 a/d AMD64 native 묶음을 검증·전송했지만 아직 활성화하지 않았다. 이전 `3b5e3dd15255ef772ee60c4c9dee9c2efc1293f3` 이미지 태그와 두 Compose 호스트의 `/opt/hololive-bot/compose/deploy-backups/storage-pre-873a4c8fc/` 실행 트리를 보존했다. Seoul classic image store의 config digest와 중앙 containerd store의 manifest digest 차이는 기존 archive 검증기로 확인했다.
- 12:39:08 UTC부터 collector를 정상 정지했고 API/worker도 exit 0으로 정지했다. metadata ACTIVE와 source PROCESSING은 0, MILESTONE outbox/event/collision은 0이었다. 백업·복원 검증 시간도 중단 창에 포함한다고 사전에 알렸다.
- 복구점: `~/.local/share/hololive-db-backup/manual/20260929T124126Z-storage-873a4c8fc/`. 암호화 DB archive는 2,222,356,673 bytes다. 비밀번호를 제외한 role archive도 암호화했고, network-none PG 18.6에서 실제 복원을 완료했다. ledger 94개, 관측 4,861,270행, application의 비NULL observation_id 10,171,985개, queue 1,202,276행, invalid index 0을 확인했다. 검증용 DB/container는 제거했다.
- 승인된 TTL 18개와 폐기된 통계 TTL 키 제거를 중앙 master에 반영하고 Hololive 범위 dry-run 후 sync했다. mirror-only 파일 0, master/mirror 일치와 manifest owner/mode 27개를 검증했다. 새 API 설정과 collector 네 대의 worker profile도 실행 검증을 통과했다. 서비스는 아직 정지 상태다.
- 최초 234 실행은 contract 등록행 삭제에서 statement timeout으로 중단됐다. ledger는 94개/233까지이며 선행 배치 일부는 commit됐으므로 구 이미지 단독 재개는 안전하지 않다. PostgreSQL context는 `remove_channel_statistics_v234()`의 등록행 삭제문이고, application kind 조회 계획은 Parallel Seq Scan이었다. 일회성 전체 count도 별도로 10초 제한에 걸려, 이후 정지 확인에는 ledger/metadata lease와 client session 조회를 사용했다(남은 client는 idle exporter 하나).
- 원인 보완: 새 244에서 `(observation_kind, provider)` 이행용 인덱스를 동시 생성하고 **manifest에서 234보다 먼저** 실행한다. 새 243은 최종 cutover 뒤 이를 동시 삭제한다. 234 본문과 이미 적용된 migration은 바꾸지 않았고 timeout·FK 검사·DDL 게이트도 완화하지 않았다. 정상 적재에 추가 인덱스를 남기지 않는다.
- 회귀는 보존할 다른 kind의 application 15,000행이 있는 실제 PG에서 contract FK 조건의 행 방문 상한을 검사한다. 수정 전 15,000행으로 실패했고 수정 후 128행 이하 기준을 통과했다. 최종 concurrent 구성의 DB/migrationrunner race, manifest/SQL ownership 검사와 실제 `db-migrate` **105개 적용**을 통과했으며 마지막 이행용 인덱스 부재도 확인했다. 정본·소비자·projection의 앞선 race 검증도 통과했다.
- 수정 revision `64d8a7be6`의 244/234 적용 뒤 235 부분 UNIQUE 생성이 문장당 4분 제한으로 두 번 중단됐다. 두 번째 시도는 해당 migration 세션만 `maintenance_work_mem=512MB`로 조정했으며 두 번 모두 invalid index는 러너가 정리했다. DB 전역·컨테이너 제한은 바꾸지 않았고 writer는 계속 정지했다.
- 사용자는 **이번 점검 창만 문장당 10분 허용**을 선택했다. CLI의 명시적 `--statement-timeout=10m`만 사용하며 기본 4분·전체 15분·lock·디스크·무결성 기준은 유지한다. runner/CLI race와 scoped lint를 통과했고, network-none PG 18.6에서 실제 CLI로 음수/11분 거절, 10분 옵션의 105개 migration 적용, 기본값의 105개 skip을 확인했다. 새 회귀는 짧은 문장 timeout 뒤 미적용 원장을 보존하고 충분한 한도로 재개하는 동작을 검증한다.
- 점검용 migrator는 로컬 commit `17278a8a1e1dab44cd539a7bd201bffbf91a4383`, ARM64 image `sha256:027f0bd0bb125f0b975383a3e73dc993f72933e1472343e4e31037938b9ab6a3`로 빌드·전송했다. 일회성 Compose image override로만 사용하며 fleet의 검증된 `64d8a7be6` 이미지 태그는 바꾸지 않았다. kapu에 ARM64 실행 에뮬레이터가 없어 해당 이미지의 `--help` 실행은 `exec format error`였고, ARM64 ELF/static·image label 검증과 native AMD64 CLI smoke를 구분했다.
- 10분 옵션 실행은 831초 동안 235–240을 적용하고 241의 `backfill incomplete` 검사에서 종료했다. timeout이 아니며 241 transaction은 rollback됐다. 세 인덱스의 valid/ready를 확인했고 DB는 27,126,789,823 bytes, 중앙 filesystem 여유는 54,589,964,288 bytes였다. writer 정지를 유지한 채 기존 1000행 독립 commit·30초 문장 제한·디스크 중단 기준의 backfill을 시작했다.
- 기존 부분 백업은 사용자 선택대로 보존했다. 일회성 전체 복구본도 그대로 있으며 자동 backup timer는 여전히 disabled/inactive다. Drive 전송·기존 사본 삭제·WAL 수동 삭제는 하지 않았다.
- 첫 backfill은 SQL 실패 없이 작업 단위의 3000초 wall-time 상한에서 종료했다. 마지막 진행 로그는 290만 행 commit이며 그 뒤에도 다음 시간 검사 전까지 일부 배치가 commit됐을 수 있으므로 이를 정확한 총 변환 수로 쓰지 않는다. 잔여 NULL 존재, backfill client 종료, 여유 53,236,621,312 bytes와 DB 28,302,104,255 bytes를 확인했다. 같은 1000행/30초/50분 제한과 최초 filesystem 기준을 유지하여 미변환 행만 재개했다. 작업 단위 재개는 문장 timeout 상한 증액이나 실패 배치의 불명확한 재실행이 아니다.
- 두 번째 backfill은 2115초에 1,937,270행/1938배치를 처리하고 0 반환과 잔여 NULL 부재를 확인했다. 이후 독립 read-only 세션에서도 잔여 NULL 0·invalid index 0을 확인했다. 마지막 migrator는 266초에 241–243을 완료하여 ledger **105개**가 모두 적용됐다. `payload_id NOT NULL`, validated FK, 구 payload/hash 열·backfill 함수·두 임시 index 부재를 확인했다.

### 운영 전환 완료와 즉시 검증

- 실행 revision은 API·worker·collector/issuer fleet 모두 `64d8a7be6b1a0e309a870bfbacdfe64f508a4681`이다. 점검용 migrator만 위 `17278a8a1`을 사용했다. 원격 build·Git push는 하지 않았다. 중앙 API는 16:26:54 UTC 재개·healthy, 새 CURRENT projection은 16:26:55 UTC의 generation 5019/560 targets였으며 통계 target은 없었다. worker/c는 16:28 UTC, a/d는 16:28:31/33 UTC, b는 16:30:01 UTC 재개했다. 12:39 UTC 정지부터 전체 fleet 복구까지 약 3시간 51분이며 짧은 중단으로 기록하지 않는다.
- Seoul 첫 호출은 중앙 deploy path를 사용해 파일 부재로 실행되지 않았고 다음 호출은 중앙 env 이름 때문에 거절됐다. 기존 컨테이너 Compose label과 AP 배포 스크립트에서 `/home/ubuntu/hololive-bot`, `/etc/stack-secrets/hololive-bot/ap-compose.env`, `COMPOSE_PROFILES=oracle`을 확인한 뒤 issuer→collector 순서로 `--no-build --no-deps` 재개했다. a/d는 이전 release/rollback-contract를 보존하고 실패 시 구 schema 이미지로 자동 복원하지 않는 활성화 경로를 사용했다.
- 최종 중앙 API/worker/c/issuer, Seoul b/issuer는 모두 healthy, restart 0, OOM false였다. a/d는 active/running, NRestarts 0이며 각 `/ready`에서 helper ok·first_success true·handoff PROCESSED를 확인했다. b/c도 같은 실제 readiness를 통과했다. collector의 `queue_full`/일부 `discovery_truncated`는 관측되었으므로 내부 작업 포화가 전혀 없다고 주장하지 않는다.
- 네 collector의 재개 후 신규 관측이 DB queue에서 PROCESSED였다. 실제 application에는 community 정본화, video/shorts APPLIED, live APPLIED/ENDED/OLDER_POSITIVE_RETAINED, schedule APPLIED/SCHEDULE_MERGED, photo RAW_RETAINED가 확인됐다. profile target은 유지됐지만 즉시 검증 표본에 새 profile 관측은 없으므로 신규 profile fetch까지 실행됐다고 주장하지 않는다. X 세션도 connected·last_error 비어 있음·16:34:07 UTC 성공을 확인했다.
- 운영 API의 TTL 18개와 enabled/policy-approved를 내부 값 비교로 확인했고 폐기한 CHANNEL_STATS 키는 없었다. 마지막 metrics는 consume success 625, pending/processing/oldest-age 0, source/queue/application/checkpoint/live-absence 삭제 각각 5000, payload GC 6이었다. retention 오류 series는 관측되지 않았다. 확인한 신규 Docker stdout·중앙 파일 로그·a/d journal에는 ERROR/FATAL/PANIC 및 SQLSTATE·권한/TLS/파일 부재/OOM 표지가 없었다. PostgreSQL 네트워크 접속 39개가 TLSv1.3이고 평문 TCP는 0이었다.
- 재개 직후 전체 표본 522건의 수신→PROCESSED p95/p99는 21.24/22.34초, 뒤의 최근 5분 표본 436건은 3.72/7.27초였다. 이는 startup/backlog를 포함하는 handoff latency이며 DB query 성능이나 기준선 대비 개선으로 치환하지 않는다. 마지막 DB는 28,559,849,151 bytes, WAL 1 GiB, filesystem 여유 52,976,799,744 bytes였다. backfill/새 index의 할당을 포함하므로 즉시 물리 용량 감소 목표를 달성했다고 주장하지 않는다.
- 복구는 보존한 정지 시점 전체 DB archive·이전 schema/ledger·`3b5e3dd15255` fleet·기존 TTL을 함께 복원하는 경로다. 구 이미지 단독 rollback은 금지한다. 기존 부분 백업과 일회성 전체 복구본·배포 복구 tree/image를 보존했고 자동 백업/Drive 전송/백업 삭제/WAL 수동 삭제는 하지 않았다. Fallback delta: none.
- **남은 시간 의존 검증:** 최소 7일 TTL 정착 관측, 실제 유입/정리/WAL/vacuum/알림 품질, 같은 대표 부하의 p95/p99 및 용량 목표 판정은 아직 완료되지 않았다. 새 timer·감시 daemon은 설치하지 않았다. 현재 완료 범위는 승인된 schema/fleet/TTL 전환과 즉시 기능·상태 검증이며, 장기 성능·용량 수용은 미확정 상태를 유지한다.

### 적대적 리뷰와 게시 준비 — 2026-09-30

- 리뷰 범위는 원격 `main`의 `8b847e683feacdd1bf4ef84f95cd930d665870b1`부터 `f18e3d898`까지의 미게시 4개 commit/169개 파일이다. 통계 계약 제거, migration 234–244의 manifest 순서·부분 commit·재실행, 공유 payload의 digest/참조/동시 발행·GC, application batch/부분 UNIQUE, projection 보존 상한, TTL 및 helper/Go 소비자 cutover를 검토했다. 추가 코드 수정이 필요한 게시 차단 결함은 확인하지 못했다. 이미 운영에 적용한 migration은 수정하지 않았다.
- kapu의 network-none PostgreSQL 18.6에서 실제 `db-migrate --statement-timeout=10m`로 105개를 적용하고 기본 옵션 재실행의 105개 skip을 확인했다. 별도 scraper transaction이 payload KEY SHARE를 유지하는 동안 runtime 권한의 GC가 잠긴 orphan과 참조된 payload를 보존하고, 잠금 해제·cursor wrap 뒤 orphan만 제거하는 시나리오를 실행했다. scraper의 payload UPDATE 권한 거절과 참조 payload DELETE의 FK 거절도 확인했다. 임시 DB/container와 smoke 바이너리는 제거했다.
- stack DB-access, retry, projection 계약 검사를 통과했다. 기존 DB/race 회귀에는 backfill rollback·재개, payload 손상 시 consume/replay 거절, 동시 publisher의 대기 후 가시성, 활성 application 멱등성과 orphan 이력, lease 보호 projection 보존이 포함된다. 최종 게시에는 저장소의 기존 pre-push hook을 그대로 사용하며 게이트 우회·강제 push·새 운영 배포는 하지 않는다.
- 위 즉시 검증과 별개로 7일 정착 관측, 운영 용량 목표와 대표 부하 성능 수용은 계속 미확정이다. 게시 성공을 해당 목표의 달성이나 운영 재배포로 해석하지 않는다.
- 첫 `main` push의 로컬 pre-push는 정상 통과했다: architecture/manifest/SQL ownership, vet·staticcheck·golangci-lint(0 issues)·NilAway, build·Go/DB 테스트·race, helper typecheck와 Node 224개 테스트, workspace 호환성 및 govulncheck의 호출 가능 취약점 0건이다. module-only 취약점 1건은 호출되지 않는 항목으로 보고됐으므로 의존성 전체의 취약점 부재라고 표현하지 않는다. GitHub는 필수 `fast-gate`가 아직 없는 직접 push를 GH013으로 거절했다. 보호 규칙 변경 없이 동일 변경의 검토 브랜치/PR에서 필수 상태 검사를 실행하는 게시 경로를 사용한다.
