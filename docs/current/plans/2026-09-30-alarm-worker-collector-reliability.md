# 알람 워커·YouTube 컬렉터 신뢰성 개선 계획

2026-09-30 코드 리뷰에서 재현한 13개 문제의 조치와 회귀 검증을 완료했습니다. 발행·발송·복구의 책임과 검증 조건을 맞추고 전체 local CI를 통과했습니다. 운영 적용은 수행하지 않았습니다. 이미 다른 작업에서 수정한 항목은 최신 검증으로 확인해 중복 수정하지 않았습니다.

## 목표와 제약

- 저장되지 않은 delivery를 발행 성공으로 표시하지 않고, 한 방의 성공이 다른 방의 미발행을 숨기지 않게 합니다.
- 선정된 알람 후보를 발행 결과가 확정되기 전에 잃지 않게 합니다. 재시도에서도 수신 방·본문·발송 경로·요청 ID의 관계를 보존합니다.
- 확정 성공, 전달 전 실패, 재시도 가능한 오류, 전달 결과 불명을 구분합니다. 확정된 OUTCOME_UNKNOWN·handoff 불명에서는 자동 재발급·재전송·dedup 해제를 하지 않습니다. transport ambiguity를 저장된 동일 ID·동일 request로 재시도하는 alarm dispatch의 현행 허용 조건은 별도로 유지합니다.
- 만료 대기 행의 종료 경로와 컬렉터의 독립 작업 진행을 확보하고, 지표가 실제 처리 결과를 나타내게 합니다.
- [PROJECT_MAP](../PROJECT_MAP.md)의 소유권을 유지합니다. collector는 수집·observation 발행, API는 consume·정본·notification intent, worker는 수신 대상·렌더링·delivery·egress를 소유합니다.
- collector의 lease/fence, projection·contract generation 검증, observation·checkpoint·lease terminal의 원자성을 유지합니다. YouTube logical ledger의 SENT/QUARANTINED 증거와 deterministic owner도 유지합니다.
- 기존 target minutes, 조회 lookback, retry 횟수, worker·provider 동시 실행 상한과 retention 값을 임의로 늘리지 않습니다. 새 런타임·외부 의존성·queue 서비스·toolchain 업그레이드는 범위에 없습니다.
- kapu에서 저장소 루트 기준으로 구현·검증합니다. 기존 변경을 복원하거나 임시 overlay의 전체 테스트 파일을 작업 트리에 덮어쓰지 않습니다.

## 근거와 현재 작업 트리

두 차례 리뷰에서 아래 문제를 격리 PostgreSQL, mock 응답, 제어된 실행 순서 및 가상 시간으로 재현했습니다. 기존 관련 테스트는 통과했으며, 진단 테스트는 각 문제를 드러내는 assertion에서 실패했습니다. 실제 Iris 발송이나 운영 DB의 발생 빈도는 확인하지 않았습니다.

[상세 리뷰 및 검증 기록](../../history/architecture/2026-09-30-alarm-worker-collector-reliability-review.md)에 처리 흐름, 13개 항목의 코드 위치·재현 입력·관찰 결과·한계, 실제 검사와 후속 검토를 기록했습니다. 이 계획은 구현 범위와 완료 결과를, 리뷰 기록은 진단 시점의 근거를 소유합니다.

임시 근거는 `/tmp/hololive-review-20260930-jyt9zxt1/overlay.json`과 `/tmp/hololive-review-deep-20260930-b_anf2zm/overlay.json`입니다. 이 경로는 영속 산출물이 아니므로 필요한 시나리오를 현재 코드에 맞춘 회귀 테스트로 옮깁니다.

현재 [수명 정합성 계획](2026-09-30-live-reconciliation-lifecycle.md)에 해당하는 shared live reducer·sourceobservation, collector helper·joblease query, API projection 및 migration 변경이 진행 중입니다. 해당 변경은 보존하며, NULL title 수정과 후보 조회 변경은 구현 시점의 최신 파일에서 조정합니다. 새 migration 번호는 현재 작업 트리의 번호를 다시 확인해 할당합니다. [9월 11일 구조 개선 계획](2026-09-11-alarm-collector-structural-refactor.md)의 완료 작업이나 제거된 Karing 경로를 다시 도입하지 않습니다.

문서화 시점에 live session 단건·복수 조회 SQL의 `COALESCE(title, '')` 추가를 확인했습니다. 기존 NULL title 진단을 최신 코드에서 다시 실행해 metadata_only·legacy_unknown 두 subtest 통과(exit 0, package 8.938초)를 확인했습니다. 이후 단건·복수·consumer의 실제 NULL 회귀와 전체 local CI도 통과했습니다. 이전 실패와 후속 통과는 리뷰 기록에 구분했습니다. 해당 제품 코드 수정은 이 작업의 구현 결과가 아닙니다.

| 재현한 문제 | 우선순위 | 작업 단계 | 회귀 검증의 핵심 |
|---|---|---|---|
| 일부 방의 발행 성공이 방송 전체 dedup을 막음 | P1 | 1 | 같은 방송의 A 성공·B 실패 뒤 B만 복구 |
| 발행 실패 뒤 다음 평가에서 upcoming 후보가 사라짐 | P1 | 2 | 이미 선정된 후보가 시간 경과 뒤에도 복구 또는 명시적 만료 |
| 범용 Markdown handoff 대기에 attempt deadline이 없음 | P1 | 3 | queued 고정·조회 오류 반복에서도 bounded 종료 |
| nullable live title을 string으로 scan해 consumer 실패 | 당시 P1 | 0·4 | COALESCE 추가 후 NULL 진단 통과; 단건·통합 검증 유지 |
| PENDING이 없으면 범용 cleanup이 실행되지 않음 | P2 | 3 | 빈 batch에서도 due maintenance 실행 |
| 해제 전 DB 결과의 늦은 cache warm이 구독을 복원함 | P1 | 4 | 구독 해제 commit·SREM 뒤 stale SADD 방지 |
| payload 충돌로 미생성된 delivery를 발행 성공으로 표시 | P1 | 1 | 혼합 batch에서 충돌 항목만 거절·claim 해제 |
| 신선도 제한을 지난 YouTube PENDING에 종료 경로가 없음 | P2 | 5 | claim 제외 행도 terminal 집계·retention으로 수렴 |
| 후보 조회 한 런너의 실패가 뒤의 정상 런너를 막음 | P2 | 6 | 반복 오류 중에도 정상 런너 조회·enqueue 진행 |
| 실패한 YouTube 전송을 공통 counter가 성공으로 기록 | P2 | 7 | FAILED/unknown/timeout을 success로 기록하지 않음 |
| 범용 delivery가 OUTCOME_UNKNOWN을 일반 실패로 재시도 | P1 | 3 | 결과 불명을 보존하고 retry·rearm·재발급 차단 |
| CLIENT_REQUEST_ID_FAILED를 복구하지 않고 quarantine | P2 | 5 | 확정된 pre-handoff 실패만 유한 세대 재발급 |
| 동일 clientRequestID 재시도에서 템플릿 변경으로 본문 변경 | P1 | 5 | 템플릿·설정 변경과 재시작 뒤 동일 request 유지 |

