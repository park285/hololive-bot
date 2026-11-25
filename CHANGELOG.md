# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **AP a 수용 중 issuer 종료 허용:** osaka1 native 배포의 수용 단계는 `accepting` 표식이 있는 동안 모든 기동을 막습니다. 이때 issuer가 `worker_timeout` 같은 정상 종료로 내려가면 socket 기동이 조건 불충족으로 반복해서 건너뛰어지고 `trigger-limit-hit`으로 실패해, 수용이 결과 기록 없이 중단됐습니다(2026-11-25 osaka1 r1.2.9 배포 14:32Z·15:06Z 두 번). 이제 수용은 issuer가 정상 종료로 비활성이고 socket이 살아 있거나, 이번 socket 실행의 trigger 한도 기록이 journal에 있는 상태를 차단 중 정지로 받아들입니다. 수용 표식을 게시한 뒤 `reset-failed`와 socket 기동으로 issuer를 다시 열고, 후보 issuer가 healthy인지 확인합니다. 정책 확정 직후 재시작이 `ExecCondition`으로 건너뛰어진 경우(`Result=exec-condition`)도 같은 정지로 봅니다.
- **대기 migration 창의 collector 정지·재기동:** `scripts/deploy/collector-pause.sh <a|b|c|d> <stop|start>`가 실행 중인 release 그대로 collector unit만 멈추고 다시 띄웁니다. 정지는 비활성·`MainPID=0`·`Result=success`로, 재기동은 같은 프로세스가 60초 동안 재시작 없이 유지되는지로 확인하고, a는 이어서 `ap-completion-check.sh osaka`를 실행합니다. osaka1에 native 전환 복구 상태가 있으면 거부합니다. #881로 a 정지 창이 사라진 뒤 대기 migration을 적용할 경로가 없던 문제를 메웁니다. 절차는 릴리즈 런북의 "대기 migration 적용 창"입니다.
- **중앙 migration 재개 전 연결 확인:** `quadlet-redeploy-service.sh --resume`이 `migration/` 표식을 만든 뒤에야 writer 연결 0건을 확인해, 남은 collector 연결 하나로도 migration을 시작하지 않은 채 재시도 불가 상태가 됐습니다. 이제 표식 전에 `writer-drain-guard.py --precheck`로 읽기 전용 확인을 하고, 연결이 남아 있으면 표식 없이 거부합니다. 배포 helper 문서가 단일 서비스 배포도 대기 migration을 적용한다고 적던 부분도 실제 동작(대기 migration이 있으면 거부)에 맞게 고쳤습니다.
- **AP native rollback의 collector 정지 누락:** rollback이 `systemctl cat`을 권한 없이 호출해, root 전용 drop-in 대상을 읽지 못하면 collector unit이 없다고 보고 `disable --now`를 건너뛴 뒤 "collector unit still active"로 끝났습니다. 이제 `sudo -n systemctl cat`으로 확인합니다.
- **PostgreSQL major 전환 도구 퇴역:** 18→19 전환이 끝나 `pg-major-upgrade.sh`, 전역 객체·지문·default ACL 정리 SQL, 전환 런북과 prestart의 `.pg-major.pending` 처리를 지웠습니다. 빈 cluster 옆에 이전 cluster volume이 있으면 기동을 거부하는 검사는 유지합니다.
- **collector 구 이름 키 거부 퇴역:** 이름 전환이 끝나 compose env 검사와 collector 기동이 `YOUTUBE_COLLECTOR_*` 구 키를 따로 거부하지 않습니다.
- **TwentyQ 게시 포트 방화벽 규칙 제거:** TwentyQ를 stack에서 내리면서 중앙 방화벽(`admin-dashboard-ingress.nft`)의 5458 허용·거부 규칙과 운영 문서의 `twentyq-postgres` 기동·정지 안내를 지웠습니다. 적용은 다음 중앙 배포의 방화벽 reload입니다.
- **AP native 배포의 구 cache 키 거부 퇴역:** osaka1 `collector.env`에 `CACHE_*` 키가 없어, native 배포가 이 키를 이유로 거부하던 검사를 지웠습니다.
- **b 정지의 구 backup 이름 검사 퇴역:** quiesce가 이제 같은 change의 `card-po-b-<change>` 구 backup 경로를 따로 확인하지 않고, 새 backup 경로만 비어 있는지 확인합니다.
- **신호 처리 중 복원 실패 보고:** 배포 도구가 신호 trap 안에서 복원할 때, 인수 없는 `return`이 bash 규칙에 따라 trap 이전의 성공 상태를 돌려줘 guard 해제 실패 같은 오류를 성공으로 처리할 수 있었습니다. 중앙·b·native 배포 라이브러리의 해당 `return`이 실제 실패 상태를 돌려주도록 고쳤습니다.
- **AP a 정지 창 퇴역:** W3 수용이 끝나 `osaka --quiesce`·`--resume`·`--settle`·`--install-initial`을 지웠습니다. native 도구는 `--dry-run`·`--apply`·`--prepare-profile`만 받습니다. 분리 정지 창, collector 이름 전환, webhook nonce 저장소 전환 절차 문서도 지웠습니다.
- **스크립트 시험 fixture 정리:** `scripts/scripttest`가 각 fixture 명령을 새 세션에서 실행하고 시험이 끝날 때 그 세션에 남은 프로세스를 모두 종료합니다. 신호 시험에서 worker가 만든 하위 프로세스(가짜 `systemctl`)가 프로세스 그룹 정리를 빠져나가 고아로 남던 문제를 고쳤습니다.

