#!/usr/bin/env python3
"""공식 pg_jsonschema GNU 바이너리의 aliases 계약과 서버 실행 비용을 비교합니다."""

import argparse
import json
import pathlib
import subprocess
import uuid

from benchmark import ROOT, command, sql, wait_ready


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    args = parser.parse_args()
    container = f"hololive-pg-jsonschema-bench-{uuid.uuid4().hex[:12]}"
    command([
        "docker", "run", "-d", "--rm", "--name", container, "--network", "none", "--read-only",
        "--cpus", "2", "--memory", "512m", "--pids-limit", "128", "--user", "999:999",
        "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
        "--tmpfs", "/var/lib/postgresql:rw,uid=999,gid=999,mode=0755",
        "--tmpfs", "/var/run/postgresql:rw,uid=999,gid=999,mode=0755",
        "--tmpfs", "/tmp:rw,mode=1777,size=64m",
        "--mount", f"type=bind,src={ROOT},dst=/benchmark,readonly",
        "-e", "PGDATA=/var/lib/postgresql/testdata", "-e", "POSTGRES_HOST_AUTH_METHOD=trust",
        args.image, "postgres", "-c", "listen_addresses=",
    ])
    try:
        wait_ready(container)
        print(command(["docker", "exec", container, "psql", "-X", "-U", "postgres", "-d", "postgres",
                       "-v", "ON_ERROR_STOP=1", "-f", "/benchmark/testqueries/jsonschema.sql"]))
        results = []
        for trial in range(4):
            for validator in (("builtin", "schema") if trial % 2 == 0 else ("schema", "builtin")):
                raw = sql(container, f"""
                    EXPLAIN (ANALYZE, BUFFERS, TIMING OFF, FORMAT JSON)
                    SELECT count(*) FROM extension_benchmark.alias_samples
                    WHERE extension_benchmark.aliases_{validator}(value)
                """)
                plan = json.loads(raw)[0]
                result = {"validator": validator, "trial": trial + 1, "rows": 20000,
                          "execution_ms": plan["Execution Time"], "planning_ms": plan["Planning Time"]}
                results.append(result)
                print(json.dumps(result), flush=True)
        args.output.write_text(json.dumps(results, indent=2) + "\n")
    except Exception:
        logs = subprocess.run(["docker", "logs", "--tail", "30", container],
                              capture_output=True, text=True, timeout=10)
        print(logs.stderr, flush=True)
        raise
    finally:
        subprocess.run(["docker", "rm", "-f", container],
                       capture_output=True, timeout=30, check=False)


if __name__ == "__main__":
    main()
