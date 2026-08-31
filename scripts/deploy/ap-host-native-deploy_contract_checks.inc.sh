# shellcheck shell=bash

if grep -Eq 'HOLOLIVE_H3_ADDR=:%s' "${DEPLOY}"; then
  record_fail "ap-host-native binds H3 to all interfaces (:port) (8c2e3ef9)"
else
  pass "ap-host-native H3 not bound to all interfaces"
fi

if grep -Eq 'HOLOLIVE_H3_ADDR=127\.0\.0\.1:%s' "${DEPLOY}"; then
  pass "ap-host-native H3 bound to loopback"
else
  record_fail "ap-host-native H3 bind not narrowed to loopback (8c2e3ef9)"
fi

if grep -Fq 'YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true' "${DEPLOY}"; then
  pass "ap-host-native enables the collector runtime"
else
  record_fail "ap-host-native must set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true"
fi

if grep -Fq '. "$REPO_ROOT/scripts/deploy/lib/source-revision.sh"' "${DEPLOY}" &&
   grep -Fq 'native_revision="$(deploy_source_revision "$REPO_ROOT")"' "${DEPLOY}" &&
   grep -Fq 'check-youtube-collector-go-artifact.sh" "$artifact_dir"' "${DEPLOY}"; then
  pass "ap-host-native stamps and validates revision through the shared artifact contract"
else
  record_fail "ap-host-native must derive one source revision and validate the built artifact before packaging"
fi
if grep -Eq 'git -C "\$REPO_ROOT" rev-parse --verify .HEAD' "${DEPLOY}"; then
  record_fail "ap-host-native must not stamp revision with raw git rev-parse HEAD"
else
  pass "ap-host-native does not use raw git rev-parse for revision"
fi

if grep -Fq 'AP_POSTGRES_HOST="${AP_POSTGRES_HOST:-hololive-postgres.tail742dd8.ts.net}"' "${DEPLOY}" &&
   grep -Fq "printf 'POSTGRES_HOST=%s\\n' \"\$AP_POSTGRES_HOST\"" "${DEPLOY}"; then
  pass "ap-host-native uses stable PostgreSQL DNS"
else
  record_fail "ap-host-native must use stable PostgreSQL DNS"
fi
if grep -Eq 'CACHE_(HOST|PORT|SOCKET_PATH|PASSWORD)' "${DEPLOY}"; then
  record_fail "ap-host-native generated env must have 0 CACHE lines"
else
  pass "ap-host-native generated env has 0 CACHE lines"
fi

if grep -Fq 'write_unit()' "${DEPLOY}" || grep -Fq 'cat > "$dest"' "${DEPLOY}"; then
  record_fail "host-native deploy must not duplicate the checked-in systemd unit owner"
elif ! grep -Fq 'cp "$UNIT_TEMPLATE" "$artifact_dir/hololive-youtube-collector@.service"' "${DEPLOY}"; then
  record_fail "host-native deploy must copy the checked-in systemd unit byte-identically"
else
  pass "host-native deploy packages the single checked-in systemd unit owner"
fi

if grep -Eq 'EnvironmentFile=-?/etc/stack-secrets/hololive-bot/(ap-)?compose\.env' "${UNIT_TEMPLATE}"; then
  record_fail "checked-in host-native unit template must not load a shared Compose env"
elif ! grep -Fxq 'EnvironmentFile=/etc/stack-secrets/hololive-bot/youtube-collector.env' "${UNIT_TEMPLATE}" ||
     ! grep -Fxq 'EnvironmentFile=/etc/hololive-bot/youtube-collector-host.env' "${UNIT_TEMPLATE}"; then
  record_fail "checked-in host-native unit template must require the two scoped env files"
else
  pass "checked-in host-native unit template exposes only collector-scoped env files"
fi

if grep -Fq 'test -r /etc/stack-secrets/hololive-bot/ap-compose.env' "${REMOTE_APPLY}"; then
  record_fail "host-native remote apply must not require the shared AP Compose env"
elif ! grep -Fq "grep -Eq '^CACHE_(PASSWORD|HOST|PORT|DB|SOCKET_PATH)=' /etc/stack-secrets/hololive-bot/youtube-collector.env" "${REMOTE_APPLY}"; then
  record_fail "host-native remote apply must reject cache keys in the collector-scoped env"
else
  pass "host-native remote apply excludes ap-compose.env and rejects cache keys in scoped env"
fi
