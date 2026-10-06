# 재현된 리팩토링 이슈 수정과 운영 반영

## 목표와 범위

사용자가 2026-10-06 조사 완료 후 확인된 이슈의 수정, 구현 리뷰와 후속 수정, 커밋·푸시·라이브 반영을 승인했다. 기존 저장 데이터·발송 멱등성·API 계약은 유지한다. 운영 DB/런타임 변경은 소유 운영 절차를 따른다.

- [x] YouTube 발송의 outcome-unknown 증거가 영구 실패로 재분류되는 결함 수정.
- [x] Matcher 공유 적재의 호출자 취소 격리와 유한 적재 수명 보장.
- [x] Matcher snapshot 교체 뒤 이전 세대의 positive/negative 결과가 남는 결함 수정.
- [x] Observation claim 경로에서 통계 집계를 분리하고 DB 예산·종료 join 유지.
- [x] 관리 통계의 전체 멤버·알림 목록 적재를 집계 전용 조회로 대체.
- [x] 현재 sender와 저장 route를 확인하여 발송 capability 호환 구현을 축소.
- [x] 구현 리뷰, 회귀 시험, 해당 race·NilAway·로컬 CI 및 pre-push gate.
- [x] 검증된 revision을 커밋·푸시하고 kapu에서 arm64 이미지를 빌드·검증한 뒤 중앙 API/worker에 no-build 배포.
- [x] 새 revision/image ID, health/readiness, 오류 로그, 기존 rollback 지점 보존 확인.

## 조사 근거와 남은 후보

원본 checkout 변경 없이 임시 Go overlay로 재현했다. Matcher 취소 2개와 세대 불일치 1개, 발송 결과 분류 2개의 재현 시험이 현재 구현에서 실패했다. 격리 PostgreSQL에서 통계 쿼리를 지연하자 이미 claim한 작업의 큐 전달도 1.21초 지연됐다. 기존 matcher/sendoutcome race 시험과 단일 handoff 실패 시험은 통과하므로 위 조합을 제품 회귀 시험으로 추가한다.

Backlog benchmark는 활성 5천/이력 0에서 14.6ms, 활성 5천/이력 20만에서 62ms, 활성 5만/이력 20만에서 215ms였다(각 1회 서버 실행 시간). 2026-10-05 계획에도 기록된 복구 부하 특성이며 정상 부하 병목은 입증되지 않았다. claim 순서·head 정책 변경, payload 불변 표현, RPC 코드 생성, 공식 일정 parser 통합은 동작 계약·추가 측정을 요구하는 후속 설계 후보로 남긴다.

레거시 decoder/과거 발송행/env 거절 가드는 운영 잔존량과 지원 rollback 집합을 확인하기 전 삭제하지 않는다. 단순히 날짜가 지났거나 소스에서 legacy라는 이름이 검색되는 것은 제거 근거가 아니다. Twitch/Chzzk 멤버 정보와 X 자동 로그인 이력 조회는 현재 API 계약에 남아 있어 일괄 삭제하지 않는다. migration 222/230이 이미 삭제한 호환 trigger는 재작업하지 않는다.

## 검증·운영 기록

- 작업 기준: `05b8171a2`, 격리 worktree `hololive-refactor-20261006`.
- 운영 읽기 세션: 중앙 `hololive-osaka`, `holo-postgres`, `default_transaction_read_only=on` 확인.
- 전체 observation/replay 참조 집계는 3초 statement timeout에 종료됐다. 제한을 늘리지 않고 필요한 레거시 조건만 별도 조회한다.
- 좁힌 운영 조회: YouTube 저장 request 40건 모두 `text`, generic notification outbox 0건. send-unit 없는 과거 종단 행은 cancelled 14·sent 344건이므로 해당 조회 경로를 보존한다.
- 배포 승인 범위는 이번 변경의 API/worker와 필요한 배포 자산이며, 데이터 강제 삭제·보존 정책 변경·백업 정리는 포함하지 않는다.

