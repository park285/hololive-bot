# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **Valkey 9.2.1:** `valkey-cache` 이미지(이미지 태그, Quadlet `VALKEY_IMAGE`, `build-all.sh` upstream digest `3ad5a213…`)와 시험 fixture를 `valkey:9.2.1-alpine3.24`로 올렸습니다. 9.2.1은 9.2.0의 TLS 인증서 로딩 회귀와 IO thread·forkless save 충돌을 고칩니다. 9.2부터 `ZRANGEBYSCORE`·`ZCOUNT`는 `(`만 있는 점수 경계를 거부하지만, 명령 허용 스크립트는 항상 숫자를 붙인 `(<cutoff>`를 씁니다. `valkey-cache`는 `appendonly no`·`save ""`이므로 다음 배포의 컨테이너 재생성은 메모리 상태를 비웁니다. 이번 변경은 배포하지 않습니다.

## r1.2.26 - 2026-11-28

- **응답 outbox 정리의 writer 점유 상한:** `durable_prune_outbox.sql`은 종료 상태와 수동 검토 상태를 OR로 묶은 뒤 만료 행 전체를 정렬하고 나서 batch만큼 지웠습니다. 그래서 쓰기 transaction 안의 정렬 비용이 만료 backlog에 비례했고, 통계상 만료 행이 많으면 planner가 retention index 대신 표 전체 SCAN을 골랐습니다. 이제 두 상태를 각자의 partial index 순서로 batch만큼만 읽어 합친 뒤 오래된 순서로 batch를 지우므로, 정렬 입력이 batch의 두 배를 넘지 않습니다. 지우는 행과 순서(`updated_at`, `id`)는 같습니다. 만료 행이 대부분인 경우의 계획 시험과 두 상태에 걸친 정리 순서 시험을 더했습니다. Fallback delta: none.
- **SQLite 통계 갱신:** hololive-api의 `shared-go/sqlite`를 v1.0.2(`36a1225`)로 올렸습니다. bot plane 상태 DB를 열 때, 마이그레이션 뒤, 그리고 한 시간마다 `PRAGMA optimize`가 통계를 만들고 갱신합니다. 대표 행을 넣은 시험 DB에서 durability 쿼리의 계획은 바뀌지 않았습니다. pending 행이 없을 때 분석한 통계로 계획해도 durability 쿼리 53개는 적체 상태에서 새로 분석한 통계와 계획이 같습니다. security workflow가 받는 shared-go 고정 commit도 `sqlite/v1.0.2`(`36a1225`)로 올렸습니다. 검사 내용은 바뀌지 않습니다. Fallback delta: none.
- **의존성 최신화:** Go 5개 모듈의 `quic-go` v0.66.0, `miniredis/v2` v2.41.0, `moby/sys/user` v0.5.1, `cloud.google.com/go/auth` v0.24.5, `google.golang.org/api` v0.317.0과 간접 의존성, youtubejs의 `@types/node` 26.6.11과 eventgrade·po-sandbox·youtubejs lockfile을 올렸습니다. ingress nginx는 1.31.10(이미지 태그, Quadlet `NGINX_IMAGE`, `build-all.sh` upstream digest)입니다. distroless `static-debian13` 기반 이미지와 PostgreSQL 19.0-alpine(운영 `holo-postgres` upstream 포함) digest는 같은 태그의 현재 index로 바꿨습니다. CI의 `actions/setup-python`은 v7.1.0 커밋으로 맞췄습니다. Valkey 9.1.3과 PostgreSQL 19.0 버전은 계약 고정값이라 그대로 둡니다. Go 모듈과 distroless 기반 이미지는 이번 hololive-api·alarm-worker image에 들어갑니다. nginx·PostgreSQL 실행 image는 host `compose.env`의 identity가 정하므로 이번 배포로 바뀌지 않습니다.
- **릴리스:** 루트 VERSION 1.2.26, hololive-api 1.2.19, hololive-alarm-worker 1.0.16. 두 image 모두 의존성과 기반 이미지가 바뀌어 다시 빌드합니다. collector와 PO issuer는 이번에 다시 배포하지 않습니다.

## r1.2.25 - 2026-11-28

- **일정 변경 알림 카드의 형광펜 표시:** 일정 변경 알림은 머리말만 "일정이 변경되었습니다"라고 알려 어떤 일정이 바뀌었는지 카드에서 알 수 없었습니다. 이제 변경 알림으로 보내는 카드에서, 알린 게시물과 보낸 뒤 함께 `sent`로 닫히는 대기 변경 알림의 게시물이 바꾼 칸(시각·날짜·취소·휴방·추가·내용 변경, 숨겨진 칸을 묶은 "외 N건" 포함)의 제목 아래에 반투명 노란 밑줄을 그립니다. 머리말 문구, `!스케쥴` 카드, 새 일정표 알림 카드는 그대로이며, 표시한 카드는 메모리·공유 카드 캐시에 넣지 않습니다. 텍스트 파일 대체에는 표시가 없습니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.25, hololive-api 1.2.18. alarm-worker는 바뀌지 않아 다시 빌드하지 않고 API만 교체합니다.

## r1.2.24 - 2026-11-28

- **handoff 실패 발송의 관리자 취소·격리:** Iris가 요청을 받은 뒤 handoff가 실패한 DLQ(`handoff_failed`)와 확인되지 않은 격리(`handoff_unconfirmed`)에는 `sent_at`이 남습니다. 관리자 `settle`이 이 표식을 이미 보낸 발송으로 보고 409를 돌려줘, 이런 행은 보존 기간 180일이 지날 때까지 발송 실패 건수에 남았습니다. 이제 `sent_at`이 있어도 `dlq`·`quarantined` 묶음을 취소하거나 격리할 수 있습니다. `sent_at`은 보존되므로 수동 재처리 차단은 그대로이고, 상태가 `sent`인 묶음과 이미 취소된 묶음은 계속 거절합니다. Fallback delta: none.
- **릴리스:** 루트 VERSION 1.2.24, hololive-api 1.2.17, hololive-alarm-worker 1.0.15(배포 트리 revision과 같은 label의 image가 필요해 다시 빌드함).