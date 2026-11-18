#!/usr/bin/env python3
import csv
import io
import select
import subprocess
import sys


PSQL = ('podman', 'exec', '-i', 'holo-postgres', 'env', '-u', 'PGSERVICE', '-u', 'PGSERVICEFILE', '-u', 'PGHOSTADDR',
        'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s '
        '-c idle_in_transaction_session_timeout=18min', 'PGCONNECT_TIMEOUT=5', 'PGSSLMODE=disable')
CLIENT = ('psql', '-XqAtw', '-v', 'ON_ERROR_STOP=1', '-h', '/var/run/postgresql', '-p', '5432', '-U', 'postgres_admin', '-d', 'hololive')
SETTINGS = """current_database()='hololive' AND current_user='postgres_admin' AND session_user=current_user
AND inet_server_addr() IS NULL AND current_setting('default_transaction_read_only')='on'
AND current_setting('transaction_read_only')='on' AND current_setting('statement_timeout')='30s'
AND current_setting('lock_timeout')='3s'"""


def confirm(session, sql):
    session.stdin.write(sql + '\n')
    session.stdin.flush()
    if not select.select([session.stdout], [], [], 38)[0] or session.stdout.readline() != 't\n':
        raise ValueError('관리 소켓의 동일 세션 차단을 확인하지 못함')


def verify_frontends():
    # 단순 SHOW CLIENTS와 달리 아직 역할/DB가 정해지지 않은 로그인도 검사한다.
    census = subprocess.run(('podman', 'exec', 'holo-pgbouncer', 'psql', '-Xqw', '--csv', '-v', 'ON_ERROR_STOP=1',
                             '-h', '/tmp', '-p', '6432', '-U', 'pgbouncer', '-d', 'pgbouncer', '-c', 'SHOW SOCKETS'),
                            check=True, capture_output=True, text=True, timeout=15)
    rows = csv.DictReader(io.StringIO(census.stdout), strict=True)
    columns = rows.fieldnames or []
    if len(set(columns)) != len(columns) or not {'type', 'user', 'database', 'state'} <= set(columns):
        raise ValueError('풀러 소켓 관찰 형식을 확인하지 못함')
    control = 0
    for row in rows:
        if None in row or None in row.values():
            raise ValueError('풀러 소켓 관찰 불완전')
        if row['type'] == 'S':
            continue
        if (row['type'], row['user'], row['database'], row['state']) != ('C', 'pgbouncer', 'pgbouncer', 'cl_active'):
            raise ValueError('기존 업무 frontend 또는 미확정 로그인 연결이 있음')
        control += 1
    if not control:
        raise ValueError('관찰자 자신의 풀러 소켓을 확인하지 못함')


def verify_auth_closed():
    # verifier 자체는 조회 결과에 싣지 않는다. 이전 인증 함수가 남아 있으면 진행하지 않는다.
    sql = f"""BEGIN READ ONLY;
SELECT ({SETTINGS}) AND NOT EXISTS (
 SELECT FROM unnest(ARRAY['hololive_runtime','hololive_alarm_worker','hololive_collector_a',
                          'hololive_collector_b','hololive_collector_c','hololive_collector_d']) r(name)
 CROSS JOIN LATERAL hololive_pooler_auth.lookup(r.name));
ROLLBACK;
"""
    probe = subprocess.run((*PSQL, 'PGAPPNAME=hololive-pool-auth-check', *CLIENT), input=sql,
                           capture_output=True, text=True, timeout=38, check=True)
    if probe.stdout != 't\n':
        raise ValueError('여섯 역할의 신규 인증 차단이 확인되지 않음')


def main():
    with subprocess.Popen((*PSQL, 'PGAPPNAME=hololive-pool-draining', *CLIENT), stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, bufsize=1) as session:
        try:
            confirm(session, f"BEGIN READ ONLY; SELECT CASE WHEN {SETTINGS} THEN pg_try_advisory_xact_lock(0x484f4c4f,6432) ELSE false END;")
            verify_auth_closed()
            verify_frontends()
            confirm(session, "SELECT set_config('application_name','hololive-pool-drained',false)='hololive-pool-drained';")
            subprocess.run(('systemctl', 'start', 'hololive-db-migrate.service'), stdin=subprocess.DEVNULL, check=True, timeout=1020)
        finally:
            session.stdin.close()
            try:
                session.wait(timeout=8)
            except subprocess.TimeoutExpired:
                session.kill()
                session.wait()
                raise ValueError('관리 세션 종료 미확정') from None
    if session.returncode:
        raise ValueError('업무 창 관리 세션이 비정상 종료됨')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, csv.Error, OSError, subprocess.SubprocessError) as exc:
        reason = str(exc) if isinstance(exc, ValueError) else type(exc).__name__
        print(f'[card-window] {reason}; 기존 hold를 보존하십시오', file=sys.stderr)
        sys.exit(1)
