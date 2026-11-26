# 변경 이력

현재 릴리즈 버전은 루트 `VERSION`에서 관리하고 Git tag는 `r<version>` 형식을 사용합니다. 번호는 r1.0.0부터 다시 시작했으며, `v<version>` tag와 절은 이전 번호 체계의 기록입니다.
버전은 배포하거나 게시할 때만 올리며, 절차는 [릴리즈 버전 관리](docs/current/runbooks/release.md)를 따릅니다.
이 문서는 `미출시`와 최근 세 버전 구간(그 사이의 tag 없는 준비 구간은 세지 않음)만 유지하며, 이전 기록은 이 파일의 Git 이력과 tag에서 확인합니다.

## 미출시

- **일정 포스터 모델 호출 SSE 전환:** hololive-api 일정 포스터의 Gemini 호출 네 곳(행 추출·번역·게시물 대조·기간 읽기)이 `generateContent` 대신 `streamGenerateContent`(SSE)를 씁니다. 청크의 parts를 이어 붙이고 마지막 사용량·종료 사유로 하나의 응답을 만든 뒤 기존 해석을 그대로 적용합니다. `CONTINUATION` 종료는 이전처럼 SDK가 continuation token으로 이어 받습니다. 종료 사유 없이 끝나거나 후보도 차단 사유도 없이 끝난 스트림은 형식 오류가 아니라 재시도 가능한 미완료 오류(`errIncompleteStream`)입니다. 일부 내용을 보낸 뒤 `SAFETY` 같은 사유로 끝난 응답은 잘린 내용 대신 그 종료 사유로 실패합니다. 스트림이 실패해도 그때까지 받은 사용량을 기록합니다. Decisions API(`/v1beta/decisions`)는 스트리밍 모드가 없어 그대로입니다. Fallback delta: none.
- **일정 포스터 기간 읽기 Vertex 이전:** 자동 검토의 기간 읽기(`gemini-4-hydrogen`)가 Gemini Developer API 키 대신 일정표 행 추출·번역과 같은 Vertex client(ADC)를 씁니다. `SCHEDULE_POSTER_GEMINI_API_KEY_FILE`은 Vertex에 없는 Decisions API(후보 판정·변경 글 판별)에만 쓰며 설정과 mount는 바뀌지 않습니다. 기간 읽기 실패의 분류도 Vertex 기준으로 바뀌어, DNS·연결 거절·TLS·ADC 토큰 실패와 `FAILED_PRECONDITION`, 401·403·404·429·503은 자동 검토 불가로, 그 밖의 400은 후보 문제로 봅니다. Fallback delta: none.

## r1.2.12 - 2026-11-26

- **종료 live head 일회성 복구 퇴역:** 2026-09-28에 승인 범위 24건을 적용해 끝난 과거 LIVE head 대조 복구(`scripts/maintenance/preview-terminal-live-heads.sql`·`reconcile-terminal-live-heads.sql`, `scripts/runtime/test-terminal-live-head-repair.sh`, runbook)를 지웠습니다. 대상 조건이 2026-08-17 이전 head로 고정돼 다시 쓸 일이 없습니다.
- **PostgreSQL 19의 기존 행 반환 활용:** replay epoch 활성화와 YouTube 발송 요청 동결에 `ON CONFLICT DO SELECT ... RETURNING`을 적용했습니다. replay epoch는 중복 활성화 시, 발송 요청은 신규·중복 요청 모두에서 별도 재조회 한 번을 줄입니다. 최초 활성화 여부는 `OLD.singleton IS NULL`로 구분하고 기존 cutoff·작업자·사유를 보존합니다. 발송 요청은 저장된 본문 해시·경로·수신방·멤버·중복 제거 키의 검증과 재발급 세대를 유지합니다. 스키마·권한·행 잠금 강도는 바꾸지 않으며 PostgreSQL 19가 필요합니다. 적용 범위와 후속 관측 데이터 최적화 조건은 [설계 문서](docs/current/architecture/postgres19-get-or-create-20261126.md)에 정리했습니다.
- **AP 전환 owner의 신호 처리:** owner가 worker pid를 기록하는 사이에 신호가 도착하면, 이어서 시작한 차단형 `wait`이 멈춘 worker를 끝없이 기다릴 수 있었습니다. 이제 신호 상태와 worker 생존을 0.1초 간격으로 함께 확인하며 기다립니다. 신호가 온 뒤 worker 종료를 기다리는 시간도 5초에서 30초로 늘렸습니다. 끝나 가는 `systemctl restart`나 부하로 늦어진 종료가 `outcome_unknown`으로 잘못 남지 않게 하기 위해서입니다. 30초 뒤에도 남은 worker는 이전과 같이 guard를 보존한 채 `outcome_unknown`으로 남습니다.

## r1.2.11 - 2026-11-25

- **osaka1 배포의 swap 경로 누락:** #881이 최초 설치용 swap 생성 코드를 지우면서 `swapfile=/swapfile` 정의도 함께 지웠습니다. 그 결과 기존 호스트 검사(`native_existing_host_check`)가 `swapfile: parameter null or not set`으로 실패해, main의 `ap-host-native-deploy.sh osaka --apply`가 원점 확인 단계에서 항상 멈췄습니다. 검사 함수가 경로를 직접 가집니다. collector 런북의 swap 서술도 "없으면 만든다"에서 "기존 swap을 확인하고 다르면 거부한다"로 고쳤습니다.

## r1.2.10 - 2026-11-25

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
