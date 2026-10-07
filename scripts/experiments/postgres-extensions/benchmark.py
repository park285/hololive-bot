#!/usr/bin/env python3
"""격리된 합성 DB에서 계측 조합의 비용을 비교합니다. 운영 접속 옵션은 없습니다."""

import argparse
import json
import math
import pathlib
import re
import statistics
import subprocess
import time
import uuid


ROOT = pathlib.Path(__file__).resolve().parent
WORKLOADS = ("discovery", "large_array", "lease_update")
VARIANTS = {
    "baseline": (),
    "kcache": ("pg_stat_kcache",),
    "wait_sampling": ("pg_wait_sampling",),
    "both": ("pg_stat_kcache", "pg_wait_sampling"),
}


def command(args, *, timeout=60, input_text=None):
    result = subprocess.run(args, input=input_text, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(f"command failed: {args[0]}: {result.stderr[-4000:]}")
    return result.stdout


def sql(container, statement):
    return command([
        "docker", "exec", container, "psql", "-X", "-U", "postgres", "-d", "postgres",
        "-v", "ON_ERROR_STOP=1", "-At", "-c", statement,
    ])


def wait_ready(container):
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        ready = subprocess.run(
            ["docker", "exec", container, "pg_isready", "-U", "postgres"],
            capture_output=True, timeout=5,
        )
        # entrypoint의 임시 서버를 지나 최종 PID 1 서버가 시작될 때까지 기다립니다.
        pid = command(["docker", "exec", container, "sh", "-c",
                       'if test -f "$PGDATA/postmaster.pid"; then head -1 "$PGDATA/postmaster.pid"; fi'])
        if ready.returncode == 0 and pid.strip() == "1":
            return
        time.sleep(0.2)
    raise RuntimeError("test database startup timed out")


def reset_statistics(container, extensions):
    statements = ["SELECT pg_stat_statements_reset()"]
    if "pg_stat_kcache" in extensions:
        statements.append("SELECT public.pg_stat_kcache_reset()")
    if "pg_wait_sampling" in extensions:
        statements.append("SELECT public.pg_wait_sampling_reset_profile()")
    sql(container, ";".join(statements))


def statistics_snapshot(container, extensions):
    result = {"kernel_cpu_seconds": None, "wait_samples": None}
    if "pg_stat_kcache" in extensions:
        result["kernel_cpu_seconds"] = float(sql(container, """
            SELECT COALESCE(sum(k.exec_user_time + k.exec_system_time), 0)
            FROM public.pg_stat_kcache() k
            JOIN public.pg_stat_statements s
              ON (k.queryid,k.dbid,k.userid,k.top)=(s.queryid,s.dbid,s.userid,s.toplevel)
            WHERE k.top AND s.query LIKE '%extension_benchmark.%'
        """).strip())
    if "pg_wait_sampling" in extensions:
        raw = sql(container, """
            SELECT COALESCE(jsonb_agg(entry), '[]'::jsonb) FROM (
                SELECT event_type,event,sum(count) AS samples
                FROM public.pg_wait_sampling_profile WHERE queryid<>0
                GROUP BY event_type,event ORDER BY sum(count) DESC
            ) entry
        """)
        result["wait_samples"] = json.loads(raw)
    return result


def run_workload(container, workload, seconds, extensions):
    base = ["docker", "exec", container, "pgbench", "-n", "-U", "postgres", "-d", "postgres",
            "-c", "4", "-j", "2", "-M", "prepared", "-f", f"/benchmark/{workload}.sql"]
    command(base + ["-T", "2"], timeout=15)
    reset_statistics(container, extensions)
    output = command(base + ["-T", str(seconds), "-l", "--log-prefix", f"/tmp/bench-{workload}"],
                     timeout=seconds + 30)
    logs = command(["docker", "exec", container, "sh", "-c", f"cat /tmp/bench-{workload}.*"])
    latencies = sorted(int(line.split()[2]) / 1000 for line in logs.splitlines())
    if not latencies:
        raise RuntimeError("pgbench produced no transaction latency samples")
    tps = re.search(r"^tps = ([0-9.]+)", output, re.MULTILINE)
    failures = re.search(r"^number of failed transactions: ([0-9]+)", output, re.MULTILINE)
    if not tps or not failures or int(failures[1]):
        raise RuntimeError(f"pgbench did not complete successfully: {output}")
    percentile = lambda p: latencies[max(0, math.ceil(len(latencies) * p) - 1)]
    return {
        "workload": workload, "transactions": len(latencies), "tps": float(tps[1]),
        "mean_ms": statistics.mean(latencies), "p95_ms": percentile(0.95), "p99_ms": percentile(0.99),
        **statistics_snapshot(container, extensions),
    }


def run_variant(image, variant, seconds, trial):
    container = f"hololive-pg-extension-bench-{uuid.uuid4().hex[:12]}"
    extensions = VARIANTS[variant]
    libraries = ",".join(("pg_stat_statements",) + extensions)
    args = [
        "docker", "run", "-d", "--rm", "--name", container, "--network", "none", "--read-only",
        "--cpus", "2", "--memory", "1536m", "--pids-limit", "256", "--user", "999:999",
        "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
        "--tmpfs", "/var/lib/postgresql:rw,uid=999,gid=999,mode=0755",
        "--tmpfs", "/var/run/postgresql:rw,uid=999,gid=999,mode=0755",
        "--tmpfs", "/tmp:rw,mode=1777,size=128m",
        "--mount", f"type=bind,src={ROOT},dst=/benchmark,readonly",
        "-e", "PGDATA=/var/lib/postgresql/testdata", "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
        image, "postgres", "-c", "listen_addresses=", "-c", f"shared_preload_libraries={libraries}",
        "-c", "compute_query_id=on", "-c", "pg_stat_statements.track_utility=off",
        "-c", "shared_buffers=128MB", "-c", "max_wal_size=256MB", "-c", "min_wal_size=64MB",
    ]
    if "pg_wait_sampling" in extensions:
        args += ["-c", "pg_wait_sampling.profile_pid=off", "-c", "pg_wait_sampling.sample_cpu=off",
                 "-c", "pg_wait_sampling.profile_period=20", "-c", "pg_wait_sampling.history_period=100",
                 "-c", "pg_wait_sampling.history_size=20000"]
    started = time.monotonic()
    command(args)
    try:
        wait_ready(container)
        startup_seconds = time.monotonic() - started
        sql(container, "CREATE EXTENSION pg_stat_statements")
        for extension in extensions:
            sql(container, f"CREATE EXTENSION {extension}")
        command(["docker", "exec", container, "psql", "-X", "-U", "postgres", "-d", "postgres",
                 "-v", "ON_ERROR_STOP=1", "-f", "/benchmark/fixture.sql"], timeout=120)
        results = []
        for workload in WORKLOADS:
            result = {"variant": variant, "trial": trial, "startup_seconds": startup_seconds,
                      **run_workload(container, workload, seconds, extensions)}
            results.append(result)
            print(json.dumps(result), flush=True)
        return results
    except Exception:
        logs = subprocess.run(["docker", "logs", "--tail", "40", container],
                              capture_output=True, text=True, timeout=10)
        print(logs.stderr, flush=True)
        raise
    finally:
        subprocess.run(["docker", "rm", "-f", container],
                       capture_output=True, timeout=30, check=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--seconds", type=int, default=8)
    args = parser.parse_args()
    if not 2 <= args.seconds <= 60:
        parser.error("측정 시간은 2~60초여야 합니다")
    results = []
    # 정방향·역방향 순서로 두 번 측정하여 순서에 따른 온도·부하 차이를 줄입니다.
    for trial, variants in enumerate((tuple(VARIANTS), tuple(reversed(VARIANTS))), 1):
        for variant in variants:
            results.extend(run_variant(args.image, variant, args.seconds, trial))
            args.output.write_text(json.dumps(results, indent=2) + "\n")


if __name__ == "__main__":
    main()
