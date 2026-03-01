#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_ROOT="$(cd "${ROOT_DIR}/../.." && pwd)"
TARGET_COUNT="${TARGET_COUNT:-100}"

if ! [[ "${TARGET_COUNT}" =~ ^[0-9]+$ ]] || [[ "${TARGET_COUNT}" -lt 1 ]]; then
  echo "[cross-validate-live] TARGET_COUNT must be a positive integer" >&2
  exit 1
fi

REPORT_DIR="${ROOT_DIR}/docs/reports"
mkdir -p "${REPORT_DIR}"

RUN_STAMP="$(date -u +%Y%m%d_%H%M%S)"
RUN_PREFIX="date_extractor_cross_validation_live_${TARGET_COUNT}_${RUN_STAMP}"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/${RUN_PREFIX}.XXXXXX")"

RAW_FIXTURE="${TMP_DIR}/fixture_raw.json"
EXPECTED_FIXTURE="${TMP_DIR}/fixture_expected.json"
GO_RESULTS="${TMP_DIR}/go_results.json"
RUST_RESULTS="${TMP_DIR}/rust_results.json"
REPORT_JSON="${REPORT_DIR}/${RUN_PREFIX}.json"
REPORT_MD="${REPORT_DIR}/${RUN_PREFIX}.md"
RAW_FIXTURE_ARTIFACT="${REPORT_DIR}/${RUN_PREFIX}.fixture_raw.json"
EXPECTED_FIXTURE_ARTIFACT="${REPORT_DIR}/${RUN_PREFIX}.fixture_expected.json"
GO_RESULTS_ARTIFACT="${REPORT_DIR}/${RUN_PREFIX}.go_results.json"
RUST_RESULTS_ARTIFACT="${REPORT_DIR}/${RUN_PREFIX}.rust_results.json"

echo "[cross-validate-live] target_count=${TARGET_COUNT}"
echo "[cross-validate-live] tmp_dir=${TMP_DIR}"

python3 - <<'PY' "${TARGET_COUNT}" "${RAW_FIXTURE}"
import json
import re
import sys
from datetime import datetime, timezone
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen
import xml.etree.ElementTree as ET

TARGET_COUNT = int(sys.argv[1])
OUTPUT_PATH = sys.argv[2]

FEEDS = [
    ("event", "https://hololive.hololivepro.com/events/feed/"),
    ("news_ja", "https://hololive.hololivepro.com/news/feed/"),
    ("news_en", "https://hololive.hololivepro.com/en/news/feed/"),
]

UA = (
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
    "AppleWebKit/537.36 (KHTML, like Gecko) "
    "Chrome/133.0.0.0 Safari/537.36"
)


def sanitize(text: str) -> str:
    cleaned = re.sub(r"[^a-zA-Z0-9]+", "-", text).strip("-").lower()
    return cleaned or "untitled"


def item_html(item):
    encoded = item.find("{http://purl.org/rss/1.0/modules/content/}encoded")
    if encoded is not None and encoded.text:
        return encoded.text

    desc = item.find("description")
    if desc is not None and desc.text:
        return desc.text

    return ""


def build_case_name(feed: str, page: int, idx: int, title: str) -> str:
    title_slug = sanitize(title)[:48]
    return f"{feed}_p{page:02d}_i{idx:02d}_{title_slug}"


cases = []
seen_names = set()

for feed_name, base_url in FEEDS:
    page = 1
    while len(cases) < TARGET_COUNT:
        page_url = base_url if page == 1 else f"{base_url}?paged={page}"
        req = Request(page_url, headers={"User-Agent": UA})
        try:
            with urlopen(req, timeout=15) as resp:
                if resp.status != 200:
                    break
                payload = resp.read()
        except HTTPError as err:
            if err.code in (404, 410):
                break
            raise
        except URLError as err:
            raise RuntimeError(f"fetch failed for {page_url}: {err}") from err

        try:
            root = ET.fromstring(payload)
        except ET.ParseError:
            break

        items = root.findall("./channel/item")
        if not items:
            break

        for idx, item in enumerate(items, start=1):
            if len(cases) >= TARGET_COUNT:
                break

            title = (item.findtext("title") or "untitled").strip()
            case_name = build_case_name(feed_name, page, idx, title)
            if case_name in seen_names:
                suffix = 2
                while f"{case_name}-{suffix}" in seen_names:
                    suffix += 1
                case_name = f"{case_name}-{suffix}"

            seen_names.add(case_name)
            cases.append(
                {
                    "name": case_name,
                    "input_html": item_html(item),
                    "expected_dates": [],
                }
            )

        page += 1

if len(cases) < TARGET_COUNT:
    raise RuntimeError(f"insufficient cases collected: {len(cases)} < {TARGET_COUNT}")

fixture = {
    "schema_version": 1,
    "generated_from": f"scripts/cross_validate_live_100.sh ({datetime.now(timezone.utc).isoformat()})",
    "cases": cases,
}

with open(OUTPUT_PATH, "w", encoding="utf-8") as fp:
    json.dump(fixture, fp, ensure_ascii=False, indent=2)
PY

pushd "${MONOREPO_ROOT}/hololive/hololive-kakao-bot-go" >/dev/null
go run ./cmd/tools/date_extractor_cross_validate \
  -fixture "${RAW_FIXTURE}" \
  -output "${GO_RESULTS}"