## 0. 구현 기준과 회귀 테스트 준비

- [x] 최신 diff와 해당 subtree 지침을 확인하고, 수정할 파일·기존 변경의 교집합을 기록했습니다. 같은 checkout에서 Git ref/index 작업을 병행하지 않습니다.
- [x] 임시 테스트에서 재현에 필요한 부분만 추출합니다. 현재 helper와 fixture를 사용하고 원래 테스트를 보존합니다. 각 변경 묶음에서 먼저 해당 문제가 재현되는지 확인합니다.
- [x] 발행 receipt, 방별 delivery, 외부 발송 outcome, lease terminal의 현재 계약과 소비자를 확인합니다. 내부 반환값 변경의 모든 호출자를 함께 수정할 목록을 만듭니다.
- [x] NULL title은 실제 PostgreSQL fixture로 재확인했습니다. 별도 작업의 COALESCE 코드에서 기존 진단 두 subtest가 통과했습니다. 중복 수정하지 않고 4단계의 단건·통합 회귀 검증을 유지합니다.

완료 조건: 13개 시나리오의 현재 재현 여부와 소유 파일이 명확하며, 이후 검증이 기존 작업의 결과와 섞이지 않습니다.

## 1. 건별 발행 receipt와 방별 dedup을 맞춥니다

대상: shared `pkg/service/alarm/dispatchoutbox/{model,repository_insert,repository_insert_batch}.go`, `pkg/service/alarm/queue/publisher.go`, worker `internal/service/alarm/checker/checking/{notifier,youtube_checker_upcoming.go}`, 직접 연결 dedup 호출자입니다.

- [x] 입력 ordinal 또는 안정적인 delivery key별로 inserted, duplicate-active, duplicate-sent, rejected-collision, rejected-terminal 결과를 반환합니다. 중복 terminal도 SENT와 DLQ/QUARANTINED/CANCELLED를 구분합니다.
- [x] repository의 `processedPublishBatchResult`와 publisher의 chunk 누적을 함께 수정합니다. 정상 commit과 collision audit commit을 구분하며, 입력 전체나 성공 prefix 길이로 건별 결과를 추정하지 않습니다.
- [x] commit되지 않은 chunk에는 성공 receipt를 주지 않습니다. 이전 chunk의 확정 receipt는 뒤 chunk 실패에도 유지합니다. commit 응답 자체가 불명확하면 안정적인 key로 ledger를 다시 확인하며, receipt 확인 전 dedup 완료를 기록하지 않습니다.
- [x] notifier는 확정 수용된 항목에만 발행 마커를 기록합니다. 기존 SENT 중복은 충족된 것으로 처리하고, collision이나 비성공 terminal은 성공으로 바꾸지 않습니다. claim 정리는 그 항목의 결과와 현행 정책을 따릅니다.
- [x] upcoming의 방송 전체 `IsAlreadyNotifiedForSchedule`을 방별 발행 여부를 대신하는 차단 조건으로 사용하지 않습니다. 같은 이벤트의 미발행 방과 새 구독 방을 확인할 수 있게 합니다.
- [x] event key·payload hash·collision 거절의 현행 의미는 유지합니다. 제목 변경을 충돌 없이 수용하는 정책은 아래 설계 검토에서 별도로 다룹니다.

검증: 같은 방송의 두 방에서 부분 commit 후 미발행 방만 복구; inserted·duplicate·collision이 섞인 batch의 마커·claim 결과; chunk 경계 실패; DB commit 뒤 cache 마커 실패와 재실행; 동일 SENT 방의 재발송 0회입니다.

완료 조건: 반환 결과, DB delivery, 방별 발행 마커가 일치합니다. 충돌 건은 `Sent`에 포함되지 않고 다른 정상 건은 유지됩니다.

## 2. 평가 시각과 미발행 후보의 수명을 분리합니다

대상: worker `checking/youtube_checker.go`, `checking/youtube_checker_upcoming.go`, `checking/notifier`, alarm scheduler·tier의 직접 호출부 및 필요한 shared dispatch ledger 경계입니다. 1단계의 건별 receipt에 의존합니다.

