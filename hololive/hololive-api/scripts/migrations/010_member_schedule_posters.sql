-- 번역 이미지는 Console이 JSON(base64)으로 받으므로 gateway 응답 상한(8MiB) 안에 들도록 4MiB로 제한합니다.
-- 60일 보존으로 행 수가 수백을 넘지 않아 기본 키 외 index를 두지 않습니다.
BEGIN;

CREATE TABLE IF NOT EXISTS public.schedule_posters (
    id bigint NOT NULL,
    channel_id character varying(64) NOT NULL,
    x_user_id text NOT NULL,
    post_url text NOT NULL,
    image_url text NOT NULL,
    posted_at timestamp with time zone NOT NULL,
    text_excerpt text NOT NULL,
    review_status text DEFAULT 'pending_review'::text NOT NULL,
    week_start date,
    week_end date,
    reviewed_at timestamp with time zone,
    translation_status text DEFAULT 'not_requested'::text NOT NULL,
    translation_error text DEFAULT ''::text NOT NULL,
    translated_image bytea,
    translated_mime text,
    translated_at timestamp with time zone,
    revision bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT schedule_posters_pkey PRIMARY KEY (id),
    CONSTRAINT chk_schedule_posters_id_positive CHECK (id > 0),
    CONSTRAINT chk_schedule_posters_review_status_vocab CHECK (review_status = ANY (ARRAY['pending_review'::text, 'confirmed'::text, 'rejected'::text, 'expired'::text])),
    CONSTRAINT chk_schedule_posters_translation_status_vocab CHECK (translation_status = ANY (ARRAY['not_requested'::text, 'pending'::text, 'succeeded'::text, 'failed'::text])),
    CONSTRAINT chk_schedule_posters_week_pair CHECK ((week_start IS NULL) = (week_end IS NULL)),
    CONSTRAINT chk_schedule_posters_week_span CHECK (week_start IS NULL OR week_end - week_start BETWEEN 0 AND 6),
    CONSTRAINT chk_schedule_posters_confirmed_week CHECK (review_status <> 'confirmed' OR week_start IS NOT NULL),
    CONSTRAINT chk_schedule_posters_translation_after_confirm CHECK (translation_status = 'not_requested' OR week_start IS NOT NULL),
    CONSTRAINT chk_schedule_posters_translated_image CHECK ((translation_status = 'succeeded') = (translated_image IS NOT NULL AND translated_mime IS NOT NULL AND translated_at IS NOT NULL)),
    CONSTRAINT chk_schedule_posters_translated_image_size CHECK (translated_image IS NULL OR octet_length(translated_image) BETWEEN 1 AND 4194304)
);

COMMIT;
