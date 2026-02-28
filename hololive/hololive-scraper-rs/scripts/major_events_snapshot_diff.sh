#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "usage: $0 <old_snapshot.tsv> <new_snapshot.tsv> [label]" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OLD_PATH="$1"
NEW_PATH="$2"
LABEL_RAW="${3:-manual}"
FAIL_ON_DIFF="${FAIL_ON_DIFF:-0}"

for path in "${OLD_PATH}" "${NEW_PATH}"; do
  if [[ ! -f "${path}" ]]; then
    echo "[snapshot-diff] file not found: ${path}" >&2
    exit 1
  fi
  if [[ ! -r "${path}" ]]; then
    echo "[snapshot-diff] file not readable: ${path}" >&2
    exit 1
  fi
done

SAFE_LABEL="$(printf '%s' "${LABEL_RAW}" | tr -cs 'A-Za-z0-9._-' '-' | sed 's/^-*//;s/-*$//')"
if [[ -z "${SAFE_LABEL}" ]]; then
  SAFE_LABEL="manual"
fi

STAMP="$(date -u +%Y%m%d_%H%M%S)"
REPORT_DIR="${ROOT_DIR}/docs/reports/dual_run_snapshots"
mkdir -p "${REPORT_DIR}"

PREFIX="major_events_snapshot_diff_${SAFE_LABEL}_${STAMP}"
REPORT_JSON="${REPORT_DIR}/${PREFIX}.json"
REPORT_MD="${REPORT_DIR}/${PREFIX}.md"

python3 - <<'PY' "${OLD_PATH}" "${NEW_PATH}" "${REPORT_JSON}" "${REPORT_MD}" "${SAFE_LABEL}"
import csv
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

old_path, new_path, report_json, report_md, label = sys.argv[1:6]

compare_fields = ["event_start_date", "event_end_date", "status"]


def load_snapshot(path: str):
    rows = {}
    with open(path, encoding="utf-8") as fp:
        reader = csv.DictReader(fp, delimiter="\t")
        expected = {
            "external_id",
            "type",
            "event_start_date",
            "event_end_date",
            "status",
            "updated_at",
        }
        missing = expected - set(reader.fieldnames or [])
        if missing:
            raise RuntimeError(f"snapshot {path} missing columns: {sorted(missing)}")

        for row in reader:
            external_id = (row.get("external_id") or "").strip()
            event_type = (row.get("type") or "").strip()
            key = f"{event_type}::{external_id}"
            rows[key] = row

    return rows


old_rows = load_snapshot(old_path)
new_rows = load_snapshot(new_path)
all_keys = sorted(set(old_rows) | set(new_rows))

added = []
removed = []
changed = []

for key in all_keys:
    old_row = old_rows.get(key)
    new_row = new_rows.get(key)

    if old_row is None:
        added.append({
            "key": key,
            "external_id": new_row.get("external_id", ""),
            "type": new_row.get("type", ""),
            "event_start_date": new_row.get("event_start_date", ""),
            "event_end_date": new_row.get("event_end_date", ""),
            "status": new_row.get("status", ""),
        })
        continue

    if new_row is None:
        removed.append({
            "key": key,
            "external_id": old_row.get("external_id", ""),
            "type": old_row.get("type", ""),
            "event_start_date": old_row.get("event_start_date", ""),
            "event_end_date": old_row.get("event_end_date", ""),
            "status": old_row.get("status", ""),
        })
        continue

    field_diff = {}
    for field in compare_fields:
        old_value = (old_row.get(field) or "").strip()
        new_value = (new_row.get(field) or "").strip()
        if old_value != new_value:
            field_diff[field] = {"old": old_value, "new": new_value}

    if field_diff:
        changed.append(
            {
                "key": key,
                "external_id": new_row.get("external_id", ""),
                "type": new_row.get("type", ""),
                "diff": field_diff,
            }
        )

summary = {
    "label": label,
    "generated_at": datetime.now(timezone.utc).isoformat(),
    "old_snapshot": str(Path(old_path).resolve()),
    "new_snapshot": str(Path(new_path).resolve()),
    "old_count": len(old_rows),
    "new_count": len(new_rows),
    "added_count": len(added),
    "removed_count": len(removed),
    "changed_count": len(changed),
    "mismatch_count": len(added) + len(removed) + len(changed),
    "added": added,
    "removed": removed,
    "changed": changed,
}

with open(report_json, "w", encoding="utf-8") as fp:
    json.dump(summary, fp, ensure_ascii=False, indent=2)

lines = [
    f"# major_events Snapshot Diff ({label})",
    "",
    f"- GeneratedAt(UTC): {summary['generated_at']}",
    f"- OldSnapshot: `{summary['old_snapshot']}`",
    f"- NewSnapshot: `{summary['new_snapshot']}`",
    f"- OldCount: {summary['old_count']}",
    f"- NewCount: {summary['new_count']}",
    f"- Added: {summary['added_count']}",
    f"- Removed: {summary['removed_count']}",
    f"- Changed: {summary['changed_count']}",
    f"- MismatchTotal: {summary['mismatch_count']}",
    "",
]

if summary["mismatch_count"] == 0:
    lines.extend(["## Result", "", "No differences detected."])
else:
    if added:
        lines.extend(["## Added", ""])
        for row in added[:30]:
            lines.append(f"- {row['type']} | {row['external_id']} | {row['event_start_date']} ~ {row['event_end_date']} | {row['status']}")
        if len(added) > 30:
            lines.append(f"- ... and {len(added) - 30} more")
        lines.append("")

    if removed:
        lines.extend(["## Removed", ""])
        for row in removed[:30]:
            lines.append(f"- {row['type']} | {row['external_id']} | {row['event_start_date']} ~ {row['event_end_date']} | {row['status']}")
        if len(removed) > 30:
            lines.append(f"- ... and {len(removed) - 30} more")
        lines.append("")

    if changed:
        lines.extend(["## Changed", ""])
        for row in changed[:50]:
            lines.append(f"- {row['type']} | {row['external_id']}")
            for field, values in row["diff"].items():
                lines.append(f"  - {field}: `{values['old']}` -> `{values['new']}`")
        if len(changed) > 50:
            lines.append(f"- ... and {len(changed) - 50} more")

with open(report_md, "w", encoding="utf-8") as fp:
    fp.write("\n".join(lines) + "\n")
PY

echo "[snapshot-diff] report_json=${REPORT_JSON}"
echo "[snapshot-diff] report_md=${REPORT_MD}"

MISMATCH_COUNT="$(python3 - <<'PY' "${REPORT_JSON}"
import json
import sys

with open(sys.argv[1], encoding='utf-8') as fp:
    payload = json.load(fp)
print(payload.get('mismatch_count', 0))
PY
)"

echo "[snapshot-diff] mismatch_count=${MISMATCH_COUNT}"

if [[ "${FAIL_ON_DIFF}" == "1" && "${MISMATCH_COUNT}" != "0" ]]; then
  echo "[snapshot-diff] FAIL_ON_DIFF=1 and mismatch detected" >&2
  exit 1
fi
