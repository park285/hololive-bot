# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **PostgreSQL major 전환 도구 퇴역:** 18→19 전환이 끝나 `pg-major-upgrade.sh`, 전역 객체·지문·default ACL 정리 SQL, 전환 런북과 prestart의 `.pg-major.pending` 처리를 지웠습니다. 빈 cluster 옆에 이전 cluster volume이 있으면 기동을 거부하는 검사는 유지합니다.
- **collector 구 이름 키 거부 퇴역:** 이름 전환이 끝나 compose env 검사와 collector 기동이 `YOUTUBE_COLLECTOR_*` 구 키를 따로 거부하지 않습니다.
- **TwentyQ 게시 포트 방화벽 규칙 제거:** TwentyQ를 stack에서 내리면서 중앙 방화벽(`admin-dashboard-ingress.nft`)의 5458 허용·거부 규칙과 운영 문서의 `twentyq-postgres` 기동·정지 안내를 지웠습니다. 적용은 다음 중앙 배포의 방화벽 reload입니다.
- **AP native 배포의 구 cache 키 거부 퇴역:** osaka1 `collector.env`에 `CACHE_*` 키가 없어, native 배포가 이 키를 이유로 거부하던 검사를 지웠습니다.
- **b 정지의 구 backup 이름 검사 퇴역:** quiesce가 이제 같은 change의 `card-po-b-<change>` 구 backup 경로를 따로 확인하지 않고, 새 backup 경로만 비어 있는지 확인합니다.
- **신호 처리 중 복원 실패 보고:** 배포 도구가 신호 trap 안에서 복원할 때, 인수 없는 `return`이 bash 규칙에 따라 trap 이전의 성공 상태를 돌려줘 guard 해제 실패 같은 오류를 성공으로 처리할 수 있었습니다. 중앙·b·native 배포 라이브러리의 해당 `return`이 실제 실패 상태를 돌려주도록 고쳤습니다.
- **AP a 정지 창 퇴역:** W3 수용이 끝나 `osaka --quiesce`·`--resume`·`--settle`·`--install-initial`을 지웠습니다. native 도구는 `--dry-run`·`--apply`·`--prepare-profile`만 받습니다. 분리 정지 창, collector 이름 전환, webhook nonce 저장소 전환 절차 문서도 지웠습니다.

## r1.2.8 - 2026-11-25

- **봇 계정이 없는 방의 발송 일시정지:** Iris `/reply-status`가 `failed`와 `reason: room_not_joined`를 돌려주면(미참가·나가기·강퇴·계정 변경) alarm dispatch는 멤버를 `cancelled`(`handoff_room_not_joined`)로, youtube delivery는 `FAILED`로 닫고 재발급·DLQ로 보내지 않습니다. 같은 정산에서 방을 새 표 `kakao_room_delivery_pauses`(migration 007)에 넣어 구독은 두고 발송만 멈춥니다. 멈춘 방의 새 alarm delivery는 `cancelled`/`room_paused`로 기록하고, YouTube fan-out은 그 방을 건너뜁니다. 봇 명령 응답도 `dead`(`handoff_room_not_joined`)로 끝납니다. hololive-api가 그 방의 메시지를 관측하면 자동으로 재개하고, 관리자는 `GET`/`DELETE /api/holo/rooms/delivery-pauses`로 목록을 보고 재개합니다. 지표 `alarm_handoff_room_not_joined_total{ledger}`, `alarm_dispatch_pg_paused_room_total`, `hololive_youtube_outbox_enqueue_paused_rooms_total`을 더했습니다. iris-client-go를 v6.0.0으로 올렸습니다. 운영 전환은 migration 007을 코드보다 먼저 적용해야 합니다.

## r1.2.7 - 2026-11-25

- **collector helper 연결 오류 응답:** youtube.js helper가 연결을 받은 뒤 2초(`headersTimeout`) 안에 요청 헤더를 다 읽지 못하면 Node 기본 처리로 본문 없는 `408`을 보냈습니다. collector는 이를 helper 프로토콜 불일치로 보고 런타임을 멈췄습니다(2026-11-25 배포 직후 부하가 높던 osaka1에서 두 번). 이제 helper는 연결 단계 오류도 프로토콜 JSON으로 답합니다. 시간 초과는 `collection_timeout`(TIMEOUT), 잘못된 HTTP는 `invalid_request`(PROTOCOL)이고, collector는 시간 초과를 다시 시도할 수 있는 실패로 처리합니다.
- **중앙 소스 트리 동기화:** `sync-opt-current.sh --preserve-runtime`이 `git archive`의 기본 `tar.umask`(0002) 때문에 그룹 쓰기 권한으로 풀린 파일로 실행 트리 검증에서 실패했습니다. 스테이징을 `tar.umask=0022`로 만들어 모든 모드에서 실행 트리를 root만 쓸 수 있게 했습니다.

## r1.2.6 - 2026-11-25

- **PostgreSQL 코드 규칙:** 다섯 모듈의 운영 SQL을 모두 `shared-go/postgres` v1.0.4의 `pgxdb.Statement`로 읽고 `pgxdb.Exec`·`Query`·`QueryRow`·`Queue`로 실행합니다. `sqlassets.MustStatementReader`가 `pgxdb.Statement`를 돌려주고 `sqlassets.WithFragment` 대신 `sqlassets.Fragment` 자리표시자와 `With`로 조각을 끼우며, `dbx.Tx`는 `pgxdb.Querier`를 품으며 `dbx.Statement.SQL`은 `pgxdb.Statement`입니다. Go 코드에서 조립하던 SQL은 `queries/*.sql`의 자리표시자와 `With`·`WithToken`으로 옮겼습니다. 433개 문장을 변경 전후로 비교해 양 끝 공백 말고는 같음을 확인했습니다. 스택 ast-grep 규칙 `go-postgres-conventions.yml` 대신 golangci-lint forbidigo가 pgx·pgxpool의 SQL 직접 실행, `BeginFunc`, 트랜잭션 직접 `Rollback`, `pgxpool.New`를 거부합니다. migration 실행·역할 bootstrap DDL, 행 수만큼 `VALUES`를 만드는 일괄 쓰기, 관리자 목록 필터만 `nolint`로 둡니다.
- **형식 검증 동기화:** `AlarmDispatchClaim` 모델에 `claim_due.sql`의 행 버전·due 재검사를 넣었습니다. 오래된 스냅샷으로 선점하는 작업자가 그 사이 회수되어 미래로 다시 예약된 retry 행을 가져가지 않는다는 `StaleClaimLeavesFutureRetry`를 검사하며, 재검사를 지운 복사본은 이 속성을 위반합니다. `CollectionJobLease`와 함께 동기화 커밋을 `5a3bb90c9`로 옮겼습니다.
