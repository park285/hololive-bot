# YouTube 수명 정합성의 근본 해결안

설계 제안에서 로컬 구현·검증 단계로 진행했습니다. 사용자가 D3 계약 변경의 로컬 구현·검증을 승인했으며 결과를 아래에 기록합니다. 운영 migration·배포·데이터 처분 승인이나 계획 전체 완료 기록은 아닙니다.

## 목표와 확인된 근거

- 과거 UPCOMING을 임의 ENDED로 바꾸지 않고, 일정 메타데이터와 실제 수명 관측을 구분합니다. 같은 원장이 반복 경보를 만드는 원인과 새 정합성 손실을 함께 다룹니다.
- 읽기 전용 운영 조회에서 현재 활성 live_snapshot 대상의 head 없는 UPCOMING은 3건, head 없는 LIVE와 존재하는 head의 상태 충돌은 각각 0건입니다. 최근 24시간 mismatch 값은 3으로 유지됐습니다.
- 최근 해당 채널 snapshot 5개는 COMPLETE이지만 coverage.statuses가 ENDED뿐이고, 과거 영상 3개를 포함하지 않습니다. 세 건 모두 positive clock·종료 후보가 없어서 현재 finalizer 대상이 아닙니다.
- schedule_persist.persistScheduleDecision과 content_premiere.persistPremiereDecision은 정상적으로 UPCOMING 정본만 만들 수 있습니다. 따라서 모든 head 없는 UPCOMING이 정합성 위반이라는 전제는 잘못입니다.
- schedule reducer는 신규 정본의 LastSeenAt에 예정 시각을 넣는 경로가 있습니다. 이 값과 scheduled_start_time을 과거 positive 관측 시각으로 역산하면 안 됩니다.
- 현행 video_live_check target과 consumer는 canonical LIVE만 다루며, terminal reducer는 기존 positive clock을 요구합니다. 시작을 놓친 영상의 종료 수용은 현재 계약 밖입니다.
- 전체 테이블에는 활성 수집 대상 밖의 head 없는 LIVE 9건도 있습니다. migration은 활성 경보의 3건만 보고 나머지를 정상 메타데이터로 분류하면 안 됩니다.
- 9월 28일 repair는 정본 ENDED/head LIVE 24건만 처리하고 head 없는 UPCOMING 3건을 명시적으로 남겼습니다.

## 권고 설계

### 1. 정본에 수명 추적 출처를 명시합니다

제안 필드: youtube_live_sessions.lifecycle_origin. 값은 metadata_only, observed, legacy_unknown입니다. 업무 status의 UPCOMING/LIVE/ENDED 어휘는 유지합니다.

- 신규 일정·최초공개 메타데이터는 metadata_only입니다. head를 만들거나 수명 positive clock을 합성하지 않습니다.
- 실제 UPCOMING/LIVE positive 또는 아래의 검증된 terminal 사실은 observed로 전환하고 정본·head·application을 같은 기존 owner transaction에서 기록합니다.
- 이미 observed인 정본을 일정·최초공개 metadata merge가 metadata_only로 낮추지 않습니다. legacy_unknown도 메타데이터 갱신만으로 검토 완료 처리하지 않습니다.
- LIVE는 출처와 관계없이 head 존재·상태 일치를 요구합니다. observed UPCOMING의 head 누락도 실제 결함입니다.
- 기존 head/확정된 수명 application으로 출처를 증명할 수 없는 기존 행은 legacy_unknown으로 보존합니다. head 부재만으로 metadata_only를 추정하지 않습니다. 기존 LIVE의 불명확한 출처도 정상으로 면제하지 않습니다.
- 기존 live_state/persist owner와 세 writer를 함께 변경합니다. 별도 worker·자동 보정 trigger·새 runtime dependency는 만들지 않습니다.

### 2. 조회 범위와 반환 결과를 분리합니다

