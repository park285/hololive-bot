import json
import math
import os
import re
import subprocess
import tempfile
from pathlib import Path


class CompareNumericError(Exception):
    pass


def parse_bench_ns(text: str, name: str) -> float | None:
    values = []
    for line in text.splitlines():
        if not line.startswith(name):
            continue
        match = re.search(r"\s(\d+(?:\.\d+)?)\s+ns/op", line)
        if match:
            value = float(match.group(1))
            if not math.isfinite(value) or value <= 0:
                raise CompareNumericError(
                    f"benchmark {name} ns/op must be a positive finite number"
                )
            values.append(value)
    if not values:
        return None
    mean = sum(values) / len(values)
    if not math.isfinite(mean):
        raise CompareNumericError(f"benchmark {name} mean ns/op overflowed")
    return mean


def collect_live_metrics(live_cmd: str | None, repo_root: Path, fields: tuple[str, ...]) -> dict:
    metrics = {field: None for field in fields}
    if not live_cmd:
        return metrics
    with tempfile.NamedTemporaryFile("w+", suffix=".json", delete=False) as handle:
        live_path = Path(handle.name)
    try:
        env = os.environ.copy()
        env["PGO_LIVE_OUTPUT"] = str(live_path)
        subprocess.run(
            ["bash", "-c", live_cmd],
            cwd=repo_root,
            env=env,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )

        def reject_constant(value: str):
            raise CompareNumericError(f"live metrics contain invalid numeric constant: {value}")

        try:
            data = json.loads(
                live_path.read_text(encoding="utf-8"),
                parse_constant=reject_constant,
            )
        except json.JSONDecodeError as exc:
            raise CompareNumericError(f"live metrics json invalid: {exc}") from exc
    finally:
        live_path.unlink(missing_ok=True)
    if not isinstance(data, dict):
        raise CompareNumericError("live metrics must be a JSON object")
    for field in fields:
        value = data.get(field)
        if value is None:
            continue
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            raise CompareNumericError(f"live metric {field} must be a number or null")
        try:
            numeric = float(value)
        except (OverflowError, ValueError) as exc:
            raise CompareNumericError(f"live metric {field} must be finite or null") from exc
        if not math.isfinite(numeric):
            raise CompareNumericError(f"live metric {field} must be finite or null")
        metrics[field] = numeric
    return metrics