## r1.2.9 - 2026-11-25

- **osaka1 영구 drop-in 고정화 및 preflight 진단:** 배포 창 수용 시 unit별 `/etc/systemd/system/<unit>.d/90-native-window-<nonce>.conf` symlink가 누적되어 구 릴리스 정리를 막던 문제를 고쳤습니다. 이제 수용 단계에서 릴리스 경로에 의존하지 않는 고정 파일 `90-native-window-permanent.conf`를 설치하고 `daemon-reload` 적재를 확인한 뒤 세 unit의 기존 `90-native-window-*.conf` symlink를 걷어 냅니다. `apply-origin`과 `native_same_window_baseline`의 각 실패 지점에도 표준 오류 진단 메시지를 추가했습니다.

## r1.2.8 - 2026-11-25

- **봇 계정이 없는 방의 발송 일시정지:** Iris `/reply-status`가 `failed`와 `reason: room_not_joined`를 돌려주면(미참가·나가기·강퇴·계정 변경) alarm dispatch는 멤버를 `cancelled`(`handoff_room_not_joined`)로, youtube delivery는 `FAILED`로 닫고 재발급·DLQ로 보내지 않습니다. 같은 정산에서 방을 새 표 `kakao_room_delivery_pauses`(migration 007)에 넣어 구독은 두고 발송만 멈춥니다. 멈춘 방의 새 alarm delivery는 `cancelled`/`room_paused`로 기록하고, YouTube fan-out은 그 방을 건너뜁니다. 봇 명령 응답도 `dead`(`handoff_room_not_joined`)로 끝납니다. hololive-api가 그 방의 메시지를 관측하면 자동으로 재개하고, 관리자는 `GET`/`DELETE /api/holo/rooms/delivery-pauses`로 목록을 보고 재개합니다. 지표 `alarm_handoff_room_not_joined_total{ledger}`, `alarm_dispatch_pg_paused_room_total`, `hololive_youtube_outbox_enqueue_paused_rooms_total`을 더했습니다. iris-client-go를 v6.0.0으로 올렸습니다. 운영 전환은 migration 007을 코드보다 먼저 적용해야 합니다.

## r1.2.7 - 2026-11-25

- **collector helper 연결 오류 응답:** youtube.js helper가 연결을 받은 뒤 2초(`headersTimeout`) 안에 요청 헤더를 다 읽지 못하면 Node 기본 처리로 본문 없는 `408`을 보냈습니다. collector는 이를 helper 프로토콜 불일치로 보고 런타임을 멈췄습니다(2026-11-25 배포 직후 부하가 높던 osaka1에서 두 번). 이제 helper는 연결 단계 오류도 프로토콜 JSON으로 답합니다. 시간 초과는 `collection_timeout`(TIMEOUT), 잘못된 HTTP는 `invalid_request`(PROTOCOL)이고, collector는 시간 초과를 다시 시도할 수 있는 실패로 처리합니다.
- **중앙 소스 트리 동기화:** `sync-opt-current.sh --preserve-runtime`이 `git archive`의 기본 `tar.umask`(0002) 때문에 그룹 쓰기 권한으로 풀린 파일로 실행 트리 검증에서 실패했습니다. 스테이징을 `tar.umask=0022`로 만들어 모든 모드에서 실행 트리를 root만 쓸 수 있게 했습니다.
