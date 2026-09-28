#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COLLECTOR_WRAPPER="${ROOT_DIR}/scripts/deploy/lib/ap-host-native-collector-wrapper.sh"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "$tmp"
}
trap cleanup EXIT

credential_probe="$tmp/credential-probe"
credential_failure="$tmp/credential-failure"
cat > "$credential_probe" <<'EOF'
#!/usr/bin/env sh
set -eu
printf '%s|%s|%s\n' "${POSTGRES_USER-}" "${POSTGRES_DB-}" "${POSTGRES_PASSWORD-}"
EOF
cat > "$credential_failure" <<'EOF'
#!/usr/bin/env sh
exit 23
EOF
chmod +x "$credential_probe" "$credential_failure"

credential_case() {
  local name="$1"
  local expected="$2"
  shift 2
  local actual
  if actual="$(env -i PATH="$PATH" "$@" "$COLLECTOR_WRAPPER" "$credential_probe")" &&
     [[ "$actual" == "$expected" ]]; then
    pass "collector wrapper credential matrix: $name"
  else
    fail "collector wrapper credential matrix: $name (got ${actual:-<no output>}, want $expected)"
  fi
}

[[ -x "$COLLECTOR_WRAPPER" ]] || fail "collector wrapper must be executable"
pass "collector wrapper is an executable shared artifact"
credential_case "default scraper role" "hololive_scraper|hololive|scraper" \
  HOLOLIVE_SCRAPER_PASSWORD=scraper HOLOLIVE_DB_PASSWORD=runtime DB_PASSWORD=legacy
credential_case "explicit scraper role" "hololive_scraper|hololive|scraper" \
  POSTGRES_USER=hololive_scraper HOLOLIVE_SCRAPER_PASSWORD=scraper HOLOLIVE_DB_PASSWORD=runtime
credential_case "custom scraper role" "custom_scraper|hololive|scraper" \
  HOLOLIVE_SCRAPER_USER=custom_scraper HOLOLIVE_SCRAPER_PASSWORD=scraper HOLOLIVE_DB_PASSWORD=runtime
credential_case "explicit password wins" "hololive_scraper|hololive|explicit" \
  POSTGRES_USER=hololive_scraper POSTGRES_PASSWORD=explicit HOLOLIVE_SCRAPER_PASSWORD=scraper HOLOLIVE_DB_PASSWORD=runtime
credential_case "runtime role uses database credential" "other|hololive|runtime" \
  POSTGRES_USER=other HOLOLIVE_SCRAPER_PASSWORD=scraper HOLOLIVE_DB_PASSWORD=runtime DB_PASSWORD=legacy
# 역할별 비밀번호가 없으면 다른 역할이나 admin(DB_PASSWORD) 비밀번호로 내려가지 않고 실행 전에 실패한다.
credential_rejected_case() {
  local name="$1"
  shift
  if env -i PATH="$PATH" "$@" "$COLLECTOR_WRAPPER" "$credential_probe" >/dev/null 2>&1; then
    fail "collector wrapper must reject unresolved role credential: $name"
  fi
  pass "collector wrapper rejects unresolved role credential: $name"
}

credential_rejected_case "admin database password is not a role credential" \
  POSTGRES_USER=other DB_PASSWORD=legacy
credential_rejected_case "scraper role does not borrow runtime credential" \
  POSTGRES_USER=hololive_scraper HOLOLIVE_DB_PASSWORD=runtime DB_PASSWORD=legacy
credential_rejected_case "missing scraper password" \
  POSTGRES_USER=hololive_scraper

if env -i PATH="$PATH" HOLOLIVE_SCRAPER_PASSWORD=scraper "$COLLECTOR_WRAPPER" "$credential_failure"; then
  fail "collector wrapper must preserve target command failure"
else
  failure_status="$?"
fi
[[ "$failure_status" -eq 23 ]] || fail "collector wrapper must preserve target command failure status"
pass "collector wrapper preserves target command failure"