- 구현 리뷰에서 prepared sender만 남기고 쓰이지 않는 비멱등 sender 메서드·인터페이스를 제거했다. 기존 저장 route/본문/ID 보존 시험과 unknown 결과 우선순위 시험으로 확인한다.
- Matcher, YouTube 발송 전체, generic 발송, alarm dispatch, runtime 종료, worker 조립, 집계 API의 integration/race 시험 통과. retry/reissue/worker/DB 스택 계약 검사 통과. 로컬 CI와 publication gate는 최종 revision에서 완료한다.
- Fallback delta: sender capability별 재시도 경로를 제거했다. 새 fallback은 없으며 결과 불명은 그대로 보존한다.

## 후속 레거시 정리

사용자가 같은 날 레거시 정리 마무리를 요청했다. PR #582는 main `50c4c6ff2`, 이미지 `bf3417174`로 중앙 API/worker 반영 및 health/readiness 확인을 완료했다.

- [x] 중앙·Seoul·Osaka·Osaka2의 활성 env 원천/마스터와 실행 프로세스에서 퇴역 키 67개가 0건임을 값 노출 없이 확인.
- [x] 모든 collector가 `ab078d31dc6a8c4fe8ab72bc7a3b11fb7d864b5a`, native 이전 release가 `0f40d93cc9478e03f794cd3fae36c47bf729cf65`, Seoul의 기본 rollback 이미지가 `23f6497ecdb977157f817d8622dc10a7eff88e48`이며 generation 2 producer와 퇴역 가드가 배포됐음을 확인.
- [x] 읽기 전용 운영 조회로 video_live_check schema 1 관측·본문·충돌 기록 0건, 현재 catalog schema 2·generation 2 확인 후 decoder·consumer 지원 항목 제거. channel_live_check schema 1은 현행 계약으로 유지.
- [x] 완료된 환경변수 거절 가드·호출·전용 테스트를 함께 삭제. 현행 숫자/bool/필수 인증/TLS/파일 stat/shortlink 검증은 유지.
- [x] 변경 검토, config/계약/저장·소비/collector race, lint, Compose/native 배포 및 스택 계약 검사.
- [x] 필수 publication gate 및 영향 런타임 반영.

코드 정리 착수 시점의 보존 기준: video_list generation 1과 YouTube live_snapshot generation 2 저장행은 존재한다. 이들의 bytes/hash를 재작성하거나 지우지 않는다. send_unit 없는 발송 이력은 sent 344·cancelled 14건(2026-05-31~2026-08-10)이었다. 이 테이블은 관리 작업 기록 FK의 대상이므로 조회 호환 경로를 유지한다. 최신 수량과 실제 참조 여부는 아래 폐기 준비안에 기록한다. 중앙의 비활성 `env`·`ap-compose.env`에 남은 퇴역 키는 활성 프로세스와 분리된 비밀 보관 자료이며, 이번 코드 정리에서 파일·manifest를 삭제하지 않는다. 보존 정책이나 데이터 폐기는 별도 범위다.

Fallback delta: 새 fallback은 없다. 구형 video_live_check는 오류로 거절하며, 저장된 다른 과거 관측의 해석은 유지한다.

### 후속 정리 배포 완료

