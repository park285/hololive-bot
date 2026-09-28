# YouTube 관측 보존 기간 단축 근거

2026-09-28 01:33 KST 기준 읽기 전용 점검이다. 운영 DB는 `hololive-osaka`의 `holo-postgres/hololive`이며 모든 SQL 세션에서 `transaction_read_only=on`을 확인했다. 파일·설정·데이터를 변경하지 않았다.

## 확인값

| 항목 | 관측 |
| --- | --- |
| 호스트 `/` | 96 GiB 중 52 GiB 사용, 45 GiB 여유 (`df -h`) |
| DB 전체 | 25 GB (`pg_database_size`) |
| `source_observations` | 14 GB, `n_live_tup` 약 580만, `n_dead_tup` 약 6천 |
| `source_observation_applications` | 6205 MB, `n_live_tup` 약 1689만, `n_dead_tup` 약 61만 |
| `youtube_live_absence_slots` | 673 MB, `n_live_tup` 약 160만, 최근 24시간 76,706행 |
| 현재 설정 | live/community/video/shorts 30일, schedule 90일, audit grace 60일, retired projection 30일, 120초·1000행 |

`TABLESAMPLE SYSTEM(0.5)` 표본에서 신규 14일 후보인 live/community/video/shorts 원본은 8,961행이었고, 28일 이상 지난 원본 연결 없는 적용 이력은 14,817행이었다. 200배 외삽하면 각각 약 180만·300만 행이다. 표본은 PostgreSQL 블록 단위이므로 종류·시점 분포에 편향이 있을 수 있고, 원본 삭제의 queue·pending replay·live-head 보호 조건을 반영하지 않는다. 정확한 전체 `count(*)`는 15초 statement timeout으로 두 차례 취소되어 강행하지 않았다.

`TABLESAMPLE SYSTEM(1)` 최근 24시간 표본에서 위 네 종류는 원본 1,175행(약 11.8만/일), 적용 이력 3,536행(약 35.4만/일)이었다. 120초·1000행의 이론상 테이블별 상한은 72만/일이며, 위 표본 유입량을 대입한 순처리 여유는 원본 약 60만/일, 적용 이력 약 37만/일이다. 표본 후보가 모두 삭제 가능하고 모든 tick이 끝까지 처리된다는 낙관적 전제에서 각각 약 3일·8일이다. FK의 `ON DELETE SET NULL`, WAL, autovacuum, 10초 DB transaction timeout, 다른 작업과의 경합 때문에 실제 완료 기간은 더 길 수 있다. 따라서 interval은 우선 120초로 유지한다.

적용 이력은 `applied_at < now() - (kind evidence age + audit grace)`이면서 `observation_id IS NULL`이어야 한다. 원본이 먼저 삭제되지 않은 적용 이력은 남는다. 일정 스냅샷은 새 설정에서 원본 30일, 적용 이력 44일이 되지만 원본이 삭제되기 전에는 이력이 삭제되지 않는다. 이전의 즉시 900만 건·4주 추산은 이 조건을 반영하지 않아 운영 근거로 사용하지 않는다.

`hololive_youtube_plane_retention_backlog_age_seconds`는 현재 `RetentionResult.BacklogAge`를 채우는 코드가 없으므로 유효한 backlog 지표가 아니다. 실제 관찰은 `hololive_youtube_plane_retention_deleted_total`, retention 오류·tick 시간, `pg_stat_user_tables`의 live/dead tuple·autovacuum 시각, DB/호스트/`pg_wal` 크기와 guarded read-only 후보 표본을 사용한다. `youtube_live_absence_slots`에는 현재 삭제 경로가 없으며, 과거 positive 재처리에서 `repository_live_absence_slots.sql`로 다시 읽는다. migration 192는 effective_at 하한이 없다는 이유로 TTL 삭제를 명시적으로 보류했다. 이 테이블은 최근 유입 기준 약 100만 행/2주가 추가되므로 별도 수명 계약과 코드 수정이 필요하다.

보존 단축으로 오래된 raw evidence를 통한 재처리·조사 가능 기간이 줄어든다. 일반 `VACUUM`은 공간 재사용만 돕고, `VACUUM FULL`·`pg_repack`은 이번 조치에 포함하지 않는다. 디스크 여유가 줄어들면 삭제 속도를 높이는 것이 오히려 WAL과 dead tuple을 늘릴 수 있다.

## 적용 이력 APPLIED 검토

운영 테이블의 `TABLESAMPLE SYSTEM(0.5)`는 전체 86,810행 중 `APPLIED` 79,877행(92.0%)이었다. 과거 94% 이상이라는 값은 이 표본에서 재현되지 않았다. `APPLIED`는 대부분 실제 정본 변경을 기록한다. 0.3% 표본의 큰 항목은 `live_snapshot/youtube_live_session` 12,078행, `viewer_sample/youtube_live_viewer_sample` 10,524행, `shorts_list/youtube_video` 6,852행, `video_list/youtube_video` 6,381행, `community_page/community_post` 6,312행이었다. 따라서 `APPLIED`를 no-op으로 보고 일괄 생략하면 감사 계약이 바뀐다.

코드에서 `source_observation_applications`를 읽는 운영 경로는 `repository_community_window_ready_0086_86.sql`이며 `community_window/CANONICALIZED`를 확인한다. 대부분의 `APPLIED` 행은 이 조회에 직접 쓰이지 않지만, 현 결정 `DEC-20260824-hololive-dependent-retention`의 kind별 추가 감사 이력이다. 안전한 절감 후보는 community window 결정 행을 유지하면서, entity_kind별 APPLIED 이력이 조사·재처리·idempotency에 사용되는지 계약을 따로 확정한 뒤 기록을 집계하거나 일부 생략하는 것이다. 이번 변경에서는 보존 유예를 14일로 줄여 크기를 제한하고 APPLIED 쓰기 경로는 바꾸지 않는다.

표본의 92.0%는 모든 `APPLIED` 행을 생략할 때의 **행 수 절감 이론 상한**일 뿐이고, 인덱스·기존 dead tuple을 포함한 디스크 절감률은 아니다. 현 감사 계약 아래 즉시 안전하다고 입증된 생략 범위는 0%다. 종류별 감사 수요와 재처리·idempotency를 정리한 후 `community_window/CANONICALIZED`를 유지하는 선택적 집계 또는 생략을 별도 결정으로 다뤄야 한다.