- youtubejscollector.liveSnapshotPayload가 반환된 session 상태 집합으로 negative coverage를 만드는 방식을 제거합니다.
- helper가 실제 조회한 상태 범위와 조회 종료·페이지 제한·접근 제한을 보고하고, collector는 그 근거를 기존 pagination/완전성 계약과 함께 검증합니다. ENDED만 반환됐다고 UPCOMING 조회 범위가 사라지거나, 빈 응답이라고 전체 조회 범위가 생기지 않아야 합니다.
- max_pages/응답 크기 제한·누락된 제한 영상 등으로 조회 범위가 완결되지 않은 관측은 positive 사실만 적용하고 부재 종료에는 사용하지 않습니다. 현재 PARTIAL/defer·lease·worker·호출 예산과 오류 의미를 유지합니다.
- 실제 조회 범위를 증명하지 못한 채 coverage를 LIVE/UPCOMING 전체로 고정하지 않습니다. 현재 순수 목록 부재만으로 UPCOMING을 종료하지 않는 규칙도 유지합니다.
- coverage 의미가 바뀌므로 contract_generation을 새 의미로 전환하고 helper/protocol/collector/API를 함께 cutover합니다. 보관된 과거 observation의 의미를 새 규칙으로 소급 해석하지 않습니다.
- metadata_only/legacy_unknown에 부재 관측이 들어왔다는 이유만으로 authoritative head나 positive clock을 생성하지 않습니다.

### 3. 영상별 확정 사실로만 수명을 정산합니다

- 기존 video_live_check/collection lease/projection을 재사용합니다. 예정 시각을 지났고 신선한 수명 관측이 없는 UPCOMING과 출처 불명 기록을 필요한 영상별 확인 대상으로 확장합니다. 미래 UPCOMING 전수 폴링·새 retry·별도 queue를 추가하지 않습니다. 현행 요청/작업 상한을 유지하며 LIVE 확인이 밀리지 않는지 검증합니다.
- 요청 영상과 canonical 채널의 identity 일치, LifecycleFactsTrusted, 모순 없는 현재 사실, 관측 시각보다 미래가 아닌 유효 ended_at, 더 새로운 positive/pending과의 정합성을 모두 요구합니다.
- 시작을 실시간으로 관측하지 못했더라도 위 검증을 통과한 명시적 종료는 받아들입니다. prior LIVE positive clock 요구를 이 좁은 terminal 경로에서만 개정합니다. started_at과 positive 관측 시각은 근거가 없으면 NULL로 남깁니다.
- 현재 video_live_check와 helper response에는 scheduled_at이 없습니다. 새 schema/generation에 optional 예정 시각을 추가하고 채널 수집의 원시 player 예정 시각·identity 검증을 재사용합니다. 기존 정본의 과거 예정 시각을 새 관측값으로 재사용하지 않습니다.
- identity·대기 상태·실제 예정 시각까지 확인한 UPCOMING과 검증된 현재 LIVE positive만 정상 observed/head 경로로 수렴합니다. 예정 시각 부재·모순은 UNKNOWN으로 남기며 새 positive clock을 합성하지 않습니다.
- 비공개·삭제 추정·목록 누락·예정 시각 경과·UNKNOWN·종료 시각 부재는 ENDED/CANCELLED의 증거가 아닙니다. 실제 취소 사실을 공급자가 증명하지 못하면 취소를 합성하지 않습니다.
- 이 확정 terminal 정산은 과거 시작 알림·재발송·새 egress를 만들지 않습니다. notification intent owner를 같은 transaction 범위에서 검증합니다.

### 4. 증거가 끝내 없는 기록도 명시적으로 처분합니다

- 현재 3건은 위 영상 확인으로 새 사실을 먼저 확보합니다. 종료를 증명하면 정산하고, 새 예정/방송 positive가 있으면 수명 추적에 편입합니다.
- UNKNOWN만 남으면 원본 status를 보존하고 운영 검토 대상으로 남깁니다. 무한 고빈도 확인 대신 현행 collection 예산 안에서 재평가하며, 자동 retry/horizon 확대는 하지 않습니다.
- 운영자가 추적 종료를 선택할 경우 기존 append-only closeout 방식과 같은 bounded CAS 검토 영수증을 수명 owner에 둡니다. 제안 저장소는 youtube_live_review_receipts이며 disposition은 closed_unresolved입니다. 원본 snapshot, 증거 참조, 운영자·사유·시각을 보존하고 payload/오류 원문은 복사하지 않습니다.
- closed_unresolved는 전송 성공이나 ENDED를 뜻하지 않습니다. 일정/수명 사실과 별개인 검토 결정입니다. 새 positive/종료 사실이나 검토한 수명 사실(상태·일정·출처·가용성 판정 등)의 변경은 기존 검토 결정을 현재 상태에 적용하지 못하게 합니다. 관측·갱신 시각과 무시한 부재 slot 증가는 수명 사실이 아닙니다(migration 262, `youtube_live_review_current_receipt`).
- 원본을 삭제하거나 임의 head를 넣지 않습니다. 기존 dispatch·dedup 원장도 변경하지 않습니다.