popd >/dev/null

python3 - <<'PY' "${RAW_FIXTURE}" "${GO_RESULTS}" "${EXPECTED_FIXTURE}"
import json
import sys

raw_fixture_path, go_result_path, out_path = sys.argv[1:4]

with open(raw_fixture_path, encoding="utf-8") as fp:
    fixture = json.load(fp)
with open(go_result_path, encoding="utf-8") as fp:
    go_results = json.load(fp)

lookup = {entry["name"]: entry.get("dates", []) for entry in go_results.get("results", [])}
for case in fixture.get("cases", []):
    name = case["name"]
    if name not in lookup:
        raise RuntimeError(f"go result missing case: {name}")
    case["expected_dates"] = lookup[name]

with open(out_path, "w", encoding="utf-8") as fp:
    json.dump(fixture, fp, ensure_ascii=False, indent=2)
PY

pushd "${ROOT_DIR}" >/dev/null
CROSS_VALIDATE_FIXTURE="${EXPECTED_FIXTURE}" \
CROSS_VALIDATE_OUTPUT="${RUST_RESULTS}" \
  cargo test -p scraper-service --test date_extractor_cross_validation -- --nocapture
popd >/dev/null

cp "${RAW_FIXTURE}" "${RAW_FIXTURE_ARTIFACT}"
cp "${EXPECTED_FIXTURE}" "${EXPECTED_FIXTURE_ARTIFACT}"
cp "${GO_RESULTS}" "${GO_RESULTS_ARTIFACT}"
cp "${RUST_RESULTS}" "${RUST_RESULTS_ARTIFACT}"

python3 - <<'PY' "${GO_RESULTS_ARTIFACT}" "${RUST_RESULTS_ARTIFACT}" "${REPORT_JSON}" "${REPORT_MD}" "${RUN_PREFIX}" "${RAW_FIXTURE_ARTIFACT}" "${EXPECTED_FIXTURE_ARTIFACT}"
import json
import sys
from datetime import datetime, timezone

(
    GO_RESULTS_PATH,
    RUST_RESULTS_PATH,
    REPORT_JSON_PATH,
    REPORT_MD_PATH,
    RUN_PREFIX,
    RAW_FIXTURE_PATH,
    EXPECTED_FIXTURE_PATH,
) = sys.argv[1:8]

with open(GO_RESULTS_PATH, encoding="utf-8") as fp:
    go_results = json.load(fp)
with open(RUST_RESULTS_PATH, encoding="utf-8") as fp:
    rust_results = json.load(fp)


def as_lookup(payload):
    return {item["name"]: item.get("dates", []) for item in payload.get("results", [])}


left = as_lookup(go_results)
right = as_lookup(rust_results)

all_names = sorted(set(left) | set(right))
mismatches = []
for name in all_names:
    go_dates = left.get(name)
    rust_dates = right.get(name)
    if go_dates != rust_dates:
        mismatches.append(
            {
                "name": name,
                "go_dates": go_dates,
                "rust_dates": rust_dates,
            }
        )

summary = {
    "run_id": RUN_PREFIX,
    "generated_at": datetime.now(timezone.utc).isoformat(),
    "raw_fixture_path": RAW_FIXTURE_PATH,
    "expected_fixture_path": EXPECTED_FIXTURE_PATH,
    "go_results_path": GO_RESULTS_PATH,
    "rust_results_path": RUST_RESULTS_PATH,
    "total_cases": len(all_names),
    "matched_cases": len(all_names) - len(mismatches),
    "mismatched_cases": len(mismatches),
    "mismatches": mismatches,
}

with open(REPORT_JSON_PATH, "w", encoding="utf-8") as fp:
    json.dump(summary, fp, ensure_ascii=False, indent=2)

lines = [
    f"# DateExtractor Cross Validation Report ({RUN_PREFIX})",
    "",
    f"- GeneratedAt(UTC): {summary['generated_at']}",
    f"- TotalCases: {summary['total_cases']}",
    f"- MatchedCases: {summary['matched_cases']}",
    f"- MismatchedCases: {summary['mismatched_cases']}",
    "",
]

if mismatches:
    lines.append("## Mismatches")
    lines.append("")
    for mismatch in mismatches[:20]:
        lines.append(f"- {mismatch['name']}")
        lines.append(f"  - Go: {mismatch['go_dates']}")
        lines.append(f"  - Rust: {mismatch['rust_dates']}")
    if len(mismatches) > 20:
        lines.append(f"- ... and {len(mismatches) - 20} more")
else:
    lines.append("## Result")
    lines.append("")
    lines.append("All cases matched.")

with open(REPORT_MD_PATH, "w", encoding="utf-8") as fp:
    fp.write("\n".join(lines) + "\n")

if mismatches:
    raise SystemExit(1)
PY

echo "[cross-validate-live] ✅ matched ${TARGET_COUNT} cases"
echo "[cross-validate-live] report_json=${REPORT_JSON}"
echo "[cross-validate-live] report_md=${REPORT_MD}"
echo "[cross-validate-live] raw_fixture=${RAW_FIXTURE_ARTIFACT}"
echo "[cross-validate-live] expected_fixture=${EXPECTED_FIXTURE_ARTIFACT}"
