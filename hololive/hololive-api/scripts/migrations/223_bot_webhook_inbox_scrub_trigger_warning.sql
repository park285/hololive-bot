-- bot_webhook_inbox terminal payload scrub trigger는 terminal 전이에서 payload를 직접 비우지 않던 이전 runtime writer용
-- 호환층이다. 현재 terminal writer(inbox_complete·inbox_abandon·inbox_release·inbox_reclaim_expired)는 같은 UPDATE에서
-- payload를 '{}'로 쓰므로, trigger가 비어 있지 않은 payload를 만나면 이전 writer가 돌고 있다는 뜻이다. 조용히 고치지 않고
-- WARNING으로 드러낸 뒤 scrub한다(PLN-20260926-stack-audit-refactoring T17). 메시지 본문과 식별자는 로그에 남기지 않는다.
-- 번호는 운영 적용된 live-evidence 218~220과 부재 증거 보존 221 다음(222 뒤)이다. 제거 조건은 docs/current/runbooks/hololive-api.md의 scrub trigger 항목이 소유한다.
CREATE OR REPLACE FUNCTION public.scrub_bot_webhook_inbox_terminal_payload() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.payload IS DISTINCT FROM '{}'::jsonb THEN
        RAISE WARNING 'bot_webhook_inbox terminal payload was scrubbed by the compatibility trigger; a writer that does not clear payload is running';
    END IF;
    NEW.payload := '{}'::jsonb;
    RETURN NEW;
END
$$;
