-- 113_add_vocab_checks_acl_watermarks.sql
-- 내부 생성 어휘 컬럼 2건에 chk_<table>_<col>_vocab CHECK를 추가한다(098 철학).
-- 값 집합: acl_rooms.list_type = acl 서비스 상수(whitelist/blacklist),
-- youtube_content_watermarks.watermark_type = domain.WatermarkType(VIDEO/SHORT/COMMUNITY_POST).

ALTER TABLE acl_rooms
    DROP CONSTRAINT IF EXISTS chk_acl_rooms_list_type_vocab;
ALTER TABLE acl_rooms
    ADD CONSTRAINT chk_acl_rooms_list_type_vocab
    CHECK (list_type IN ('whitelist', 'blacklist')) NOT VALID;
ALTER TABLE acl_rooms
    VALIDATE CONSTRAINT chk_acl_rooms_list_type_vocab;

ALTER TABLE youtube_content_watermarks
    DROP CONSTRAINT IF EXISTS chk_youtube_content_watermarks_watermark_type_vocab;
ALTER TABLE youtube_content_watermarks
    ADD CONSTRAINT chk_youtube_content_watermarks_watermark_type_vocab
    CHECK (watermark_type IN ('VIDEO', 'SHORT', 'COMMUNITY_POST')) NOT VALID;
ALTER TABLE youtube_content_watermarks
    VALIDATE CONSTRAINT chk_youtube_content_watermarks_watermark_type_vocab;
