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
- [ ] 필수 publication gate 및 영향 런타임 반영.

보존 기준: video_list generation 1과 YouTube live_snapshot generation 2 저장행은 존재한다. 이들의 bytes/hash를 재작성하거나 지우지 않는다. send_unit 없는 발송 이력은 sent 344·cancelled 14건(2026-05-31~2026-08-10)이며 관리 작업 기록의 FK도 연결되므로 조회 호환 경로를 유지한다. 중앙의 비활성 `env`·`ap-compose.env`에 남은 퇴역 키는 활성 프로세스와 분리된 비밀 보관 자료이며, 이번 코드 정리에서 파일·manifest를 삭제하지 않는다. 보존 정책이나 데이터 폐기는 별도 범위다.

Fallback delta: 새 fallback은 없다. 구형 video_live_check는 오류로 거절하며, 저장된 다른 과거 관측의 해석은 유지한다.
