#!/usr/bin/env bash
set -euo pipefail

MODULE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROOT_DIR="$(cd "${MODULE_DIR}/../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

pass() {
  echo "[PASS] $*"
}

fixture_root="${TMP_DIR}/repo"
fixture_module="${fixture_root}/hololive/hololive-kakao-bot-go"
fakebin="${TMP_DIR}/fakebin"
poison_file="${TMP_DIR}/env-command-executed"

mkdir -p "${fixture_module}/scripts" "${fixture_module}/bin" "${fixture_root}/scripts/deploy/lib" "${fakebin}"
cp "${MODULE_DIR}/scripts/bot.sh" "${fixture_module}/scripts/bot.sh"
cp "${ROOT_DIR}/scripts/deploy/lib/compose-env.sh" "${fixture_root}/scripts/deploy/lib/compose-env.sh"
touch "${fixture_module}/bin/bot"
chmod +x "${fixture_module}/scripts/bot.sh"

cat > "${fixture_module}/.env" <<EOF
IRIS_BASE_URL=http://127.0.0.1:3000
HOLODEX_API_KEY_1=test-key
CACHE_HOST=127.0.0.1
POISON=\$(touch ${poison_file})
EOF

cat > "${fakebin}/podman" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  ps)
    exit 0
    ;;
  *)
    exit 1
    ;;
esac
EOF
chmod +x "${fakebin}/podman"

if PATH="${fakebin}:${PATH}" CONTAINER_CLI=podman "${fixture_module}/scripts/bot.sh" start --no-ready-wait >/tmp/bot-env-loader-test.out 2>/tmp/bot-env-loader-test.err; then
  fail "bot start should stop before dependency checks for invalid literal env"
fi

[[ ! -e "${poison_file}" ]] || fail ".env command substitution was executed"
grep -q "command substitution" /tmp/bot-env-loader-test.err || fail "expected command substitution rejection"

pass "bot env loader treats .env as literal data"