- [x] 채널의 조회·tier 갱신 시각과 알람 발행 결과의 확인 시점을 분리합니다. `UpdateChannelState`를 뒤로 옮기는 것만으로 완료하지 않습니다.
- [x] 선정된 후보의 event identity, 선정 category, 예정 시각, payload snapshot, 대상 방과 미해결 결과를 보관합니다. 재평가 때 target crossing을 다시 찾아야만 재발행할 수 있는 구조를 제거합니다.
- [x] 기존 dispatch ledger로 durable staging과 대상별 진행을 표현할 수 있는지 확인합니다. 부족하면 같은 outbox package 내부의 최소 PostgreSQL intent 저장소로 한정합니다. 별도 daemon·queue 서비스는 추가하지 않습니다.
- [x] 후보 staging과 그 후보를 평가 완료로 인정하는 checkpoint를 같은 소유 경계에서 처리합니다. DB commit 전 실패에서는 checkpoint를 확정하지 않고, 살아 있는 실행은 미해결 후보를 보존합니다. 재시작은 durable 후보부터 복구합니다.
- [x] 후보 수, 배치 크기, 조회량, 재시도 간격을 기존 실행 예산 안에서 제한합니다. DB commit 전 정보까지 모든 장애·재시작에서 복구한다고 보장하지 않습니다. 보장 시작점을 durable staging commit으로 명시합니다.
- [x] 이미 선정된 후보의 유효기간을 명시합니다. 제안은 예정 시작 전까지 복구하고, 시작 뒤에는 해당 분 전 알람을 만료시키며 시작 알람으로 변환하지 않는 것입니다. 기존 75초 조회 lookback은 유지하고, 도입 전 발행 재시도 기한과 일정 변경·취소 처리 계약을 함께 확정합니다.
- [x] 더 최신의 일정 변경·구독 해제·취소 사실을 확인하면 이전 미발행 후보를 사유와 함께 종료합니다. 단순 목록 누락을 취소 사실로 해석하지 않습니다. 진행 중인 발송 결과 불명은 후보 만료로 재발송 가능 상태가 되지 않습니다.

검증: 5분 후보 선정 후 publish 오류와 65초 경과; 부분 방 복구; staging 이후 재시작; DB commit 뒤 응답 소실; 오래된 후보 만료; 예정 시각 변경; 시작 이후 분 전 알람 발송 0회입니다. 가상 시간과 실제 PostgreSQL로 저장·복구를 각각 검사합니다.

완료 조건: 선정된 후보는 확정 receipt 또는 명시적 종료까지 추적됩니다. 조회 watermark나 lookback 제한 때문에 조용히 사라지지 않습니다.

## 3. 범용 delivery의 발송 예산과 결과 불명 처리를 수정합니다

대상: shared `pkg/service/delivery/{dispatcher,outbox_repository}.go`와 SQL, worker `internal/egress/iris_sender.go`, `internal/service/dispatchrun`·`internal/egress/youtubedispatch`의 오류 분류 연결부입니다.

- [x] 공통 발송 outcome 분류를 shared 경계에 두고 세 파이프라인이 사용합니다. Iris code와 handoff 결과 불명 표식을 같은 의미로 해석하고 transport ambiguity를 확정된 OUTCOME_UNKNOWN과 구분합니다. 저장소의 상태 전이와 증명된 동일 ID 재시도 정책은 각 파이프라인에 유지합니다.
- [x] 범용 dispatcher에 attempt timeout을 추가합니다. 기본 제안은 기존 YouTube 발송 기본값과 같은 10초이며, 발송·최종 상태 반영 예산이 60초 delivery lease 안에 들어오게 검증합니다. 더 짧은 부모 deadline은 보존합니다.
- [x] sender 호출 전 취소와 호출 이후의 불명을 구분합니다. queued 고정·status poll 오류 반복도 attempt deadline으로 종료합니다. HTTP 한 요청의 timeout을 전체 handoff deadline으로 간주하지 않습니다.
- [x] `OUTCOME_UNKNOWN`, handoff 불명, 요청 후 timeout/cancel은 일반 `MarkFailed`로 보내지 않습니다. worker/status fence로 즉시 QUARANTINED 전이를 시도하고, 저장 실패 시 SENDING을 보존해 기존 stale sweep이 처리하게 합니다.
- [x] 범용 delivery의 결과 불명에서는 자동 retry, FAILED rearm, 새 ID 생성과 dedup 해제를 차단합니다. 확정 성공 뒤 MarkSent 실패도 일반 미발송 실패로 바꾸지 않습니다. alarm dispatch의 기존 transport 재시도는 저장된 ID와 5단계의 고정 request를 요구하며, structured OUTCOME_UNKNOWN·handoff 불명에는 적용하지 않습니다.
- [x] cleanup과 실패 집계는 batch 유무와 분리해 due 시점에 실행합니다. 별도 goroutine을 추가하기보다 기존 tick의 bounded maintenance 경계를 사용합니다. 장애 때문에 실패한 sweep은 성공으로 기록하지 않습니다.

검증: synctest의 queued 고정·반복 poll 오류·부모 종료; unknown HTTP code와 handoff sentinel; 확정 success 뒤 DB 오류; stale fence 불일치; 빈 batch cleanup; 실패 행 enqueue와 quarantine 행 enqueue의 차이입니다.

완료 조건: 한 handoff가 범용 dispatch round를 무기한 막지 않습니다. 결과 불명 행은 재전송 대상으로 돌아가지 않으며 유지보수는 유휴 상태에서도 실행됩니다.

## 4. 구독 캐시와 nullable live 조회를 정리합니다

대상: shared `pkg/service/alarm/{targets,cache_warm_targets}.go`, `pkg/service/notification/alarmservice`의 구독 변경 테스트, `pkg/service/youtube/sourceobservation/live_state.go`와 직접 SQL입니다. 다른 단계와 기술적 의존성은 작지만 기존 수명 정합성 변경과 파일이 겹칩니다.

- [x] 단일 채널 DB fallback에서도 checker batch 경로처럼 positive subscriber set을 read-through로 채우지 않습니다. 구독 변경·명시적 rebuild가 cache set을 관리하는 현행 경계를 사용합니다.
- [x] negative empty marker도 구독 추가와 경합하는지 확인합니다. revision 없는 read-through의 empty marker 기록은 구독이 생긴 뒤 늦게 적용돼 수신을 막을 수 있으므로, 이 경로의 기록을 제거하거나 검증된 조건부 기록으로 제한합니다.
- [x] cache eviction·cache 오류의 DB fallback, singleflight 취소, 구독 추가·삭제와 explicit rebuild의 동작을 보존합니다. TTL 없는 stale positive set이 다시 생성되지 않아야 합니다.
- [x] 실제 NULL title을 nullable scan 경계에서 처리합니다. SQL/domain에서 NULL과 빈 문자열의 의미를 확인하고 해당 경계 하나에서만 정규화합니다. 오류 classifier를 완화하거나 테스트 fixture를 빈 제목으로 바꾸어 결함을 숨기지 않습니다.
- [x] 새 lifecycle_origin·generation 처리와 함께 검증합니다. 기존 상태·출처·clock·notification intent는 제목 정규화로 변경하지 않습니다.

