#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from collections import Counter, defaultdict
from pathlib import Path

RISK_BY_PATH_PREFIX = [
    ("hololive/hololive-dispatcher-go/internal/app/runtime", "R5"),
    ("hololive/hololive-alarm-worker/internal/service/alarm/scheduler", "R5"),
    ("hololive/hololive-kakao-bot-go/internal/bot", "R5"),
    ("hololive/hololive-shared/pkg/service/alarm/dispatchoutbox", "R4"),
    ("hololive/hololive-shared/pkg/service/alarm/queue", "R4"),
    ("hololive/hololive-shared/pkg/service/youtube/outbox", "R4"),
    ("hololive/hololive-shared/pkg/service/auth", "R4"),
    ("hololive/hololive-shared/pkg/service/cache", "R4"),
    ("hololive/hololive-shared/pkg/domain/template_sample", "R0"),
]


def module_of(path: str) -> str:
    parts = path.split("/")
    if path.startswith("shared-go/"):
        return "shared-go"
    if path.startswith("hololive/") and len(parts) >= 2:
        return "/".join(parts[:2])
    return parts[0]


def package_of(path: str) -> str:
    return path.rsplit("/", 1)[0]


def risk_of(path: str) -> str:
    for prefix, risk in RISK_BY_PATH_PREFIX:
        if path.startswith(prefix):
            return risk
    if "/runtime" in path or "lifecycle" in path or "scheduler" in path:
        return "R5"
    if "queue" in path or "outbox" in path or "repository" in path or "cache" in path:
        return "R4"
    if "handler" in path or "router" in path or "middleware" in path:
        return "R2"
    if "formatter" in path or "parser" in path:
        return "R1"
    return "R3"


def chunk_size_for_risk(risk: str) -> int:
    if risk == "R5":
        return 1
    if risk == "R4":
        return 2
    if risk in {"R2", "R3"}:
        return 3
    return 5


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--output-dir", required=True)
    args = parser.parse_args()

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    payload = json.loads(Path(args.input).read_text(encoding="utf-8"))
    items = payload["items"] if isinstance(payload, dict) and "items" in payload else payload

    by_module = Counter(module_of(item["path"]) for item in items)
    by_package = Counter(package_of(item["path"]) for item in items)
    by_file: dict[str, list[dict[str, object]]] = defaultdict(list)
    for item in items:
        by_file[str(item["path"])].append(item)

    (output_dir / "summary-by-module.tsv").write_text(
        "module\tover_budget\n" + "".join(f"{k}\t{v}\n" for k, v in by_module.most_common()),
        encoding="utf-8",
    )
    (output_dir / "summary-by-package.tsv").write_text(
        "package\tover_budget\n" + "".join(f"{k}\t{v}\n" for k, v in by_package.most_common()),
        encoding="utf-8",
    )
    (output_dir / "summary-by-file.tsv").write_text(
        "file\tover_budget\trisk\n" + "".join(
            f"{path}\t{len(functions)}\t{risk_of(path)}\n"
            for path, functions in sorted(by_file.items())
        ),
        encoding="utf-8",
    )

    rows = []
    cards = []
    for path, functions in sorted(by_file.items()):
        risk = risk_of(path)
        size = chunk_size_for_risk(risk)
        functions = sorted(functions, key=lambda item: (-int(item.get("score", 0)), int(item["line"]), str(item["name"])))
        for index in range(0, len(functions), size):
            chunk = functions[index:index + size]
            shard_id = f"AUTO-{len(rows)+1:03d}"
            names = ",".join(f'{item["name"]}@{item["line"]}' for item in chunk)
            max_score = max(int(item.get("score", 0)) for item in chunk)
            rows.append((shard_id, risk, path, names, str(len(chunk)), str(max_score), "open", ""))
            cards.append(f"""### {shard_id} — {path}

- risk: {risk}
- 대상 함수: `{names}`
- 개수: {len(chunk)}
- max_score: {max_score}
- 작업 지시: 이 파일과 같은 package의 테스트를 먼저 확인하고, 대상 함수만 기본 gate 이하로 낮추십시오. 외부 동작, public API, status code, DB/cache/queue key는 유지하십시오.
- 검증:
  - `python3 scripts/architecture/check-function-budget.py --root . --report-over-budget --include-prefix {path} --output text --sort-by score --limit 20`
  - 해당 package `go test`
""")

    (output_dir / "shard-ledger.tsv").write_text(
        "shard_id\trisk\tpath\tfunctions\tcount\tmax_score\tstatus\tnotes\n"
        + "".join("\t".join(row) + "\n" for row in rows),
        encoding="utf-8",
    )
    (output_dir / "auto-shard-cards.md").write_text("\n".join(cards), encoding="utf-8")

    print(f"over_budget={len(items)}")
    print(f"modules={len(by_module)} packages={len(by_package)} files={len(by_file)} shards={len(rows)}")
    print(f"wrote {output_dir / 'shard-ledger.tsv'}")
    print(f"wrote {output_dir / 'auto-shard-cards.md'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