### 5. 실제 위반과 정보 부족을 별도 관측합니다

- 실제 head/session 상태 충돌, LIVE/head 누락, observed UPCOMING/head 누락은 정합성 결함으로 경보합니다.
- 정상 metadata_only UPCOMING, 미검토 legacy_unknown, 검토 종료된 unresolved는 별도 건수와 신선도로 노출합니다. 신규 미검토 기록은 계속 경보하고 검토 종료 영수증과 원본 snapshot이 정확히 일치하는 기록만 미검토 집계에서 제외합니다.
- 보존 총량은 계속 보여 줍니다. 지금의 raw mismatch > 0을 임의 숫자 3이나 오래됨 조건으로 면제하지 않습니다. 과거 검토 건과 새로운 실제 장애를 구분하는 것입니다.

## 남은 작업과 실제 의존성

1. 신규 origin·coverage 의미·검증된 terminal 수용·review disposition 계약을 먼저 확정합니다. 시작 미관측 terminal 수용은 기존 D3 범위와 다르므로 명시적인 계약 변경 승인이 필요합니다.
2. 다음 owner들을 함께 변경합니다.
   - collector: youtubejs protocol/channel helper, youtubejscollector/{mapper,channel,channel_live}.go.
   - shared: contracts/sourceobservation, live reducer/canend, sourceobservation의 live_state/live_persist/live_check_consumer/schedule_persist/content_premiere 및 SQL.
   - API: projection의 stale_live_videos 선택, 수명 정산·진단 query, collection_metrics.
   - migration/schema/manifest/runtime 최소 권한, 현재 서비스·운영 계약, observability alert 원본과 Grafana 생성 owner.
3. 전체 기존 행의 출처를 read-only로 분류한 뒤 보수적인 origin migration을 준비합니다. 과거 시각을 날조하지 않으며 신규 writer가 legacy_unknown을 기본 입력으로 쓰지 않도록 합니다.
4. 새 의미를 처리할 API/DB를 준비하고 검증한 collector bundle a/b/c/d를 전환합니다. 실제 migration·재시작·배포·관측 설정 반영은 각각 대상과 효과를 포함한 운영 승인 뒤 수행합니다.
5. 새 경로가 준비된 뒤 현재 3건을 exact 대상과 CAS 증거로 처리합니다. 실제 player 결과에 따라 terminal/positive/unknown을 구분하므로 완료 전에 세 건 모두 ENDED 또는 경보 0이 된다고 약속하지 않습니다.

## 검증

아래는 구현 검증 항목이며 실제 수행 결과는 문서 끝에 기록합니다.

- 일정·최초공개 metadata_only UPCOMING은 정상 headless로 분류되지만, LIVE/head 누락과 observed UPCOMING/head 누락은 검출됩니다.
- positive 사실의 정본/head 원자성, metadata merge의 observed 출처 유지, 관측 순서·재처리·충돌에서 같은 결과를 검사합니다.
- ENDED만 포함한 완결 조회와 빈 완결 조회, 페이지 제한·접근 제한·PARTIAL 관측을 구분합니다. 반환 항목의 상태가 negative 조회 범위를 바꾸지 않고 제한된 결과는 부재 종료를 만들지 않아야 합니다.
- old UPCOMING+head 없음에 검증된 explicit end를 넣으면 실제 ended_at으로만 정산되고 시작 clock은 합성되지 않으며 새 알림이 생성되지 않습니다.
- identity 불일치, 미래/모순된 시각, 더 새로운 positive, private/UNKNOWN/종료 시각 부재는 원본 수명을 보존합니다.
- 검토 영수증은 CAS 불일치·새 사실을 면제하지 않고, API/collector/Grafana 재시작 뒤에도 신규 미검토·실제 결함을 계속 검출합니다.
- kapu에서 affected Go/shared·youtubejs 기존 suite와 실제 격리 PostgreSQL/consumer smoke를 수행합니다. 실제 API read와 target refresh/queue/dispatch/비대상 불변을 관찰합니다. DB·retry·projection·worker·cross-repo contract 영향에 맞는 현행 product gate만 실행합니다.

