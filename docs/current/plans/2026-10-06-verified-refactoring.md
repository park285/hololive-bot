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

## 구형 관측 폐기 완료

사용자는 폐기안 준비 후 실제 정리를 승인했고, 기존 영상이 새 영상으로 판단되어 재알림되지 않아야 한다고 명시했다. 이에 삭제 범위를 구형 관측으로 한정했다. 새 호환 연결·대체 이력 테이블·자동 백업은 만들지 않았다.

- [x] 일관된 읽기 전용 snapshot에서 대상 94,326건, application 195,506건, content absence slot 3,029건, content clock 1건과 공유 payload 609건의 복구 자료를 보존하고 압축·JSON·수량·SHA256 검증.
- [x] 격리 PostgreSQL에서 원본 삭제 후 같은 slot 재수신·다음 slot 수집에도 신규성 기준점과 SENT/FAILED 알림이 유지되는 회귀 시험. LIVE/ENDED 상태와 원본·payload·감사 FK 복원 시험 통과.
- [x] API·발송 워커를 잠시 정지하고 고정 ID를 500건씩 삭제. queue·PENDING replay·현재 end-candidate 참조가 없고 schema 1의 승인된 두 세대인지 매 배치 재검증.
- [x] 실제 삭제 93,718건, 기존 retention으로 먼저 소멸한 snapshot 대상 608건, 대상 잔여 0건 확인. 최초 조사 95,984건과의 차이는 실행 전 기존 retention에 따른 감소다.
- [x] 삭제 전후 영상, 신규성 clock, 채널 기준점, watermark, absence slot, live session/head, notification outbox, delivery/send unit/event의 전체 행 digest 동일 확인. 참조 FK가 NULL로 바뀌는 두 열만 비교에서 제외했다.
- [x] API 먼저 재개하고 발송 워커 재개 전후 기존 영상 1,975개의 새 NEW_VIDEO 알림 0건 확인. API/worker healthy, 재시작 0, 인증된 worker 호출 count 24, PG/Valkey 연결과 collector readiness 확인.
- [x] 구형 video_list generation 1 및 YouTube live_snapshot generation 2 consumer 지원 제거, 현재 계약 회귀 검증과 배포.

대상은 `source_observations`의 `provider='youtubejs'` 중 `video_list` generation 1과 `live_snapshot` generation 2뿐이다. 영상·신규성 기준점·알림 및 발송 상태는 초기화하지 않았다. payload는 다른 세대와 공유되므로 수동 삭제하지 않고 기존 orphan retention에 맡긴다. application 등 감사 행은 기존 FK의 SET NULL만 적용하며 원래 retention을 유지한다.

`send_unit_id IS NULL`인 종단 발송 357건(sent 343, cancelled 14)은 전부 현행 `v2:room:` dedupe key를 사용한다. 현재 InsertBatch 충돌 방지와 findByDedupeKey에 필요한 행이므로 삭제하지 않았고, 해당 조회 분기도 유지한다. 새 연결이나 tombstone으로 대체하지 않는다.

복구 자료는 중앙 root 전용 `compose/backups/legacy-observations-20261006T043300Z`에 보존했다. `manifest.json`에 수량·열·FK·SHA256, `delete-intent-*`와 `delete-applied-*`에 실제 배치 영수증, `protected-before/after.json`에 상태 비교, `RESTORE.txt`에 복구 절차가 있다. 복구 시 실제 삭제 영수증의 ID만 payload → observation → 아직 NULL이고 다른 필드가 동일한 감사 FK 순서로 되돌린다. 변경된 현재 행을 덮어쓰거나 새 queue/replay를 만들지 않는다. 이미지 rollback만으로 DB 이력은 복구되지 않는다. 복구 자료·이전 이미지·volume은 삭제하지 않았다.

현재와 지원 rollback collector `ab078d31dc6a8c4fe8ab72bc7a3b11fb7d864b5a`는 video_list generation 2 및 YouTube live_snapshot generation 3만 생산한다. Holodex live_snapshot generation 2는 현행 계약이므로 공유 decoder를 유지한다. Fallback delta: 새 fallback 없음.

### 구형 관측 지원 제거 배포 완료

- 코드와 운영 기록 PR [#584](https://github.com/park285/hololive-bot/pull/584). 실제 배포 산출물 revision은 `05cd902cca71d63dbf8debe1f91a8b805baa0c1e`이며 이후 완료 기록 변경은 문서만 포함한다.
- 필수 pre-push 전체 일반/race/NilAway/정적 검사와 dependency hygiene, 코드 revision의 원격 fast-gate가 통과했다. import·호출 취약점은 0건이며 기존 미사용 OpenPGP 모듈 경고만 남는다.
- 중앙 API/worker 및 a/b/c/d collector·issuer 모두 kapu에서 빌드한 같은 revision으로 반영했다. 중앙·Seoul은 arm64 Compose, Osaka·Osaka2는 amd64 native 산출물이며 원격 빌드는 수행하지 않았다.
- API image `sha256:23bcd5cbae3961bc178aeab0aa4b94cbae8c747c0d7a7f54f9448f75d7176c44`, worker image `sha256:97a1c7d1de0e525289b4615d2409dafcde5deb7a6b7b78f9107d0e59da0c26bd`가 실제 실행 이미지와 일치한다. 두 서비스 healthy, 인증된 worker 호출 count 24, PG/Valkey 연결과 새 오류 없음 확인.
- 모든 collector의 first_success, handoff PROCESSED, readiness와 실행 파일/manifest 일치를 확인했다. API/worker/collector 재시작은 0이며 issuer는 정상 generation 교체를 허용하는 종료 코드 0·OOM 없음·healthy 기준을 적용했다.
- 복구 지점: 중앙 API/worker `compose/rollback/legacy-05cd902cca71d63dbf8debe1f91a8b805baa0c1e`, 중앙 c `compose/backups/po-c-20261006T050602Z`, Seoul `backups/seoul-collector-20261006T050722Z`. Native a/d의 previous는 각각 `20261006T041057Z-ea5d221e641b-osaka`, `20261006T041501Z-ea5d221e641b-osaka2`로 보존했다.
- 최종 읽기 전용 확인에서도 폐기 대상 관측·충돌 기록 0건, 보호한 기존 영상 1,975개의 새 NEW_VIDEO 알림 0건, NULL-send-unit 발송 이력 sent 343·cancelled 14건 유지.