검증: 해제 commit·SREM 뒤 늦은 warm; empty 조회 뒤 구독 추가; eviction 뒤 연속 조회; 실제 NULL/빈 제목/정상 제목의 live consumer 경로입니다. 동시성 검사는 race와 제어된 실행 순서 검증을 함께 사용합니다.

완료 조건: cache read-through가 구독 mutation을 덮어쓰지 않습니다. nullable title이 consumer scan 실패와 불필요한 deadletter를 만들지 않습니다.

## 5. 최종 request 고정, 확정 실패 복구, 만료 종료를 구현합니다

대상: shared dispatchoutbox의 send unit·SQL, worker dispatchrun, YouTube `lifecycle`·`store`, generic delivery의 저장 모델, API migration·dbtest schema입니다. 3단계 outcome 분류를 먼저 사용합니다.

### 최종 request와 ID generation

- [x] alarm send unit에 최종 본문·발송 경로·본문 hash와 원본 ID·현재 generation을 저장합니다. 첫 BeginSending 전에 membership과 request를 CAS로 고정하고, 재시도는 저장된 request를 사용합니다.
- [x] 템플릿·설정·멤버 표시 변경이나 재시작에도 본문을 다시 만들지 않습니다. 같은 send unit의 일부 membership만 발송하지 않습니다. YouTube와 generic의 ID/request 대응도 확인하고 필요한 저장 경계에 같은 불변 조건을 적용합니다.
- [x] 기존 미고정 행의 전환은 상태별로 구분합니다. 한 번도 외부 발송을 시작하지 않은 행만 현재 request를 고정할 수 있습니다. 이전 전송 가능성이 있는 retry/SENDING/unknown 행의 과거 본문을 현재 템플릿으로 추정하지 않습니다. 해당 행의 cutover 분류는 운영 inventory가 필요한 항목으로 남깁니다.
- [x] [로컬 Iris SDK](../../../../iris-client-go/README.md)의 `IsPreHandoffClientRequestIDConflict`, `ReissuedClientRequestID`, `ReplyReissueMaxGenerations=2`를 재사용합니다. base ID에서 결정적으로 generation ID를 만들고, generation 증가를 fence로 저장한 뒤 전송합니다.
- [x] `CLIENT_REQUEST_ID_FAILED`만 새 generation을 허용합니다. unknown·payload mismatch·already exists·code 없는 409·transport 오류에는 ID를 재발급하지 않습니다. 동일 generation은 동일 본문과 경로를 사용하며 기존 retry/time horizon을 우회하지 않습니다.
- [x] generation 소진 시 현재 terminal 정책으로 종료하고 사유를 남깁니다. 무제한 revive가 ID generation과 발송 횟수 상한을 초기화하지 않게 합니다. 다른 종류의 현행 FAILED group revive를 일괄 제거하지 않습니다.

검증: timeout 뒤 DB 템플릿 변경; 설정 변경·재시작; payload mismatch에서 재발급 0회; FAILED에서 r1/r2만 사용; generation 저장 후 crash replay; concurrent CAS; 부수효과 시작 가능성이 있는 legacy request 분류입니다.

### 오래된 PENDING의 종료

- [x] 부모 created_at 신선도 기준을 유지하며, claim 제외된 known-unsent PENDING을 bounded batch로 찾습니다. row_version과 logical owner를 검증한 뒤 기존 FAILED terminal과 명시적 만료 사유로 수렴시키는 방식을 우선합니다.
- [x] SENT/QUARANTINED ledger와 SENDING sibling이 있으면 기존 논리 상태 판정을 먼저 적용합니다. 오래됐다는 이유로 unknown evidence를 삭제하거나 일반 미발송 만료로 바꾸지 않습니다. 만료를 SENT/QUARANTINED ledger로 날조하지 않습니다.
- [x] 부모 aggregate와 terminal_at을 반영해 기존 terminal retention·cleanup으로 연결합니다. 만료 사유는 revive 대상에서 제외하고 followers는 owner 결과를 따릅니다.
- [x] ready snapshot은 실제 claim predicate와 맞추고, 만료 대기 행은 별도 bounded 집계로 보여 줍니다. cleanup을 위해 active child를 바로 삭제하지 않습니다.

검증: 10일 된 pending parent/children; logical sibling·ledger·동시 claim 경합; expiry sweep 반복의 멱등성; aggregate 실패 뒤 복구; retention 뒤 full-row cleanup과 ledger 유지입니다.

완료 조건: 동일 ID의 request가 고정되며, 확정 pre-handoff 실패만 유한 재발급으로 복구됩니다. 오래된 PENDING은 자동 전송 없이 terminal 정리로 수렴합니다.

## 6. 컬렉터 discovery의 오류 범위와 cursor 진행을 맞춥니다

대상: collector `internal/runtime/collectorruntime/scheduler_discovery*.go`, `internal/runtime/joblease/repository_candidates.go`와 테스트, 필요하면 API target projection validation입니다.

- [x] 부모 취소, stale projection, DB 전체 장애, 런너/target 계약 오류를 분류합니다. 전역 오류는 cycle을 중단하고, 국소 오류는 해당 작업을 실패로 남기며 독립 런너를 계속 조회합니다.
- [x] 오류가 난 런너에서 cursor가 영구 고정되지 않게 합니다. 실제 조회 지점과 잔여 capacity를 기준으로 다음 시작점을 계산하고, cycle 전체 성공 여부와 cursor 이동을 분리합니다.
- [x] mixed poll interval 등 잘못된 target bundle은 작업을 실행하지 않습니다. 기존 INTERNAL 오류를 조용히 무시하지 않고 해당 runner/target 오류와 진단을 보존합니다. projection 작성 경계에서도 bundle 조건을 검증할 수 있는지 확인합니다.
- [x] enqueue dedup·queue capacity·projection generation snapshot·lease 획득 및 provider 실행 예산을 유지합니다. 조회 실패가 backlog를 무제한 생성하지 않게 합니다.
- [x] 정상 런너가 진행하더라도 cycle의 실패를 성공으로 숨기지 않습니다. runner별 오류·마지막 discovery 진행을 낮은 cardinality로 관측합니다.