## 수행한 검증과 승인 경계

- 착수 전에는 실제 PostgreSQL의 default_transaction_read_only=on을 증명하고 bounded aggregate/관측 구조/현재 finalizer predicate만 조회했습니다. 이후 로컬 구현·빌드·feature 검증 결과는 아래 기록과 구분합니다. 운영 배포는 수행하지 않았습니다.
- 현재 3건의 근본 처분은 authoritative source 사실 또는 운영자의 정확한 검토 결정이 필요합니다. 정보 부족은 상태 추정으로 해결하지 않습니다.
- runtime dependency·worker·호출 상한·retention·백업 정책은 변경 범위가 아닙니다.

## 구현 착수 기록 (2026-09-30)

사용자의 작업 시작 지시에 따라 로컬 코드·migration 준비를 시작했습니다. 아래 기록은 운영 반영 승인이나 계획 전체 완료를 뜻하지 않습니다.

- 첫 변경: `lifecycle_origin`의 세 값과 세 owner writer(live, schedule, Premiere)를 연결했습니다. 실제 positive와 현행 규칙으로 확정된 종료는 observed, 신규 일정·Premiere·실제 시작 미확정 메타데이터는 metadata_only입니다. 메타데이터 conflict merge는 observed와 legacy_unknown을 낮추지 않습니다.
- positive clock 없는 metadata_only/legacy_unknown에 목록 부재가 들어오면 canonical/head/pending을 쓰지 않습니다. 미확정 LIVE 메타데이터도 새 authoritative head를 만들지 않습니다. 기존 head의 갱신과 관측된 수명의 부재·종료 계약은 유지합니다.
- migration 245는 기존 행을 legacy_unknown으로 추가 분류한 뒤 실제 positive clock 쌍 또는 live_snapshot/video_live_check의 APPLIED·ENDED application으로 증명되는 행만 observed로 옮깁니다. 활성 projection 밖의 LIVE도 같은 기준으로 처리합니다. status·예정/시작/종료 시각·positive clock은 수정하지 않습니다.
- origin migration은 전용 application 인덱스로 현재 canonical에 필요한 증거만 임시 테이블에 한 번 모으고 PK keyset 최대 1000행씩 독립 commit합니다. 재실행은 이미 분류된 행을 다시 쓰지 않습니다. backfill procedure는 migration 끝에서 제거하며 자동 보정 trigger를 추가하지 않습니다. 구버전 writer의 출처 누락은 기본값 legacy_unknown으로 남고 새 owner writer는 출처를 명시합니다.
- 실제 검증: kapu의 기존 live/sourceobservation/dbtest suite, 신규 출처·headless·원자성 회귀 검사, 2105행 migration 분류·재실행과 schema golden이 통과했습니다. 로컬 검증 중의 컴파일·fixture·정렬 문자열 검사·골든 드리프트·lint 지적은 수정하고 아래 결과로 재검증했습니다.

### 다음 계약 변경의 구체적 승인 대상

- video_live_check에서 canonical UPCOMING도 신뢰 가능한 영상별 수명 사실을 수용합니다. 요청 영상·canonical 채널 identity, LifecycleFactsTrusted, 현재 사실의 무모순성, 관측 시각 이하의 유효 ended_at, 더 새로운 positive/pending 부재를 모두 확인한 명시적 종료에만 기존 prior LIVE positive 요구를 해제합니다. 시작을 관측하지 못했다면 started_at·positive clock은 NULL로 유지하고 과거 시작 알림은 만들지 않습니다.
- live_snapshot coverage는 helper가 실제 조회한 범위와 pagination·접근/크기 제한 근거로 판정하는 새 contract_generation을 사용합니다. 기존 세대 observation은 기존 의미로 보존합니다.
- video_live_check 새 schema/generation의 optional scheduled_at은 해당 요청에서 얻은 raw player 예정 시각만 허용합니다. identity·대기 상태·예정 시각을 확인한 UPCOMING만 positive 경로에 넣고, 예정 시각 부재·모순과 UNKNOWN은 수명을 보존합니다.
- 이는 위 남은 작업 1번의 D3 계약 변경에 해당합니다. 사용자가 “D3 계약 변경의 로컬 구현·검증을 승인합니다”라고 명시적으로 승인했습니다. 실제 migration·배포·재시작·관측 설정 반영과 운영 세 건 처분은 별도의 운영 승인 대상입니다.

