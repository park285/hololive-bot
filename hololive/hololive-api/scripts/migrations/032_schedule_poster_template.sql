BEGIN;

CREATE TABLE IF NOT EXISTS public.member_schedule_card_templates (
    member_id integer PRIMARY KEY REFERENCES public.members(id) ON DELETE CASCADE,
    template text NOT NULL CHECK (template IN ('classic', 'orbit'))
);

-- 설정이 없는 멤버는 기존 양식을 쓴다. 재실행 시 운영자가 바꾼 선택을 덮지 않는다.
INSERT INTO public.member_schedule_card_templates (member_id, template)
SELECT id, 'orbit' FROM public.members WHERE slug = 'sorashina-sopia'
ON CONFLICT (member_id) DO NOTHING;

COMMIT;
