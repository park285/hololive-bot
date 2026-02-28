-- ============================================================================
-- 008: DB 인덱스 최적화
-- 날짜: 2024-12-30
-- 목적: 실제 쿼리 패턴 분석 기반 인덱스 정리 및 최적화
-- ============================================================================

-- ============================================================================
-- 1. members 테이블 최적화
-- 문제: seq_scan 1747회 vs idx_scan 6회 (0.3% 인덱스 사용률)
-- 원인: GetAllMembers(), GetAllChannelIDs()가 전체 스캔, 
--       FindByChannelID(), FindByName() 쿼리에 적합한 인덱스 부재
-- ============================================================================

-- 1-1. channel_id 조회 최적화 (FindByChannelID 쿼리용)
-- 참고: FUWAMOCO처럼 멤버가 채널을 공유하는 케이스가 있어 UNIQUE 제약 불가
DROP INDEX IF EXISTS idx_members_channel_id;
CREATE INDEX IF NOT EXISTS idx_members_channel_id 
    ON members (channel_id) 
    WHERE channel_id IS NOT NULL;

-- 1-2. english_name 단일 조회 최적화 (FindByName 쿼리용)
DROP INDEX IF EXISTS idx_members_english_name;
CREATE INDEX IF NOT EXISTS idx_members_english_name 
    ON members (english_name);

-- 1-3. is_graduated + channel_id 복합 인덱스 (졸업 멤버 제외 필터링)
-- GetNearMilestoneMembers, GetClosestMilestoneMembers의 JOIN 조건 최적화
CREATE INDEX IF NOT EXISTS idx_members_active_channel 
    ON members (channel_id) 
    WHERE is_graduated = false AND channel_id IS NOT NULL;

-- 1-4. 미사용 인덱스 삭제 (0회 사용, 불필요한 저장공간/쓰기 오버헤드)
DROP INDEX IF EXISTS idx_members_name_search;   -- 48kb, 미사용
DROP INDEX IF EXISTS idx_members_slug;          -- 16kb, 미사용
DROP INDEX IF EXISTS idx_members_status;        -- 16kb, 미사용

-- ============================================================================
-- 2. youtube_milestones 테이블 최적화
-- 문제: seq_scan 471회 vs idx_scan 0회 (0.0% 인덱스 사용률)
-- 원인: 쿼리 조건과 인덱스 불일치
-- ============================================================================

-- 2-1. 채널+타입 복합 인덱스 (GetAchievedMilestones, HasAchievedMilestone 최적화)
-- WHERE channel_id = ANY($1) AND type = $2
DROP INDEX IF EXISTS idx_milestones_channel;
CREATE INDEX IF NOT EXISTS idx_milestones_channel_type 
    ON youtube_milestones (channel_id, type);

-- 2-2. 채널+타입+값 복합 인덱스 (HasAchievedMilestone EXISTS 쿼리 covering index)
-- WHERE channel_id = $1 AND type = $2 AND value = $3
CREATE INDEX IF NOT EXISTS idx_milestones_lookup 
    ON youtube_milestones (channel_id, type, value);

-- 2-3. 미발송 알림 부분 인덱스 유지 (GetUnnotifiedMilestones용)
-- idx_milestones_unnotified는 유지 (WHERE notified = false 쿼리 최적화)

-- 2-4. 미사용 인덱스 삭제
DROP INDEX IF EXISTS idx_milestones_achieved_at;  -- 16kb, 미사용 (ORDER BY만으론 인덱스 효과 낮음)
DROP INDEX IF EXISTS idx_milestones_unique;       -- 16kb, UNIQUE CONSTRAINT와 중복

-- ============================================================================
-- 3. alarms 테이블 최적화
-- 문제: seq_scan 87회 vs idx_scan 16회 (15.5% 사용률)
-- ============================================================================

-- 3-1. room_user 복합 인덱스 (FindByUser, ClearByUser용)
-- 이미 존재하지만 미사용 → 쿼리 조건 확인 필요
-- idx_alarms_room_user: (room_id, user_id) → 유지

-- 3-2. channel_id 인덱스는 활발히 사용 중 (6회) → 유지

-- ============================================================================
-- 4. youtube_stats_changes 테이블 최적화
-- 상태: idx_changes_detected (56회), idx_changes_unnotified (2회) - 양호
-- ============================================================================

-- 4-1. channel_id+detected_at 복합 인덱스 (MarkChangeNotified용)
-- WHERE channel_id = $1 AND detected_at = $2
CREATE INDEX IF NOT EXISTS idx_changes_channel_detected 
    ON youtube_stats_changes (channel_id, detected_at);

-- ============================================================================
-- 5. youtube_milestone_approaching 테이블 최적화
-- ============================================================================

-- 5-1. 채널+마일스톤 조회 최적화 (HasApproachingNotified용)
-- WHERE channel_id = $1 AND milestone_value = $2
-- UNIQUE CONSTRAINT가 이미 존재하므로 추가 인덱스 불필요

-- 5-2. 미사용 인덱스 정리
DROP INDEX IF EXISTS idx_approaching_channel;  -- 16kb, 미사용

-- ============================================================================
-- 6. 통계 갱신
-- ============================================================================
ANALYZE members;
ANALYZE youtube_milestones;
ANALYZE youtube_milestone_approaching;
ANALYZE youtube_stats_changes;
ANALYZE youtube_stats_history;
ANALYZE alarms;

-- ============================================================================
-- 예상 효과:
-- 1. members: 0.3% → 60%+ 인덱스 사용률 향상
-- 2. youtube_milestones: 0% → 80%+ 인덱스 사용률 향상
-- 3. 저장공간: ~120KB 인덱스 공간 절약 (미사용 인덱스 삭제)
-- 4. 쓰기 성능: 불필요한 인덱스 유지 비용 제거
-- ============================================================================