검증: 첫/중간/마지막 런너의 반복 오류, mixed bundle, capacity 1·queue full, 중복 후보, stale projection, 부모 취소, 전역 DB 오류와 정상 런너의 계속 진행입니다. 이후 lease 갱신·fence loss·atomic publish 기존 검사를 유지합니다.

완료 조건: 국소 후보 오류가 독립 작업의 기아 상태를 만들지 않습니다. 전역 불변 조건과 각 오류 원인은 보존됩니다.

## 7. attempt 지표와 전체 연결을 검증합니다

대상: worker YouTube dispatcher·ClaimManager·SendEngine, 공통 workercontract 연결, ready snapshot과 직접 서비스 문서입니다.

- [x] `processed > 0`을 success로 기록하는 연결을 제거합니다. claimed/prepared/fulfilled-without-send와 실제 provider attempt를 구분합니다.
- [x] 공통 attempt tracker의 begin/end·counter를 실제 provider operation에 연결합니다. grouped operation은 provider 호출 1회와 room delivery 결과를 각각 기록해 횟수를 혼동하지 않습니다.
- [x] success, failed, timeout, canceled, panic, outcome_unknown을 실제 결과대로 기록합니다. provider 성공과 DB finalization 실패도 구분합니다. BeginAttempt 없이 terminal counter만 증가하거나 시작한 attempt가 종료되지 않는 경로를 확인합니다.
- [x] 범용·alarm·YouTube 지표가 같은 단어를 같은 의미로 쓰게 합니다. 기존 전용 delivery 지표를 유지하며 ID·방·원문 오류를 새 metric label로 넣지 않습니다.
- [x] 발행 거절→후보 복구→방별 claim→request 고정→provider 결과→DB terminal→집계·cleanup의 연결 테스트를 수행했습니다. 신규 subscriber와 logical sibling 처리도 포함합니다.
- [x] 최종 동작과 실제 검증 결과를 alarm-worker, youtube-collector 및 해당 현재 계약 문서에 반영합니다. 이 계획은 완료한 작업과 남은 한계를 근거와 함께 갱신합니다.

완료 조건: 실패·불명 발송이 공통 success로 표시되지 않습니다. 13개 회귀가 해결되거나, 최신 코드에서 이미 해결된 근거가 확인됩니다. 구현된 변경의 실패 경로와 운영 전환 한계가 설명됩니다.

## 구현 순서와 변경 묶음

권장 순서는 `0 → 1 → 2 → 3 → 4 → 5 → 6 → 7`입니다. 4·6단계는 기술적으로 독립적이지만 현재 같은 checkout의 수정 파일이 겹치므로 파일 소유권을 확인한 뒤 수행합니다. 이 계획은 서브에이전트나 병렬 Git 작업을 승인하지 않습니다.

리뷰 가능한 변경 묶음은 ① receipt·방별 dedup, ② 후보 보존, ③ generic timeout·unknown·maintenance, ④ subscriber·NULL scan, ⑤ immutable request·유한 reissue, ⑥ expired delivery terminal, ⑦ discovery·attempt metrics와 통합 검증으로 나눕니다. migration과 그것을 사용하는 코드·회귀 검증은 같은 묶음에서 준비합니다. 커밋·PR·원격 게시 여부는 실제 후속 지시에 따릅니다.

## 검증 방법

각 변경 묶음에서 재현 회귀와 직접 영향 패키지를 먼저 실행합니다. 통합 시 다음 범위를 kapu에서 검증하되, 다른 작업의 실패는 원인과 파일 소유권을 구분해 보고합니다.

```sh
env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB \
  go test -p 2 -timeout 180s \
  ./hololive/hololive-shared/pkg/service/alarm/... \
  ./hololive/hololive-shared/pkg/service/notification/alarmservice/... \
  ./hololive/hololive-shared/pkg/service/delivery/... \
  ./hololive/hololive-alarm-worker/internal/service/alarm/... \
  ./hololive/hololive-alarm-worker/internal/service/dispatchrun/... \
  ./hololive/hololive-alarm-worker/internal/egress/... \
  ./hololive/hololive-youtube-collector/internal/runtime/collectorruntime/... \
  ./hololive/hololive-youtube-collector/internal/runtime/joblease/...
```

- subscriber·discovery·attempt tracking·발송 fence를 변경하면 같은 영향 패키지의 `go test -race -p 2 -count=1`을 실행합니다.
- sourceobservation·API projection·migration을 수정하면 해당 shared/API 패키지와 `hololive-dbtest`의 실제 PostgreSQL migration replay·schema snapshot 검사를 추가합니다. 새 migration은 멱등성, 부분 실패 후 재실행, 새 DB·업그레이드 DB, 기존 행 보존을 검증합니다.
- helper를 수정할 때만 `npm --prefix hololive/hololive-youtube-collector/youtubejs test`와 `npm --prefix hololive/hololive-youtube-collector/youtubejs run typecheck`를 실행합니다. 의존성·lockfile은 이 작업 때문에 갱신하지 않습니다.
- [workspace 검증 규칙](../../../../.agents/workflows.md)에 따라 영향 범위의 `check-stack-retry-contract`, `check-stack-db-access-policy`, `check-stack-projection-tables`, `check-stack-reissue-contract`, `check-stack-worker-contract`를 `/home/kapu/work/iris-stack` 루트에서 기존 `bash tools/checks/<name>.sh`로 실행합니다.
- 통합 완료 시 [README](../../../README.md)의 현행 local CI와 해당 Stage 3/prerequisite·NilAway·race를 통과시킵니다. architecture 영향은 기존 boundary gate로 확인합니다. checker 자체 테스트·doc/script 문구 검사·신규 gate 단계를 만들지 않습니다.
- 이미지 검증이 필요하면 `./build-all.sh --build-only --no-bump`를 사용합니다. 원격 게시가 승인된 경우에만 현행 `scripts/ci/pre-push-gate.sh`를 적용합니다. 문서는 diff·내용·참조 경로 검토로 검증합니다.

