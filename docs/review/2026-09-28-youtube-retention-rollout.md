# YouTube 보존 정책 운영 반영

2026-09-28 KST, 대상은 `hololive-osaka`의 `hololive-api`와 `holo-postgres/hololive`이다. 모든 운영 SQL은 `default_transaction_read_only=on`을 먼저 확인했고, migration만 검증된 `hololive-db-migrate` one-shot으로 썼다. 사용자 요청은 DB 증설 없이 보존 데이터 축소와 종료 증거 수명 구현까지 완료하는 것이다.

## 최종 적용값과 산출물

| 대상 | 전 → 후 |
| --- | --- |
| live_snapshot, community_page, video_list, shorts_list | 30일 → 14일 |
| schedule_snapshot | 90일 → 30일 |
| application audit grace | 60일 → 14일 |
| retired projection | 30일 유지 |
| live absence slots | 무기한 → 30일 |
| retention interval / batch | 120초 / 1000행 유지 |

원본과 적용 이력 여섯 값은 정적 설정 master에서 수정하고 `hololive-osaka`에 동기화했다. master/mirror의 in-place match, `0640 root:1002`, Compose `config --quiet`, 실제 container 값의 일치를 확인했다. 초기에는 retired projection을 7일로 낮췄지만 기존 projection generation 삭제가 10초 DB 제한을 두 주기 연속 초과했다. 그 값만 원래의 30일로 되돌린 뒤 API를 다시 생성했다. target table 약 322만 행의 FK cascade가 원인일 가능성은 추정이며, 직접 실행 계획·lock 경합 증거만으로 원인을 확정하지 않는다.

소스 후보는 실행 중이던 `d0f8a2feccbb47001ba7193d0df641c9d803cbbc`의 후손 `1d0dd1a683ead298fa3ee8be7164a4f77b66f735`이다. 별도 clean worktree에서 전체 `local-ci.sh`(staticcheck·golangci-lint·NilAway·Go 및 race 테스트 포함)와 다섯 배포 정적 계약을 통과했고, `kapu-multiarch`에서 arm64 API 이미지를 만들었다. 로컬·원격 이미지 ID는 `sha256:49833e60ba7637f10da89868f52093e3bbe1b3971cc7707ff70c16e8646b9fe9`, 라벨 revision은 위 40자리 SHA와 일치한다. 새 migration은 `221_live_absence_slot_retention.sql`이며 `applied=1 skipped=81 total=82`로 끝났다. 읽기 전용 사후 조회에서 ledger 221, runtime의 함수 EXECUTE=true, 테이블 DELETE=false를 확인했다.

원격 Compose에는 실행 중인 별도 PO 서비스 정의가 있어 저장소 기준 파일과 달랐다. 해당 정의를 보존하고 새 보존 키 한 줄만 더한 파일을 설치했다. 이전 Compose/manifest는 `/opt/hololive-bot/compose/rollbacks/retention-20260928-pre-1d0dd1a68/`, 이전 이미지는 `hololive-api:rollback-retention-d0f8a2fec`에 보존했다. 배포 payload는 `/opt/hololive-bot/compose/rollouts/retention-20260928-1d0dd1a68/`에 있으며 tar와 설치 파일 checksum은 로컬 준비본과 일치했다. 원격에서 빌드하지 않았고 migration을 API 재생성 전에 실행했다. API 외의 alarm-worker, collector, PostgreSQL container는 재생성하지 않았다.

## 현재 검증과 추적

마지막 API `StartedAt`은 2026-09-27 17:45:22 UTC 이후이며 이미지 ID가 후보와 같고 health=healthy, restart=0이다. bot/llm/admin health와 인증된 llm readiness 모두 통과했다. 사후 읽기 전용 조회에서 DB는 약 25GB, 호스트 여유는 45GB였다. `youtube_live_absence_slots`의 가장 오래된 `scheduled_for`는 2026-09-06이므로 30일 보존으로 즉시 지워질 행은 없다. 변경 직후 원본·적용 이력은 각각 약 8.5행/초로 삭제되어 신규 유입 표본보다 빠르다. 파일 크기는 vacuum 전후에도 즉시 줄지 않을 수 있다.

첫 24시간은 삭제량, retention 오류·tick 시간, DB·WAL·호스트 크기, `n_dead_tup`·autovacuum을 6시간 간격으로 확인한다. 초기 적용 이력 dead tuple은 약 110만 건으로 증가했으며 기본 autovacuum trigger는 scale factor 0.2, threshold 50이다. `hololive_youtube_plane_retention_backlog_age_seconds`는 현재 값이 채워지지 않아 판단에 사용하지 않는다. retired projection을 30일로 되돌린 뒤 시작 직후와 다음 120초 tick에서 retention 오류는 0건, API는 healthy/restart 0이었다. 장기 크기 안정은 적체 해소 후 7일 자료로 판정한다.

종료 증거 삭제는 제한된 함수가 tick당 최대 1000행을 지우며, 오래된 live snapshot의 queue PENDING/PROCESSING 또는 replay PENDING이 있으면 보류한다. 이미 session/head와 pending end에 반영된 사실은 대상이 아니다. 30일 이전 slot이 삭제된 뒤에는 과거 positive 재처리에서 그 slot을 복원할 수 없다. 이미지와 설정을 이전 상태로 되돌리면 이후 삭제를 멈출 수 있지만 이미 삭제된 행은 복원하지 못한다. 이전 사용자 요청에 따라 새로운 off-host 비밀 백업은 실행하지 않았다. Fallback delta: none.

`APPLIED`는 0.5% 표본에서 92.0%였고, 대부분 실제 정본 반영이다. community window는 application 테이블의 `CANONICALIZED` 결정을 다시 읽는다. 따라서 APPLIED 쓰기 생략은 하지 않았으며, 감사 계약을 분리해 검토할 후속 작업으로 남긴다.
