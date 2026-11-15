-- 확정 포스터 게시 뒤 멤버가 X에 올린 글의 판별 기록(schedule_poster_posts)과 그 글에서 뽑은 일정 변경(schedule_poster_changes)을 둔다.
-- 원래 행(schedule_poster_rows)은 고치지 않는다. 화면 일정은 행 묶음에 변경을 글 게시 시각 순서로 적용해 계산한다.
-- 글은 포스터에 묶지 않고 post_id로 한 번만 기록해 같은 글을 다시 판별하지 않는다. matched_poster_id·matched_rows_at은
-- 마지막으로 대응한 행 묶음(포스터와 그 rows_extracted_at)이다. 지금 게시 중인 묶음과 다르면 그 글의 변경은 읽지 않고 다시 대응한다.
-- 그래서 재추출·주 범위 수정·거절·확정본 교체가 이 표를 지우거나 되돌리지 않는다. 묶음 도장은 FK가 아니다.
-- 앞선 글이 늦게 대응되어 반영 변경을 남기면 같은 묶음에 이미 대응한 뒤 글은 도장을 비우고 relevant로 돌아가 게시 순서대로 다시 대응한다.
-- revoked는 관리자가 철회한 글이다. 도장과 변경을 지우고 되살리기(relevant로 돌림) 전까지 다시 판별·대응하지 않는다.
-- 변경의 day·start_time·status는 글이 말한 값만 담는다. NULL은 그 값을 바꾸지 않고, start_time ''은 시각을 지운다.
-- 같음(same)도 글이 말한 값을 담는다. 늦게 기록된 앞선 글의 변경을 게시 순서대로 덮기 위해서다. 한국어 칸은 갖지 않는다.
-- start_time은 019의 행 시각과 같이 포스터식 24시 넘는 표기(29:59까지)를 그대로 둔다.
-- 대상은 원래 행 번호(slot)이거나 앞선 글이 더한 방송(target_post_id, target_item)이고, 둘 다 없는 added는 새 방송이다.
-- 거절(rejected) 행은 사유와 칸 번호만 담는다.
-- 출처 글 주소는 https://x.com/i/status/<post_id>다. reason은 실패·거절 사유 코드이고 값은 Go 코드가 정한다(019의 rows_error와 같다).
-- 글은 게시 60일 뒤 지우고 변경은 함께 지워진다. 수천 행 이하라 기본 키 외 index를 두지 않는다. 이전 image는 두 표를 읽지도 쓰지도 않는다.
BEGIN;

CREATE TABLE IF NOT EXISTS public.schedule_poster_posts (
    post_id bigint NOT NULL,
    channel_id character varying(64) NOT NULL,
    posted_at timestamp with time zone NOT NULL,
    body text NOT NULL,
    status text DEFAULT 'unjudged'::text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    schedule_info real,
    kind text,
    matched_poster_id bigint,
    matched_rows_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT schedule_poster_posts_pkey PRIMARY KEY (post_id),
    CONSTRAINT chk_schedule_poster_posts_id_positive CHECK (post_id > 0),
    CONSTRAINT chk_schedule_poster_posts_status_vocab CHECK (
        status IN ('unjudged', 'not_relevant', 'judge_failed', 'relevant', 'matched', 'match_failed', 'revoked')
    ),
    CONSTRAINT chk_schedule_poster_posts_kind_vocab CHECK (
        kind IS NULL OR kind IN ('announce', 'change', 'cancel', 'weekly', 'other')
    ),
    CONSTRAINT chk_schedule_poster_posts_answer CHECK (
        (schedule_info IS NULL) = (kind IS NULL)
        AND (schedule_info IS NULL OR schedule_info BETWEEN 0 AND 1)
        AND (status NOT IN ('not_relevant', 'relevant', 'matched', 'match_failed', 'revoked') OR kind IS NOT NULL)
        AND (status <> 'unjudged' OR kind IS NULL)
    ),
    CONSTRAINT chk_schedule_poster_posts_match CHECK (
        (matched_poster_id IS NULL) = (matched_rows_at IS NULL)
        AND (status IN ('matched', 'match_failed')) = (matched_poster_id IS NOT NULL)
    ),
    CONSTRAINT chk_schedule_poster_posts_reason CHECK (
        (status IN ('judge_failed', 'match_failed')) = (reason <> '') AND char_length(reason) <= 40
    ),
    CONSTRAINT chk_schedule_poster_posts_body CHECK (char_length(body) BETWEEN 1 AND 4000)
);