## 설계 검토와 운영 적용 경계

- 제목·표시 메타데이터 변경을 같은 event key에서 어떻게 처리할지는 별도 계약 판단입니다. 기본 수정은 collision 거절을 정직하게 반환하는 것입니다. 후속 제안은 최초 확정 snapshot을 새 방에도 재사용하되 의미가 다른 이벤트를 섞지 않는 방식이며, 현재 key/hash 계약을 바꿀 때는 구체적인 예시·테스트·영향을 준비합니다.
- 선정된 미발행 후보는 예정 시작까지 복구하고, 최신 일정 변경·종료·구독 해제를 확인하면 사유 종료하는 정책으로 확정했습니다. 조회 lookback을 늘리는 방법으로 대체하지 않습니다.
- videos/Shorts 순차 묶음의 실패 비대칭, worker의 직접 Holodex 조회와 canonical 입력 병합, `/ready`에 executor 진행을 포함하는 정책은 재현 결함 수정 뒤의 별도 설계 검토입니다. provider 호출량, ownership, probe 계약을 바꾸기 전에 실제 근거와 검증안을 준비합니다.
- 로컬 migration·cutover SQL·배포 bundle·dry-run 결과는 구현 단계에서 준비할 수 있습니다. 실제 공유 DB migration/backfill, legacy 미고정 행 처분, runtime 설정 반영·배포·재시작과 원격 쓰기는 [AGENTS.md](../../../AGENTS.md)의 명시적 승인 대상입니다. 현재 구현 요청에 이 승인까지 포함된 것으로 보지 않습니다.
- 기존 SENDING/QUARANTINED나 결과 불명 행을 자동 재시도하거나 일괄 초기화하지 않습니다. 전환 전 상태별 대상 수와 과거 request 증거를 확인하고, 증거 없는 행은 보존합니다. 실제 운영 조회는 담당 ops 경계에서 수행합니다.
- schema는 additive 확장을 우선하고 파괴적 축소는 포함하지 않습니다. 과거 send evidence·generation을 구버전 바이너리가 보존하지 못하면 단순 binary rollback이 안전하다고 가정하지 않습니다. 해당 단계는 egress 정지·증거 보존·호환 코드 또는 forward fix를 포함한 복구안을 준비합니다.
- 외부 공개 계약의 breaking 변경이나 retry/reissue 예외를 도입한다면 해당 변경의 trigger, 횟수·시간·신선도·데이터 상한, terminal 동작, telemetry, owner와 근거를 먼저 구체화합니다. 승인이 필요한 변경만 분리하고 독립적인 결함 수정·검증은 계속할 수 있게 합니다.

## 실행 기록 (2026-09-30)

- 기존 lifecycle reducer·sourceobservation·API projection·collector helper 및 migration 245–247 변경을 보존했습니다. NULL title 제품 코드는 중복 변경하지 않았으며 독립 회귀 파일만 추가했습니다. migration manifest와 schema snapshot은 기존 내용에 additive 변경을 통합합니다.
- 사용자 `$ultracode` 실행 지시에 따라 1차 2개, 2차 3개, 후보 보존 1개, 독립 검토 1개(총 7개) 에이전트로 파일 소유권을 분리했습니다. 부모가 통합과 최종 검증을 담당합니다. 모든 Go 작업에 Modern Go Guidelines를 적용하고 언어별 모던 스킬 사용 선호를 저장소 AGENTS.md에 기록했습니다.
- 구독 cache: 제어된 DB 조회 완료→구독 변경→늦은 cache write 순서에서 기존 3개 실패(single 해지·single 추가·batch 추가)를 실제 PostgreSQL로 재현했습니다. read-through positive/negative 쓰기를 제거한 후 전체 alarm·alarmservice 테스트와 race가 통과했습니다(일반 4.923/7.491초, race 6.361/9.320초).
- NULL title: 실제 NULL·빈 문자열·정상 제목의 단건/복수 조회 및 metadata_only·legacy_unknown consumer의 관측 상태 승격 회귀를 추가했습니다. 집중 테스트 통과(3.844초), 기존 COALESCE 변경의 소유권은 유지합니다.
- 후보 보존 정책은 staging commit부터 보장하며 예정 시작 시각까지 복구합니다. 명시적 일정 변경·종료·구독 삭제는 사유 종료하고 단순 목록 누락은 취소로 취급하지 않습니다. 제목 변경은 기존 payload collision 거절 의미를 유지합니다.
- 공통 request/ID 불변성 검토에서 FAILED rearm의 room 변경과 과거 attempt evidence 초기화가 추가 발견되어 수정·회귀 검증했습니다.
- migration replay·schema snapshot 및 hololive-dbtest 전체 테스트가 통과했습니다(집중 dbtest 28.426초). 최종 전체 local CI도 통과했으며 실행 조건은 아래 검증 결과에 기록합니다.

### 통합 변경과 회귀 근거

