-- Migration: 템플릿 변경 이력(Revision) 테이블
-- Date: 2026-01-22

-- 1. Revision 테이블 생성
CREATE TABLE IF NOT EXISTS notification_template_revisions (
    id BIGSERIAL PRIMARY KEY,
    template_id BIGINT NOT NULL REFERENCES notification_templates(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 2. 조회 최적화 인덱스 (template_id별 최근 순 정렬)
CREATE INDEX IF NOT EXISTS idx_template_revisions_template_created 
    ON notification_template_revisions(template_id, created_at DESC);

-- 3. 코멘트
COMMENT ON TABLE notification_template_revisions IS '템플릿 변경 이력 (저장 전 body 보관)';
COMMENT ON COLUMN notification_template_revisions.template_id IS '원본 템플릿 ID (FK, CASCADE DELETE)';
COMMENT ON COLUMN notification_template_revisions.body IS '변경 전 템플릿 본문';
COMMENT ON COLUMN notification_template_revisions.created_at IS '이력 생성 시각';
