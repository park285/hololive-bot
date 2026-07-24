-- 114_drop_unused_indexes.sql
-- 라이브 DB idx_scan=0 + 코드 참조 검증(서빙 쿼리 부재 확인) 기반 미사용 인덱스 일괄 제거.
-- 테이블이 작아 planner가 seq scan을 택할 뿐 서빙 쿼리가 코드에 존재하는 인덱스는 유지했다.

-- 통계·마일스톤 서브시스템 은퇴(966aa463)로 읽기 경로가 사라진 테이블의 인덱스.
DROP INDEX IF EXISTS idx_changes_channel_detected;
DROP INDEX IF EXISTS idx_changes_detected;
DROP INDEX IF EXISTS idx_changes_unnotified_detected_at;
DROP INDEX IF EXISTS idx_ysh_time_brin;
DROP INDEX IF EXISTS idx_milestones_channel_type;
DROP INDEX IF EXISTS idx_milestones_unnotified_achieved_at;
DROP INDEX IF EXISTS idx_approaching_unnotified;

-- 컬럼/술어 조합과 일치하는 쿼리가 프로덕션 코드에 없는 인덱스.
DROP INDEX IF EXISTS idx_ylvs_channel_time;
DROP INDEX IF EXISTS idx_ylvs_captured_at_brin;
DROP INDEX IF EXISTS idx_yls_status_topic_last_seen;
DROP INDEX IF EXISTS idx_yss_channel_ended;
DROP INDEX IF EXISTS idx_ycssp_detected_at;
DROP INDEX IF EXISTS idx_ycsas_alarm_sent_at;
DROP INDEX IF EXISTS idx_ycsas_channel_detected;
DROP INDEX IF EXISTS idx_ycat_alarm_sent_at;
DROP INDEX IF EXISTS idx_ynd_sent_cleanup;
DROP INDEX IF EXISTS idx_ydt_channel_path_event;
DROP INDEX IF EXISTS idx_ydt_post_event;
DROP INDEX IF EXISTS idx_yno_sent_cleanup;
DROP INDEX IF EXISTS idx_yno_dispatched_cleanup;
DROP INDEX IF EXISTS idx_ndo_lease_expired;
DROP INDEX IF EXISTS idx_major_events_link_check;
DROP INDEX IF EXISTS idx_members_twitch_user_id;
DROP INDEX IF EXISTS idx_members_aliases_gin;
DROP INDEX IF EXISTS idx_alarm_dispatch_admin_actions_delivery_created;
DROP INDEX IF EXISTS idx_alarm_dispatch_event_collisions_status_created;

-- alarm_dispatch_outbox 테이블은 SSOT·마이그레이션·코드 어디에도 없는 라이브 전용 고아다
-- (058은 파일명과 달리 events/deliveries/admin_actions를 만든다). 테이블 처분은 별도 결정으로
-- 남기고 인덱스만 제거한다.
DROP INDEX IF EXISTS idx_alarm_dispatch_outbox_due;
DROP INDEX IF EXISTS idx_alarm_dispatch_outbox_leased_expired;
DROP INDEX IF EXISTS idx_alarm_dispatch_outbox_room_created;
DROP INDEX IF EXISTS idx_alarm_dispatch_outbox_sending_stale;
DROP INDEX IF EXISTS idx_alarm_dispatch_outbox_status_created;

-- slug 유니크 중복 해소: 시드 ON CONFLICT arbiter는 idx_members_slug(CONVENTIONS.md, 097 복원)이므로
-- 마이그레이션 이력에 없는 레거시 constraint 쪽을 제거한다.
ALTER TABLE members DROP CONSTRAINT IF EXISTS members_slug_key;
