# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

## r1.2.16 - 2026-11-26

- **일정 포스터 새 일정·변경 알림:** 포스터의 일정표 행을 처음 뽑으면 그 채널의 LIVE 구독 방에 새 일정 카드를, 게시물 대응이 일정을 바꾸면 변경 카드를 보냅니다. 카드는 `!스케쥴`과 같은 템플릿·무작위 구성으로 알림마다 한 번 그려 모든 방에 같은 그림을 보내고(일부 방 기록이 실패해 다시 시도한 방은 다른 구성을 받을 수 있음), 앞에 머리말(`📅 … 새 주간 일정표`·`🔄 … 일정이 변경되었습니다.`와 게시물 링크)을 붙입니다. 알림은 행·대응 기록과 같은 트랜잭션에서 새 표 `schedule_poster_notices`(migration 009)에 남고, bot plane이 30초마다 가져와 bot reply outbox로 보냅니다. 방 목록은 alarm-worker의 새 경로 `GET /internal/alarm/channel/:id/live-rooms`(일시 정지 방 제외)입니다. 자동 확정·자동 반영도 관리자 확인 없이 나갑니다. 보내지 못하고 `skipped`로 닫힌 알림은 다음 추출·재대응의 알림을 막지 않습니다. migration 009는 이미 뽑은 포스터와 이미 대응한 게시물을 `baseline`으로 채워 배포 직후 지난 일정을 알리지 않습니다. migration 009는 대기 migration이므로 `docs/current/runbooks/release.md`의 "대기 migration 적용 창"에서 적용하고 alarm-worker와 hololive-api를 함께 엽니다. Fallback delta: 새 재시도 예외 계약 "일정 알림 재시도"(5분 임차 뒤 다시 시도, 생긴 지 6시간이 지나면 `skipped`).
- **`!스케줄` 별칭 퇴역:** Holodex 일정 명령 `!일정`의 별칭 `!스케줄`을 지웠습니다. 주간 일정표 카드 `!스케쥴`은 그대로입니다. Fallback delta: none.

## r1.2.15 - 2026-11-26

- **평문 서식 모사 퇴역:** `shared-go`를 v2.26.1로 올렸습니다. 일반채팅과 Markdown을 끈 방에 보내는 평문이 강조를 수학용 Unicode 글꼴(`𝗵𝗲𝗹𝗹𝗼`)로, 제목을 `【…】`로 바꾸지 않고 원래 문자를 씁니다. 목록·인용·표 구조와 URL, 코드 공백은 유지합니다. 알림 발송은 렌더링한 본문을 고정한 뒤 재시도에 그대로 쓰므로 배포 전 drain이 필요 없습니다. Fallback delta: none.

## r1.2.14 - 2026-11-26

- **일정 포스터 모델 진단 문구:** 빈 응답의 진단 문구(로그와 오류)에 공급자의 `blockReasonMessage`·`finishMessage` 원문을 넣지 않고 차단·종료 사유 enum만 남깁니다. 기간 읽기를 Vertex로 옮기면서 이 원문이 기간 읽기 오류에 들어가던 문제입니다. 행 추출·게시물 대조도 같은 문구를 씁니다. 문구의 thinking·출력 token 수는 continuation hop 전체를 더한 값입니다. Fallback delta: none.
