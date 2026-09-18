#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
. "$ROOT_DIR/scripts/deploy/lib/ap-prechange-config.sh"
fail() { echo "[FAIL] $*" >&2; exit 1; }
diagnostic() { printf '%s\n' "$1" >&2; return 17; }
for key in IRIS_WEBHOOK_TOKEN IRIS_BOT_TOKEN SESSION_SECRET ADMIN_PASS_BCRYPT; do
    ap_prechange_config diagnostic "error while interpolating services.fixture.environment.$key: required variable $key is missing a value: $key is required" || fail "legacy key rejected: $key"
done
for line in \
    'IRIS_BOT_TOKEN permission denied' \
    'env file /etc/stack-secrets/hololive-bot/bot.env not found' \
    'error while interpolating services.fixture.environment.IRIS_BOT_TOKEN: required variable IRIS_BOT_TOKEN is missing a value: SESSION_SECRET is required' \
    'error while interpolating services.fixture.environment.DATABASE_PASSWORD: required variable DATABASE_PASSWORD is missing a value: DATABASE_PASSWORD is required' \
    $'error while interpolating services.fixture.environment.IRIS_BOT_TOKEN: required variable IRIS_BOT_TOKEN is missing a value: IRIS_BOT_TOKEN is required\nYAML failure'; do
    if ap_prechange_config diagnostic "$line"; then fail "unrelated or mixed error accepted"; fi
done
# remote에 전송하는 동일한 함수 본문도 같은 판정을 수행한다.
bash -c "$(declare -f ap_prechange_config diagnostic); ap_prechange_config diagnostic 'error while interpolating services.fixture.environment.SESSION_SECRET: required variable SESSION_SECRET is missing a value: SESSION_SECRET is required'"
for script in ap-deploy.sh ap-rollback.sh; do
    [[ $(grep -c '^ap_prechange_config sudo ' "$ROOT_DIR/scripts/deploy/$script") == 1 ]] || fail "$script prechange wrapper missing"
    grep -E '^[[:space:]]*sudo -n env .*config --quiet' "$ROOT_DIR/scripts/deploy/$script" >/dev/null || fail "$script mandatory post-change config missing"
done
echo '[PASS] bounded legacy Compose preflight and mandatory post-change checks'
