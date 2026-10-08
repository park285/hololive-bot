-- 2026-10-08 서울 운영: 종료 전에 비공개·삭제로 바뀐 방송의 LIVE 세션 4건이 익명 영상 확인에서 identity_missing만 남겨
-- 명시적 종료(identity 필요)도 부재 종료(채널 스냅샷이 ENDED까지 scope에 넣어 PARTIAL)도 받지 못한 채 영구히 LIVE였다.
-- head에 마지막 LIVE positive 이후 identity_missing이 이어진 첫 관측 시각을 두고 end_reason 어휘에 UNRESOLVABLE_VIDEO를
-- 더한다. 열 추가는 rewrite 없는 catalog 변경이고, CHECK는 NOT VALID로 선언한 뒤 VALIDATE해 ACCESS EXCLUSIVE 잠금을 짧게
-- 끊는다(3,962행). 어휘 CHECK 이름은 스키마 표준(chk_<table>_<col>_vocab)으로 바꾼다. 모든 문장은 멱등이다.
BEGIN;
SET LOCAL lock_timeout = '3s';
ALTER TABLE public.youtube_live_reconciliation_heads ADD COLUMN IF NOT EXISTS unresolvable_since timestamptz;
COMMENT ON COLUMN public.youtube_live_reconciliation_heads.unresolvable_since IS
    '마지막 LIVE positive 이후 영상 확인이 identity_missing으로만 이어진 첫 관측 시각. positive가 지우며 UNRESOLVABLE_VIDEO 종료의 ended_at 하한이다.';
ALTER TABLE public.youtube_live_reconciliation_heads DROP CONSTRAINT IF EXISTS youtube_live_reconciliation_heads_end_reason_check;
ALTER TABLE public.youtube_live_reconciliation_heads DROP CONSTRAINT IF EXISTS chk_youtube_live_reconciliation_heads_end_reason_vocab;
ALTER TABLE public.youtube_live_reconciliation_heads ADD CONSTRAINT chk_youtube_live_reconciliation_heads_end_reason_vocab
    CHECK (end_reason = ANY (ARRAY['EXPLICIT_END', 'CANCELLED_BEFORE_LIVE', 'SCOPED_ABSENCE', 'UNRESOLVABLE_VIDEO'])) NOT VALID;
ALTER TABLE public.youtube_live_reconciliation_heads VALIDATE CONSTRAINT chk_youtube_live_reconciliation_heads_end_reason_vocab;
COMMIT;
