-- 생성 열의 decoder는 영수증 INSERT와 같은 invoker 권한으로 실행된다.
-- 이미 허가된 기록 역할에만 이 의존 권한을 보충하고 기록·표·역할 구성·PUBLIC 권한은 바꾸지 않는다.
DO $roles$
DECLARE
    recording_role text;
BEGIN
    FOR recording_role IN
        SELECT DISTINCT role.rolname
        FROM pg_catalog.pg_proc procedure
        CROSS JOIN LATERAL pg_catalog.aclexplode(COALESCE(procedure.proacl,pg_catalog.acldefault('f',procedure.proowner))) privilege
        JOIN pg_catalog.pg_roles role ON role.oid=privilege.grantee
        WHERE procedure.oid='public.record_youtube_live_review(uuid,text,text,text,text)'::regprocedure
          AND privilege.privilege_type='EXECUTE'
    LOOP
        EXECUTE format('GRANT EXECUTE ON FUNCTION public.youtube_live_review_receipt_value(jsonb) TO %I',recording_role);
    END LOOP;
END
$roles$;