CREATE TABLE IF NOT EXISTS public.schedule_poster_changes (
    post_id bigint NOT NULL,
    item smallint NOT NULL,
    kind text NOT NULL,
    reason text DEFAULT ''::text NOT NULL,
    slot smallint,
    target_post_id bigint,
    target_item smallint,
    day date,
    start_time text,
    status text,
    source_text text DEFAULT ''::text NOT NULL,
    title_ko text,
    with_ko text[] DEFAULT '{}'::text[] NOT NULL,
    place_ko text DEFAULT ''::text NOT NULL,
    tags_ko text[] DEFAULT '{}'::text[] NOT NULL,
    note_ko text DEFAULT ''::text NOT NULL,
    terms jsonb DEFAULT '[]'::jsonb NOT NULL,
    doubt_ko text DEFAULT ''::text NOT NULL,
    warnings text[] DEFAULT '{}'::text[] NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT schedule_poster_changes_pkey PRIMARY KEY (post_id, item),
    CONSTRAINT fk_schedule_poster_changes_post FOREIGN KEY (post_id)
        REFERENCES public.schedule_poster_posts (post_id) ON DELETE CASCADE,
    CONSTRAINT chk_schedule_poster_changes_item CHECK (item BETWEEN 1 AND 20),
    CONSTRAINT chk_schedule_poster_changes_kind_vocab CHECK (kind IN (
        'same', 'time_changed', 'confirmed', 'tentative', 'cancelled', 'off',
        'added', 'moved', 'moved_out', 'content_changed', 'rejected'
    )),
    CONSTRAINT chk_schedule_poster_changes_reason CHECK (
        (kind = 'rejected') = (reason <> '') AND char_length(reason) <= 40
    ),
    CONSTRAINT chk_schedule_poster_changes_target CHECK (
        (slot IS NULL OR slot BETWEEN 1 AND 100)
        AND (target_post_id IS NULL) = (target_item IS NULL)
        AND (target_post_id IS NULL OR target_post_id > 0)
        AND (target_item IS NULL OR target_item BETWEEN 1 AND 20)
        AND (slot IS NULL OR target_post_id IS NULL)
        AND (kind IN ('same', 'rejected', 'added') OR slot IS NOT NULL OR target_post_id IS NOT NULL)
    ),
    CONSTRAINT chk_schedule_poster_changes_values CHECK (
        (kind <> 'rejected' OR (day IS NULL AND start_time IS NULL AND status IS NULL AND title_ko IS NULL))
        AND (kind <> 'same' OR (title_ko IS NULL AND cardinality(with_ko) = 0 AND place_ko = '' AND cardinality(tags_ko) = 0
                                AND note_ko = ''))
        AND (kind NOT IN ('moved', 'moved_out') OR day IS NOT NULL)
        AND (kind <> 'added' OR slot IS NOT NULL OR target_post_id IS NOT NULL
             OR (day IS NOT NULL AND start_time IS NOT NULL AND status IS NOT NULL))
        AND (start_time IS NULL OR start_time = '' OR start_time ~ '^([01]?[0-9]|2[0-9]):[0-5][0-9]$')
        AND (status IS NULL OR status IN ('planned', 'tentative', 'cancelled', 'off'))
    ),
    CONSTRAINT chk_schedule_poster_changes_rejected CHECK (
        kind <> 'rejected'
        OR (target_post_id IS NULL AND source_text = '' AND cardinality(with_ko) = 0 AND place_ko = ''
            AND cardinality(tags_ko) = 0 AND note_ko = '' AND terms = '[]'::jsonb AND doubt_ko = '' AND cardinality(warnings) = 0)
    ),
    CONSTRAINT chk_schedule_poster_changes_text CHECK (
        char_length(source_text) <= 500 AND (title_ko IS NULL OR char_length(title_ko) BETWEEN 1 AND 1000)
        AND char_length(place_ko) <= 1000 AND char_length(note_ko) <= 1000 AND char_length(doubt_ko) <= 1000
    ),
    CONSTRAINT chk_schedule_poster_changes_lists CHECK (
        cardinality(with_ko) <= 40 AND cardinality(tags_ko) <= 40 AND cardinality(warnings) <= 40
        AND jsonb_typeof(terms) = 'array'
    )
);

COMMIT;