## 로컬 구현·검증 결과

- 완료한 코드 범위: origin과 세 writer, head 없는 metadata/legacy의 부재 처리, 검증된 영상별 terminal·waiting positive, 신규 관측 schema/generation과 과거 세대 decoder, helper의 실제 streams 조회 범위·pagination 증명, API target·진단·신선도 지표, append-only 검토 영수증 CAS, 관측 경보 원본과 Grafana 생성물입니다. NULL 제목이 있는 기존 행도 수명 consumer가 읽도록 수정했습니다. UPCOMING 사실만으로 근거 없는 기존 LIVE에 LIVE head를 만들지 않습니다.
- 새 계약은 YouTube.js live_snapshot schema 1/generation 3, video_live_check schema 2/generation 2입니다. API는 과거 snapshot generation 2와 video generation 1을 기존 의미로 처리합니다. 채널 확인·Holodex 세대와 실제 시작이 관측된 LIVE의 기존 grace는 유지합니다.
- 조회 호출 예산은 현행 한 페이지입니다. `streams` continuation 또는 종료 플래그 미확정은 PARTIAL, 접근 제한도 positive-only입니다. 빈 응답과 ENDED-only 응답은 실제 query proof가 완결된 경우에만 같은 전체 streams scope를 갖습니다. 미래 UPCOMING 전수 polling·runtime dependency·worker·retry/horizon/retention 확장은 없습니다. Fallback delta: none.
- 검토 대상은 최근 확인보다 새롭거나 같은 positive가 없는 UPCOMING입니다. 가용성 PUBLIC도 수명 사실은 미상일 수 있으므로 두 의미를 구분합니다. 검토 영수증은 runtime SELECT 전용이며 기록 함수 실행·직접 INSERT/UPDATE/DELETE 권한을 부여하지 않습니다. 기존 default grant도 회수합니다. 변경·삭제 거부 trigger는 영수증 보호용으로 canonical 자동 보정이 아닙니다. snapshot CAS·동일 owner 잠금 순서·SERIALIZABLE은 원자성과 TOCTOU 방지 제약입니다.
- 실제 결함과 `unresolved_unreviewed` 경보를 분리했습니다. legacy_unknown(미래 일정 UPCOMING 제외) 또는 지난 metadata_only 일정의 실제 확인 후 미정산 기록은 경보하며, 현재 원본에 적용되는 closed_unresolved만 검토 집계에서 제외됩니다. 미래 일정 legacy_unknown UPCOMING은 `legacy_unreviewed`·`legacy_not_due`로 보존·노출합니다. 같은 활성 범위의 보존 총량·출처·검토 종료·미확인 건수와 실제 확인/검토 나이를 노출합니다. 수명 관측 시각을 예정 시각에서 만들지 않습니다.

### 실제 통과한 검증