| 재현 항목 | 구현과 주요 검증 |
|---|---|
| 방별 부분 발행 / collision 성공 오인 | 입력 ordinal receipt, rollback·혼합 batch·뒤 chunk 오류·SENT 재실행 회귀. dispatchoutbox/queue/notifier race 통과 |
| 선정 후보 소실 | staging/checkpoint atomic commit, precommit 생존 실행 보관, 1000건 chunk·제한 복구, restart·commit 응답소실·최신 일정·canonical 종료·unsubscribe·시작시각 만료. shared/checking/scheduler/dispatchrun race·NilAway 통과 |
| generic 무한 handoff / 유휴 cleanup / unknown retry | 10초 attempt·5초 finalization, empty tick maintenance, fenced quarantine, synctest queued·poll 오류·부모 취소, 실제 PG unknown/rearm 회귀 |
| 구독 부활 / NULL title | 제어된 구독 mutation 경합 4경로와 단건/복수/consumer 실제 NULL 조회. 전체 영향 패키지 일반/race 통과 |
| 동일 ID 본문 변경 / 확정 실패 quarantine | alarm·generic·YouTube 최종 request 고정, 원자 generation/retry, 설정·템플릿·restart·CAS·legacy·r1/r2 소진·결합 unknown 우선 회귀 |
| 오래된 YouTube PENDING | ledger/owner/fence 우선 expiry, no-revive·inflight 보존·aggregate·retention, orphan request body cleanup 회귀 |
| discovery 기아 | 명시적인 local contract 오류만 격리, 실패 표시·cursor 진행, first/middle/last runner·capacity1·취소·stale·DB 오류 회귀. 전체 두 패키지 race·lint·NilAway 통과 |
| 실패 attempt success 집계 | 세 pipeline 모두 실제 provider 호출에서 begin/end. 실패·unknown·timeout·cancel·panic과 성공 뒤 DB 실패를 구분. grouped 호출 수와 방별 결과 분리 |

- migration 248: alarm request snapshot/generation; 249: YouTube request/binding/legacy 판정; 250: upcoming candidate/checkpoint; 251–253: 단일 CONCURRENTLY 인덱스입니다. 모두 additive이며 manifest의 기존 245–247 실행 순서를 유지했습니다. 새 DB replay, 부분 적용 재실행, 기존 행·generation 보존을 검사했습니다.
- AP source manifest에서 제거된 cache helper를 빼고 `go list -deps`가 요구하는 새 소스를 추가했습니다. 기존 lifecycle 작업의 실제 dependency도 포함되며 원격 전송은 하지 않았습니다.
- workspace reissue 검사의 삭제된 함수명 anchor를 현재의 immutable request+persisted ID 요구 조건으로 갱신했습니다. 기존 cross-repository 상수 검사와 횟수 상한은 유지하고 checker 자체 테스트·새 gate 단계는 추가하지 않았습니다.
- Fallback delta: durable staging 뒤 upcoming 후보를 예정 시작까지 기존 budget 내에서 복구하고, SDK가 부수효과 없음을 보장한 CLIENT_REQUEST_ID_FAILED만 r1/r2로 다음 정규 attempt에서 재발급합니다. 재시도 횟수·backoff·freshness·provider 동시 상한은 유지합니다. unknown·취소·충돌은 새 ID 복구 대상이 아니며 결합 오류에서도 unknown 증거를 우선합니다. Owner는 worker/dispatchoutbox이고 실제 PG·가상시간·race 회귀로 유지 조건을 검증합니다.

### 최종 검증 결과

- 완료: 최종 통합 local CI 종료 코드 0, `[LOCAL CI] Passed`. architecture, canonical vet, integration-tag vet, staticcheck, pinned lint(0 issues), NilAway, build, production 검사, 전체 일반 test/race를 통과했습니다. 로그는 `/tmp/alarm-reliability-local-ci.log`입니다.
- 실행 명령: `env -u TEST_DATABASE_URL -u TEST_DATABASE_OWNER_TOKEN -u ALLOW_EXTERNAL_TEST_DB GOMAXPROCS=2 GOFLAGS='-p=1' RACE_TEST_PARALLEL=2 ./scripts/ci/local-ci.sh`. 외부 DB 연결 설정을 제거하고 테스트 병렬도만 줄였으며 검사 범위는 유지했습니다.
- 완료: dbtest 전체 집중 검사 28.426초, schema snapshot 생성 및 migration replay, 영향 패키지 담당별 race/NilAway, 독립 리뷰의 확인된 지적 수정. 실제 PostgreSQL 회귀는 실행했으며 별도 `RUN_INTEGRATION_TESTS=true` 옵션의 전체 integration-tag 실행은 하지 않았습니다.
- 완료: workspace retry·DB access·projection tables·reissue·worker 계약 검사 5개와 최종 `git diff --check`. 의존성 manifest와 lockfile 변경은 없습니다.

### 운영 전환 시 남은 확인

로컬 구현 완료와 별개로 운영 inventory·migration·backfill·배포·재시작은 실행하지 않았습니다. 과거 request 증거가 없는 legacy retry/SENDING/unknown 행은 현재 본문으로 추정 복원하지 않습니다. 운영 전환 전에 상태별 row 수·전송 증거를 검토하고, 기존 증거 보존과 egress 정지 후 forward fix/호환 복구 경로를 준비해야 합니다. Concurrent index 생성이 중단되면 invalid index 여부를 확인하고 승인된 migration 복구 절차로 정리해야 하며, IF NOT EXISTS를 invalid index 복구로 간주하지 않습니다. 실제 Iris 발송은 검증하지 않았습니다.

### 통합 검증 후속 수정

- 최종 전체 CI에서 기존 lifecycle 변경의 공통 generation helper가 Holodex에도 YouTube.js generation 3을 강제하는 회귀를 발견했습니다. 기존 계약대로 Holodex=2, YouTube.js=3으로 provider별 검증을 분리하고 stale/지원 밖 generation 거절을 보존했습니다. 실제 collector `test-prod` 전체, 두 영향 패키지 race·lint·NilAway가 통과했습니다. decoder나 provider 조회 증명 요구를 완화하지 않았습니다.
- notifier 직전 만료 검사가 도입되면서 과거 날짜를 사용하던 기존 발행 fixture 4건은 모두 만료로 분류됐습니다. 발행 동작을 검증하는 fixture만 현재 시각 기준의 미래 예정 시각으로 수정했고, 실제 만료 거절 회귀는 유지했습니다. notifier 전체 race 재검증이 통과했습니다(9.233초).
- API runtime·targetprojection·migrationrunner 실제 PG 검증도 통과했습니다(6.286/8.430/28.957초).
- 사용자의 추가 지시에 따라 구현 뒤 `gpt-6.1-sol`·`xhigh` 독립 검토 2개를 별도로 완료했습니다. 총 5차 작업·9개 에이전트이며, 이후 승인된 새 서브에이전트의 기본 모델 설정을 AGENTS.md에 기록했습니다.
- YouTube.js(`youtubei.js`)는 기존 2026-09-24 커밋 `57c2ab718`에서 18.0.0→18.1.0으로 갱신돼 있습니다. package.json·lockfile·로컬 설치 모두 18.1.0이며 이번 신뢰성 작업에서 버전을 추가 변경하지 않았습니다.