- 코드 PR [#583](https://github.com/park285/hololive-bot/pull/583), main `86b5cb3c47543f4dcd8cd0b970274d23544eb6c9`; 실제 배포 산출물은 tree가 같은 `ea5d221e641b512b4af23c6f690b9ad567122f26`이다.
- 필수 pre-push 전체 Go test/race/NilAway/정적 검사와 dependency hygiene, 원격 fast-gate를 통과했다. `GO-2026-5932`는 import·호출되지 않는 OpenPGP 모듈 경고이며 실제 영향은 0으로 보고됐다.
- kapu에서 빌드한 arm64 이미지로 중앙 API/worker·중앙 c·Seoul b를, amd64 native artifact로 Osaka a·Osaka2 d를 순차 교체했다. 모든 collector와 issuer는 같은 전체 revision이며 원격 빌드는 수행하지 않았다.
- 중앙 API/worker health·readiness·인증된 worker 호출과 PG/Valkey 연결을 확인했다. a/b/c/d 모두 first_success 및 handoff PROCESSED, 새 revision·실행 파일 일치, 재시작 0과 새 오류 없음으로 인수했다. Osaka 초기 health timeout은 대기 범위 내 재확인에서 해소됐고 최종 완료 검사는 통과했다.
- 복구 지점: 중앙 API/worker `compose/rollback/legacy-ea5d221e641b512b4af23c6f690b9ad567122f26`, 중앙 c `compose/backups/po-c-20261006T040838Z`, Seoul `backups/seoul-collector-20261006T041015Z`, native a/d는 이전 `ab078d31dc6a` release와 paired issuer를 보존했다.
- 이번 배포에서 schema migration, 수동 데이터 삭제, 비밀값 변경, 보존기간 변경은 하지 않았다.

## 구형 이력 폐기 준비안

사용자는 2026-10-06 이력 폐기안 준비와 추가 연결 없이 정리하는 방향을 요청했다. 새 호환 경로·재연결·대체 이력 테이블은 만들지 않는다. 이 절은 삭제 대상·손실·복구 준비안이며 실제 DB 삭제, 보존기간 단축, 데이터 백업·복원은 실행하지 않았다.

### 대상과 확인 결과

중앙 `hololive-osaka`의 `holo-postgres/hololive`에서 `default_transaction_read_only=on`, statement timeout 3초로 확인했다. 수량은 2026-10-06 13시대 KST 조회 시점의 값이며 기존 retention으로 변한다.

| 대상 조건 | 확인 수량 | 삭제 시 사라지는 정보 |
| --- | ---: | --- |
| `source_observations`: `provider='youtubejs' AND observation_kind='video_list' AND contract_generation=1` | 10,667 | 9월 29일~10월 4일 수신한 과거 목록 관측 및 재생 근거 |
| 같은 테이블: `provider='youtubejs' AND observation_kind='live_snapshot' AND contract_generation=2` | 85,317 | 9월 29일~30일 수신한 과거 라이브 관측 및 재생 근거 |
| `alarm_dispatch_deliveries`: `send_unit_id IS NULL AND status IN ('sent','cancelled')` | sent 343, cancelled 14 | 과거 발송 조회·dedupe 근거. 취소 14건의 실제 종단일은 9월 21일 |

- 관측 두 집합 전체에서 queue·PENDING replay·현재 `end_candidate_observation_id` 참조는 0건이었다. 발송 이력의 admin action 참조·pending upcoming candidate 참조·잠금 필드는 0건이었다. 실행 직전에 같은 조건을 다시 확인한다.
- 전체 감사 연결 집계는 3초 timeout에 중단했다. 각 관측 10,001건의 제한 표본에는 application 참조가 각각 98,923건·10,599건 있으므로 전체 연결이 없다고 판단하지 않는다. 실행 전 ID 구간별로 끝까지 집계해야 한다.
- 운영 retention은 관측 7일, sent/cancelled 각각 90일이며 활성화되어 있다. 취소 이력은 생성일이 아니라 `cancelled_at`, 발송 이력은 `sent_at`, 관측은 `received_at`으로 만료를 판정한다. 기본안은 현행 기간을 유지한 순차 소멸이다. 기한 전 일괄 폐기는 별도 조기 삭제 범위다.

### 삭제 순서와 복구 조건

1. **실행 전 범위 고정:** 각 조건의 ID·당시 상태·timestamp·참조를 DB 내부에서 제한 배치로 확정하고, 아직 만료되지 않은 행과 활성 queue/replay/현재 판정 참조가 있는 행은 제외한다. 기존 retention과 동시에 대상을 바꾸지 않도록 해당 유지보수 실행을 조정할 창을 정한다. 전체 참조 집계가 미완료이면 강제 삭제를 시작하지 않는다.
2. **복구 자료 준비:** 별도 승인된 저장 위치에 같은 일관성 시점의 대상 observation·공유 payload·delivery·필요한 부모 event와 영향받는 자식 행/FK 매핑을 보존한다. payload/hash를 재작성하지 않는다. 방 식별자·본문·운영자 정보는 보고서나 Git에 넣지 않는다. 현재 취소된 자동 백업을 재활성화하지 않는다. 격리 DB에서 복원하여 행 수·참조·hash·구세대 decoder 조회를 검증한 복구 자료가 없으면 삭제하지 않는다.
3. **한정 삭제:** 관측은 기존 retention의 queue 없음·PENDING replay 없음·현재 end-candidate 참조 없음 조건을 보존하면서 승인된 ID 집합만 최대 1,000행씩 처리한다. 현재 retention 함수는 kind/time 범위여서 구세대 ID 집합만 지정할 수 없으므로, 이를 그대로 호출해 다른 세대까지 지우지 않는다. 조기 폐기를 택하면 같은 불변조건을 갖춘 한정 실행문과 격리 DB 검증을 먼저 준비한다. 발송도 고정 ID와 종단 상태·NULL send unit을 재검사한다. 오류·수량 불일치·잠금 경합에는 중단하고 이미 커밋한 배치를 보고한다.
4. **관련 데이터 정리:** FK의 `SET NULL`·`CASCADE` 영향을 삭제 영수증에 기록한다. observation 삭제는 application/replay/conflict/profile/photo/availability 등 기존 참조를 NULL로 만들고 queue/viewer evidence를 연쇄 삭제할 수 있다. 새 연결은 만들지 않는다. 감사 행 자체는 소유 retention을 따르고, 이를 즉시 함께 폐기하려면 별도 대상에 포함한다. payload는 새 세대를 포함한 모든 observation에서 참조가 사라진 행만 기존 orphan 정책으로 지운다. event/send unit/현행 generation catalog·projection 상태는 일괄 삭제하지 않는다.
5. **검증과 코드 제거:** 대상 잔여·의도하지 않은 삭제·고아 참조·중복 발송·readiness를 확인한다. 구세대 저장행뿐 아니라 replay/collision/지원 rollback producer에도 사용이 없을 때만 `video_list` generation 1·YouTube `live_snapshot` generation 2 decoder와 NULL-send-unit 조회 분기를 제거하고 해당 회귀/race 검사를 수행한다. 저장행이 남은 동안에는 현재 읽기 경로를 유지한다.
6. **복구:** 커밋 전 오류는 transaction rollback으로 취소한다. 커밋 후에는 쓰기·retention·재처리를 통제한 창에서 payload/부모 event → observation/delivery → 실제 삭제된 자식 행 및 NULL로 바뀐 FK 매핑 순서로 복원한다. 현재 행의 변경과 충돌하면 덮어쓰지 않고 중단한다. decoder를 이미 제거했다면 기존 schema와 호환되는 보존 이미지를 먼저 복원한다. 이미지 rollback만으로 삭제된 DB 이력은 돌아오지 않는다. `VACUUM FULL`, volume 축소, 복구 자료 삭제는 이 안의 범위가 아니다.

### 실행 전 필요한 결정

현행 retention 완료를 기다리는 기본안은 현재 기간·서비스를 바꾸지 않는다. 즉시 폐기를 원하면 관측 95,984건과 종단 발송 최대 357건 중 실행 시점의 정확한 집합, 감사 이력의 동반 삭제 여부, 복구 자료 저장 위치·보존 기간 및 필요한 유지보수 창을 확정하여 승인받는다. 기존 배포 승인에는 운영 데이터의 조기 삭제와 백업·복원이 포함되지 않는다.
