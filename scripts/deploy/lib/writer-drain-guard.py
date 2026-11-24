#!/usr/bin/env python3
import select
import subprocess
import sys
from pathlib import Path


GATE = Path(__file__).resolve().parents[3] / 'hololive/hololive-api/cmd/db-migrate/queries/bootstrap_writer_admission.sql'
SERVER = ('podman', 'exec', '-i', 'holo-postgres', 'env', '-u', 'PGSERVICE', '-u', 'PGSERVICEFILE', '-u', 'PGHOSTADDR',
          'PGCONNECT_TIMEOUT=5', 'PGSSLMODE=disable')
PSQL = (*SERVER, 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s '
        '-c idle_in_transaction_session_timeout=18min')
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


WRITERS = """ARRAY['hololive_runtime','hololive_alarm_worker','hololive_collector_a',
                  'hololive_collector_b','hololive_collector_c','hololive_collector_d']::name[]"""


def verify_writers_closed():
    sql = f"""BEGIN READ ONLY;
SELECT ({SETTINGS}) AND EXISTS (
 SELECT FROM pg_catalog.pg_event_trigger e JOIN pg_catalog.pg_proc p ON p.oid = e.evtfoid
 WHERE e.evtname = 'hololive_writer_admission' AND e.evtevent = 'login' AND e.evtenabled = 'A'
   AND p.oid = pg_catalog.to_regprocedure('hololive_writer_admission.reject_during_writer_drain()') AND p.prosecdef)
AND NOT EXISTS (
 SELECT FROM pg_catalog.pg_stat_activity a
 WHERE a.usename = ANY({WRITERS}) AND a.backend_type = 'client backend');
ROLLBACK;
"""
    probe = subprocess.run((*PSQL, 'PGAPPNAME=hololive-writers-check', *CLIENT), input=sql,
                           capture_output=True, text=True, timeout=38, check=True)
    if probe.stdout != 't\n':
        raise ValueError('업무 역할의 신규 로그인 차단 또는 기존 연결 0건을 확인하지 못함')


def install_writer_admission():
    subprocess.run((*SERVER, 'PGOPTIONS=-c statement_timeout=30s -c lock_timeout=3s', 'PGAPPNAME=hololive-writers-gate', *CLIENT),
                   input=GATE.read_text(encoding='utf-8'), capture_output=True, text=True, timeout=38, check=True)


def main():
    install_writer_admission()
    with subprocess.Popen((*PSQL, 'PGAPPNAME=hololive-writers-draining', *CLIENT), stdin=subprocess.PIPE,
                          stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, bufsize=1) as session:
        try:
            confirm(session, f"BEGIN READ ONLY; SELECT CASE WHEN {SETTINGS} THEN pg_try_advisory_xact_lock(0x484f4c4f,6432) ELSE false END;")
            verify_writers_closed()
            confirm(session, "SELECT set_config('application_name','hololive-writers-drained',false)='hololive-writers-drained';")
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
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        reason = str(exc) if isinstance(exc, ValueError) else type(exc).__name__
        print(f'[writer-drain] {reason}; 기존 hold를 보존하십시오', file=sys.stderr)
        sys.exit(1)