### Sol 6.1 xhigh 후속 검토와 수정

- 발행·후보 복구 검토가 dedup 대기 중 만료 후 발행(P2), 오래된 canonical 보강 일정에 의한 후보 종료(P1)를 재현했습니다. claim/batch join 뒤 발행 직전 만료 필터·Skipped 집계·claim 정리를 추가하고, provider의 이번 응답은 persisted merge 전에 복제해 복구에 전달하도록 수정했습니다. 최신 canonical 사실의 종료는 selected_at 비교가 있는 SQL이 담당하며 최신 최초공개도 포함합니다. 이미 수용된 delivery의 재시도 정책을 후보 만료로 변경하지 않습니다.
- 당시 후속 재검토에서 두 지적과 최초공개 분류의 연결을 해당 재현 입력으로 확인했습니다. 이후 실제 schedule/Premiere writer의 시각 계약을 포함한 추가 검토에서 같은 원인의 남은 경로를 발견했고 아래에 수정 근거를 기록했습니다.
- 수정 후 checker/notifier/scheduler/dispatchoutbox 전체 race가 통과했습니다(9.297/4.479/1.046/6.313초). 실제 Check+격리 PG에서 stale canonical/provider 실패는 후보를 유지하고 fresh provider 일정 변경·최신 canonical 최초공개는 종료함을 검증했습니다. pinned lint 및 NilAway도 통과했습니다.
- 전체 local CI의 일반 테스트 중 nested workspace 실행 한 건은 Docker reaper 등록 조회 timeout으로 fixture 준비가 실패했습니다. 같은 package의 외부 일반 실행 및 production 검사는 통과했습니다. 검사 제외 없이 GOMAXPROCS=2, 패키지 병렬도 1(전체 race는 기존 최소 2)로 같은 전체 CI를 재실행해 통과했습니다. nested workspace 일반 검사도 153.172초에 통과했습니다.

### 후속 적대적 리뷰와 게시·운영 반영 준비

- 사용자가 추가 적대적 리뷰·이슈 수정·커밋·푸시·라이브 반영을 요청했으며, 별도 수명 정합성/D3 변경도 같은 반영 범위로 명시했습니다. migration 245–254 및 YouTube.js 두 관측 계약의 세대 전환을 포함해 준비합니다.
- 공통 발송 오류 분류에서 결합된 dial/확정 handoff 실패가 다른 분기의 응답 소실을 가리는 결함을 재현했습니다. 응답 소실을 TransportAmbiguous로 보존하며 TransportError의 결합 원인은 모든 분기가 전달 전 실패를 증명할 때만 Failed로 처리하도록 수정했습니다. 서로 다른 결합 형태의 회귀 3건이 수정 전 실패·수정 후 통과했고, 실제 PostgreSQL generic outbox에서 ID generation 0·provider 호출 1회·quarantine을 확인했습니다.
- 변경 후 sendoutcome·delivery·alarm dispatch·YouTube send engine 전체 race가 통과했습니다(1.012/13.095/16.711/18.378초). 추가 결합 conflict의 PostgreSQL 회귀도 race로 통과했습니다.
- 독립 적대적 검토가 실제 schedule writer의 `last_seen_at=예정 시각`을 최신 관측으로 오인한 후보 종료와, 실제 Premiere writer가 `last_seen_at`을 유지하여 새 분류를 놓친 후보 복구를 재현했습니다(수정 전 actual PG 5.129/5.887초 실패). migration 254는 nullable 상태·일정 관측 시각 2개를 추가하며 기존 시각·업무값과 과거 NULL 증거를 보존합니다. 각 값과 provenance는 같은 owner upsert로 적용하고, 일정 없는 positive·유지된 상태는 해당 clock을 전진시키지 않습니다. 후보 SQL은 선정 이후의 상태·일정 관측만 사용합니다. 확정된 최초공개는 불변 분류이므로 수신/선정 시각과 무관하게 제외합니다. 실제 Publish→Stage→Consume→Pending 경합도 회귀로 보존합니다.
- 중앙 운영 DB 읽기 전용 guard(on)와 statement_timeout 5초를 증명했습니다. 기존 snapshot/video 관측 큐의 PENDING/PROCESSING과 진행 중 발송은 0건이며 YouTube PENDING 5건은 2026-05-17 생성된 과거 행입니다. 기존 전송 가능 증거와 ledger를 보존하고 현행 신선도 만료 절차가 처리하도록 준비합니다.
- 확인 시점에 중앙 앱과 collector a/b/c/d는 이미 중지 상태였습니다. 빌드·게시 검증은 kapu에서 수행하고, 전체 SHA·아키텍처·아티팩트 검증 후 중앙 schema/API, registry, collector fleet, worker 순서로 전환합니다. 이전 이미지·배포 파일은 롤백 지점으로 보존하며 세대 활성화 후 역방향 전환은 새 관측 큐 drain과 호환성 검증을 요구합니다.
- topology·compose service·AP version·native deploy·systemd compose 필수 계약 검사 5개가 통과했습니다. 게시 gate와 실제 라이브 완료 여부는 실행 후 별도 기록합니다.
- 최종 필드별 관측·Premiere 수정 뒤 live/schedule reducer·sourceobservation·checking·dispatchoutbox·dbtest 전체 race가 통과했습니다(1.019/1.012/67.785/12.255/6.873/32.074초). pinned lint 0 issues·NilAway·schema golden·migration manifest·AP source manifest 검사가 통과했고 독립 재검토의 잔여 확정 P1/P2는 없습니다.
