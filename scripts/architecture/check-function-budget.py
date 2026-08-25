#!/usr/bin/env python3
"""Compatibility delegate for the canonical Hololive structure analyzer."""

from __future__ import annotations

import argparse
import subprocess
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".")
    parser.add_argument("--report-over-budget", action="store_true")
    parser.add_argument("--include-prefix", action="append", default=[])
    parser.add_argument("--output", choices=("text", "json"), default="text")
    parser.add_argument(
        "--sort-by",
        choices=("path", "score", "lines", "complexity", "nesting"),
        default="path",
    )
    parser.add_argument("--limit", type=int, default=0)
    args = parser.parse_args()

    script_dir = Path(__file__).resolve().parent
    command = [
        "python3",
        str(script_dir / "check-structure-budget.py"),
        "--root",
        str(Path(args.root).resolve()),
        "--mode",
        "advisory" if args.report_over_budget else "hard",
        "--format",
        args.output,
        "--component",
        "functions",
    ]
    for prefix in args.include_prefix:
        command.extend(("--include-prefix", prefix))
    # sort-by and limit remain accepted for CLI compatibility. Canonical output is
    # stable-ID sorted and intentionally complete so hard-gate evidence is not truncated.
    return subprocess.run(command, check=False).returncode


if __name__ == "__main__":
    raise SystemExit(main())