- kapu, Go 1.27.1: 변경된 live reducer·sourceobservation contracts/consumer, collector collectutil/youtubejs/youtubejscollector/joblease, API runtime/targetprojection, dbtest의 10개 package suite를 `go test -race -p 2 -count=1`로 통과했습니다. PostgreSQL 18 격리 DB에서 실제 consumer·schema 재생·origin 2105행 backfill/replay·owner transaction rollback·review CAS와 immutable receipt·target/queue 우선순위·새 registry의 진단 재구성을 확인했습니다.
- private/UNKNOWN/종료 시각 부재·채널 identity 불일치·더 새로운 positive/pending은 기존 UPCOMING을 보존했습니다. 시작 미관측 terminal은 실제 ended_at만 저장하고 started_at·positive clock과 notification outbox를 만들지 않았습니다. PUBLIC이지만 예정 사실이 없는 확인은 검토 가능하고, 확인된 waiting positive는 unresolved로 닫지 않습니다.
- YouTube.js `npm test` 226개와 `npm run typecheck`가 통과했습니다. ENDED-only/빈 완결 조회, continuation·접근/크기 제한, raw player identity·시각·대기 사실, 기존 요청 상한 검사를 포함합니다.
- golangci-lint 0 issues, NilAway, pinned staticcheck가 통과했습니다. 기존 architecture/SQL ownership/migration 경계 게이트와 stack DB access policy도 통과했습니다. 문자열 정렬 모양에 의존하던 subject candidate 검사는 실제 PostgreSQL priority/discovery 검사로 대체했습니다.
- 성능 게이트는 36개 모델 관측의 projected cycle 약 0.356초, 예산 3.6초로 통과했습니다. collector production Go JSON v2 빌드와 Linux/ARM64 API·collector·db-migrate 컴파일이 통과했습니다.
- observability의 promtool 실제 rule suite와 Grafana 생성물 동기 검증이 통과했습니다. 검토된 집계가 줄어도 실제 결함과 새 미검토 기록은 계속 경보하고 실패/오래된 snapshot은 현재 상태로 주장하지 않음을 확인했습니다. Grafana/collector/API 운영 재시작은 수행하지 않았습니다.

### 운영 읽기 전용 근거와 남은 반영

- hololive-osaka의 holo-postgres에서 `transaction_read_only=on`, statement_timeout 5초를 증명한 조회입니다. 전체 canonical 8,826행은 ENDED 8,380 / LIVE 13 / UPCOMING 433건이며 head 없는 행은 각각 5,536 / 9 / 3건, 존재하는 head와의 상태 충돌은 0건입니다. 실제 positive effective/seen clock 쌍이 있는 head는 각각 2,844 / 4 / 166건입니다. head 없는 LIVE 9건을 정상 metadata로 면제하지 않았습니다.
- source_observation_applications의 통계상 약 1,519만 행·4.60GB를 포함한 전체 application 분류 조회는 5초 제한으로 중단됐습니다. 이는 전체 출처 분류 완료가 아닙니다. 인덱스 없는 전수 분류를 반복하거나 timeout을 늘리지 않았습니다. 현재 head 근거로 3,014행이 증명되며 나머지는 application 증명을 확인하기 전까지 미상입니다.
- 이 근거로 migration 247의 확정 수명 application 부분 인덱스를 준비했고 manifest 실행 순서는 **247 → 245 → 246**입니다. 245는 인덱스의 valid/ready를 요구하고 그 뒤 canonical에 필요한 증거만 조회합니다. 실제 인덱스 생성·origin backfill·receipt schema/권한 적용은 운영 승인 후 db-migrate에서 수행해야 합니다.
- source 계약 활성화 SQL은 `scripts/migrations/manual/youtube_live_lifecycle_cutover.sql`에 분리했습니다. 이전 snapshot/video queue drain, API/schema 준비, collector a/b/c/d quiescence·검증한 bundle과 새 세대 동시 cutover가 선행 조건입니다. schema migration 준비는 세대 활성화가 아닙니다.
- 알람 후보 복구와의 통합 적대적 검토에서 `last_seen_at`의 예정 시각/보존 계약을 최신 관측으로 사용할 수 없음을 재현했습니다. migration 254의 nullable `status_observed_at`·`schedule_observed_at`은 각 owner가 적용한 사실의 실제 observation effective/received 시각만 기록합니다. 값과 clock은 같은 upsert 경계에서 갱신하며 과거 행을 추정 backfill하지 않습니다. 기존 last_seen_at·positive clock·업무 상태·예정 시각 계약은 유지합니다. 불변인 Premiere TRUE 분류는 시각과 무관하게 live 후보에서 제외합니다.
- 운영 반영·fleet bundle 전환·경보 설정 반영·현재 세 건의 authoritative player 확인/정산 또는 정확한 CAS 검토는 남아 있습니다. 운영 값은 변경하지 않았습니다. 세 건의 ENDED 또는 경보 0을 약속하지 않습니다. Git commit/push도 수행하지 않았습니다.
