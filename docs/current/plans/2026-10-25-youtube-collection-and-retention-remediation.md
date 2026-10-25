# YouTube 수집 경계 정리와 DB 보존 적체 해소 통합 계획

**상태: 로컬 구현 진행 중(1단계 완료). 운영 변경·배포·migration 적용은 미실행입니다.**

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

- [ ] DB 계획의 처리량 상향 전 기준선을 확정합니다: foreground 지연, queue age, DB 슬롯 대기, WAL, 디스크 여유와 각 허용 범위.
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

- [ ] **채널 live 숨은 fan-out(리뷰 #1, 선택지 a).**
  - 한 RPC 안의 실제 upstream 호출이 Go `RateLimiter` 간격과 `jobMaxUpstreamCalls` 예산에 반영되게 합니다. 예정 영상 확인 상한 초과는 `parser_drift`가 아니라 RESOURCE_LIMIT으로 분류합니다.
  - 관측 계약은 바꾸지 않고, helper wire 변경은 같은 이미지 안의 짝 변경으로 처리합니다.
  - 선택지 b(예정 영상 확인을 Go 소유 RPC로 이동)는 snapshot의 `scheduled_at`과 이를 쓰는 API 경로를 다시 설계해야 하고 관측량도 늘어나므로 보류합니다. 3단계 운영 효과를 확인한 뒤 5단계와 함께 다시 평가합니다.
- [ ] **badge 판정(리뷰 #3).** 인식하지 못한 badge는 `ENDED` 대신 `parser_drift`로 거부합니다. 배포 뒤 snapshot 실패율이 늘면 badge 구조를 조사하며, 기본값으로 되돌리지 않습니다.
- [ ] **예정 최초공개 판정(리뷰 #4).** `classifyPublication`이 `WaitingStateConfirmed`를 직접 요구하게 하고, helper 응답 검증에 "`scheduled_at`이 있으면 대기 상태 확인" 관계를 추가합니다.
- [ ] **continuation 일시 실패(리뷰 #6).** network, 5xx, timeout으로 한정하고, 그 밖의 SDK 예외는 `parser_drift`로 둡니다.
- [ ] **공유 Innertube 초기화(리뷰 #7).** 요청 컨텍스트 밖(bootstrap)에서 생성하고, config 조회는 명시적으로 끄거나 실패를 분류된 오류로 드러냅니다.
- [ ] **PO 상태 노출(리뷰 #5).** `proof.state`, `last_error`, 발급 누계를 Go 메트릭으로 내보냅니다.
- [ ] **PO mint 취소 의미(리뷰 #8).** broker와 controller 중 한쪽 의미로 맞추고, JS fake broker를 실제 동작에 맞춥니다. broker를 바꾸면 issuer 이미지도 함께 바뀝니다.
- [ ] **Node heap 상한(리뷰 #10).** helper spawn 인자에 heap 상한을 명시합니다. 값은 0단계 관측과 기존 Node 몫 192MiB 예산에서 정합니다.
- 리뷰 #11의 낮은 항목은 같은 파일을 수정할 때만 함께 처리합니다.

### 3. DB 1단계: 삭제 가능 적체 처리량 (API)

- [ ] DB 계획의 "1 삭제 가능한 적체의 처리량 개선" 체크리스트를 그대로 수행합니다.
- 2단계와는 런타임이 달라 독립적이며 병렬로 진행할 수 있습니다. 운영 적용 전에 0단계 기준선이 있어야 합니다.
- 운영 적용 뒤 DB 계획의 24시간·7일 관찰을 수행합니다.

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
