# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

## r1.2.25 - 2026-11-28

- **일정 변경 알림 카드의 형광펜 표시:** 일정 변경 알림은 머리말만 "일정이 변경되었습니다"라고 알려 어떤 일정이 바뀌었는지 카드에서 알 수 없었습니다. 이제 변경 알림으로 보내는 카드에서, 알린 게시물과 보낸 뒤 함께 `sent`로 닫히는 대기 변경 알림의 게시물이 바꾼 칸(시각·날짜·취소·휴방·추가·내용 변경, 숨겨진 칸을 묶은 "외 N건" 포함)의 제목 아래에 반투명 노란 밑줄을 그립니다. 머리말 문구, `!스케쥴` 카드, 새 일정표 알림 카드는 그대로이며, 표시한 카드는 메모리·공유 카드 캐시에 넣지 않습니다. 텍스트 파일 대체에는 표시가 없습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.25, hololive-api 1.2.18. alarm-worker는 바뀌지 않아 다시 빌드하지 않고 API만 교체합니다.

## r1.2.24 - 2026-11-28

- **handoff 실패 발송의 관리자 취소·격리:** Iris가 요청을 받은 뒤 handoff가 실패한 DLQ(`handoff_failed`)와 확인되지 않은 격리(`handoff_unconfirmed`)에는 `sent_at`이 남습니다. 관리자 `settle`이 이 표식을 이미 보낸 발송으로 보고 409를 돌려줘, 이런 행은 보존 기간 180일이 지날 때까지 발송 실패 건수에 남았습니다. 이제 `sent_at`이 있어도 `dlq`·`quarantined` 묶음을 취소하거나 격리할 수 있습니다. `sent_at`은 보존되므로 수동 재처리 차단은 그대로이고, 상태가 `sent`인 묶음과 이미 취소된 묶음은 계속 거절합니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.24, hololive-api 1.2.17, hololive-alarm-worker 1.0.15(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).

## r1.2.23 - 2026-11-28

- **PostgreSQL bot plane 표와 함수 삭제(migration 014):** r1.2.22에서 SQLite로 옮긴 bot plane 표 7개(`bot_webhook_inbox`·`bot_webhook_heads`·`bot_command_executions`·`bot_reply_outbox`·감사 두 표·`bot_webhook_nonces`)와 `reserve_bot_webhook_nonce`·replay·discard·감사 trigger 함수 7개를 PostgreSQL에서 지웁니다. 이 표는 r1.2.22 전환 뒤 갱신되지 않았고 현행 runtime은 읽거나 쓰지 않습니다. 일회성 이관 명령 `bot-durable-admin import-postgres`와 그 SQLite 적재 경로, PostgreSQL bot 표를 검사하던 시험도 지웠습니다. head 계약은 표·함수가 없음을 확인합니다. 014는 대기 migration 적용 창(collector 정지 뒤 `--resume`)에서 적용합니다. PostgreSQL bot 표를 쓰던 r1.2.21 이하 API로는 이 migration 뒤에 돌아갈 수 없습니다. Fallback delta: none.
- **정산 쓰기 우선순위 복원:** PostgreSQL 시절 `pgxdb.WithControlAcquire`가 webhook 정산·reply 정산·heartbeat·handoff claim 9곳에 주던 우선 획득이 SQLite 전환에서 빠졌습니다. 이 9곳에 shared-go `sqlite/v1.0.1`의 `sqlitedb.WithWritePriority`를 다시 표시해, 대기 중인 일반 쓰기보다 먼저 SQLite writer를 받게 했습니다.
- **bot plane 작업자 지표 label:** `bot_webhook_inbox`·`bot_reply_outbox` 작업자의 `queue_backend` label이 `postgres`에서 `sqlite`로 바뀝니다. 두 작업자는 r1.2.22부터 SQLite 상태 DB를 씁니다.
- **security workflow의 shared-go 고정 commit 갱신:** `go.work`가 r1.2.22부터 `../shared-go/sqlite`를 쓰는데, security workflow는 이 모듈이 없는 `postgres/v1.0.4` commit을 받아 main의 NilAway와 govulncheck가 모듈을 읽지 못하고 실패했습니다. 고정 commit을 `sqlite/v1.0.1`(`78b8d8a`)으로 올렸습니다. 검사 내용은 바뀌지 않습니다.
- **ChatBotGo PostgreSQL 방화벽 규칙 삭제:** ChatBotGo가 r1.0.15에서 PostgreSQL을 퇴역해 `admin-dashboard-ingress.nft`의 5432 게시 규칙(`bot_postgres_sources` 집합과 input·forward의 허용·거부 규칙)을 지웠습니다. hololive PostgreSQL(5433) 규칙은 그대로입니다. 설치 뒤 `systemctl reload admin-dashboard-ingress-firewall.service`로 적용합니다.
- **릴리스:** 루트 VERSION 1.2.23, hololive-api 1.2.16, hololive-alarm-worker 1.0.14(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).
