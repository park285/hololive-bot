-- Migration: member news 구독 테이블 추가
-- Date: 2026-02-16

CREATE TABLE IF NOT EXISTS member_news_subscriptions (
  id SERIAL PRIMARY KEY,
  room_id VARCHAR(255) UNIQUE NOT NULL,
  room_name VARCHAR(255),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_member_news_subscriptions_created_at
ON member_news_subscriptions(created_at);
