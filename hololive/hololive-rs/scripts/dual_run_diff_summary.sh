#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT_DIR="${ROOT_DIR}/docs/reports/dual_run_snapshots"
WINDOW_DAYS="${1:-3}"
REQUIRE_FULL_WINDOW="${REQUIRE_FULL_WINDOW:-1}"
REQUIRE_UNIQUE_DAYS="${REQUIRE_UNIQUE_DAYS:-1}"

if ! [[ "${WINDOW_DAYS}" =~ ^[0-9]+$ ]] || [[ "${WINDOW_DAYS}" -lt 1 ]]; then
  echo "usage: $0 [window_days>=1]" >&2
  exit 1
fi

if [[ ! "${REQUIRE_FULL_WINDOW}" =~ ^[01]$ ]]; then
  echo "REQUIRE_FULL_WINDOW must be 0 or 1 (got: ${REQUIRE_FULL_WINDOW})" >&2
  exit 1
fi

if [[ ! "${REQUIRE_UNIQUE_DAYS}" =~ ^[01]$ ]]; then
  echo "REQUIRE_UNIQUE_DAYS must be 0 or 1 (got: ${REQUIRE_UNIQUE_DAYS})" >&2
  exit 1
fi

if [[ ! -d "${REPORT_DIR}" ]]; then
  echo "[dual-run-summary] report directory not found: ${REPORT_DIR}" >&2
  exit 1
fi

STAMP="$(date -u +%Y%m%d_%H%M%S)"
OUT_JSON="${REPORT_DIR}/dual_run_diff_summary_${WINDOW_DAYS}d_${STAMP}.json"
OUT_MD="${REPORT_DIR}/dual_run_diff_summary_${WINDOW_DAYS}d_${STAMP}.md"

set +e
python3 - <<'PY' "${REPORT_DIR}" "${WINDOW_DAYS}" "${OUT_JSON}" "${OUT_MD}" "${REQUIRE_FULL_WINDOW}" "${REQUIRE_UNIQUE_DAYS}"
import glob
import json
import os
import sys
from datetime import datetime, timezone

report_dir = sys.argv[1]
window_days = int(sys.argv[2])
out_json = sys.argv[3]
out_md = sys.argv[4]
require_full_window = sys.argv[5] == "1"
require_unique_days = sys.argv[6] == "1"

json_paths = sorted(glob.glob(os.path.join(report_dir, "major_events_snapshot_diff_*.json")))
if not json_paths:
    raise SystemExit("no diff json reports found")

def parse_generated_at(value: str, path: str):
    if value:
        try:
            return datetime.fromisoformat(value.replace("Z", "+00:00"))
        except ValueError:
            pass

    base = os.path.basename(path)
    stamp = base.rsplit("_", 2)[-2:]
    joined = "_".join(stamp).replace(".json", "")
    try:
        return datetime.strptime(joined, "%Y%m%d_%H%M%S").replace(tzinfo=timezone.utc)
    except ValueError:
        return datetime.fromtimestamp(0, timezone.utc)


reports = []
for path in json_paths:
    with open(path, encoding="utf-8") as fp:
        payload = json.load(fp)
    generated_at = payload.get("generated_at", "")
    reports.append({
        "path": path,
        "generated_at": generated_at,
        "mismatch_count": int(payload.get("mismatch_count", 0)),
        "label": payload.get("label", ""),
        "_sort_key": parse_generated_at(generated_at, path),
    })

reports.sort(key=lambda item: item["_sort_key"])
selected = reports[-window_days:]
all_zero = all(item["mismatch_count"] == 0 for item in selected)
window_satisfied = len(selected) >= window_days
unique_days = sorted({item["_sort_key"].astimezone(timezone.utc).date().isoformat() for item in selected})
unique_days_satisfied = len(unique_days) >= window_days

for item in selected:
    item.pop("_sort_key", None)

summary = {
    "generated_at": datetime.now(timezone.utc).isoformat(),
    "window_days": window_days,
    "selected_count": len(selected),
    "window_satisfied": window_satisfied,
    "missing_reports": max(window_days - len(selected), 0),
    "require_full_window": require_full_window,
    "unique_days": unique_days,
    "unique_day_count": len(unique_days),
    "unique_days_satisfied": unique_days_satisfied,
    "require_unique_days": require_unique_days,
    "all_zero": all_zero,
    "selected_reports": selected,
}

with open(out_json, "w", encoding="utf-8") as fp:
    json.dump(summary, fp, ensure_ascii=False, indent=2)

lines = [
    f"# Dual-run Diff Summary ({window_days}d)",
    "",
    f"- GeneratedAt(UTC): {summary['generated_at']}",
    f"- SelectedReports: {summary['selected_count']}",
    f"- WindowSatisfied: {summary['window_satisfied']}",
    f"- MissingReports: {summary['missing_reports']}",
    f"- UniqueDayCount: {summary['unique_day_count']}",
    f"- UniqueDaysSatisfied: {summary['unique_days_satisfied']}",
    f"- AllZeroMismatch: {summary['all_zero']}",
    "",
    "## Unique Days",
    "",
    f"- {', '.join(summary['unique_days']) if summary['unique_days'] else '(none)'}",
    "",
    "## Reports",
    "",
]
for item in selected:
    lines.append(
        f"- {item['generated_at']} | mismatch={item['mismatch_count']} | {os.path.basename(item['path'])}"
    )

with open(out_md, "w", encoding="utf-8") as fp:
    fp.write("\n".join(lines) + "\n")

if require_full_window and not window_satisfied:
    raise SystemExit(2)

if require_unique_days and not unique_days_satisfied:
    raise SystemExit(3)

if not all_zero:
    raise SystemExit(1)
PY
STATUS=$?
set -e
echo "[dual-run-summary] summary_json=${OUT_JSON}"
echo "[dual-run-summary] summary_md=${OUT_MD}"

case "${STATUS}" in
  0)
    echo "[dual-run-summary] validation=pass"
    ;;
  1)
    echo "[dual-run-summary] validation=fail (mismatch detected)" >&2
    ;;
  2)
    echo "[dual-run-summary] validation=fail (insufficient report count for window)" >&2
    ;;
  3)
    echo "[dual-run-summary] validation=fail (insufficient unique days for window)" >&2
    ;;
  *)
    echo "[dual-run-summary] validation=fail (unexpected exit code: ${STATUS})" >&2
    ;;
esac

exit "${STATUS}"
