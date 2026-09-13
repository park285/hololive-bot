#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
# 실제 계정·호스트 secret을 사용하지 않고 격리한 root 컨테이너 안에서 파일 계약을 검증합니다.
docker run --rm --interactive --pull=missing --network=none --read-only --user=0 --tmpfs /tmp:size=8m,mode=1777 \
  --mount "type=bind,src=$root/scripts/deploy/materialize-admin-dashboard-secrets.sh,dst=/materialize.sh,readonly" \
  --entrypoint bash node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e -s <<'SH'
set -euo pipefail
export ADMIN_DASHBOARD_ENV_FILE=/tmp/source.env
export ADMIN_DASHBOARD_SECRET_DIR=/tmp/credentials
export HOLOLIVE_RUNTIME_GID=1002
umask 077
cat > /tmp/source.env <<'ENV'
ADMIN_PASS_HASH=$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
HOLO_BOT_API_KEY=synthetic-fixture-api-key-0000000000000000
SESSION_SECRET=retired-fixture-value
VALKEY_URL=retired-fixture-value
ENV
bash /materialize.sh
[[ "$(stat -c %a /tmp/credentials)" == 700 ]]
[[ "$(find /tmp/credentials -type f | wc -l)" -eq 2 ]]
for file in login-password-hash bot-hololive; do
  [[ "$(stat -c %a "/tmp/credentials/$file")" == 400 ]]
  [[ "$(stat -c %u "/tmp/credentials/$file")" == 1000 ]]
done
sed -i 's/^ADMIN_PASS_HASH=.*/ADMIN_PASS_HASH=retired-bcrypt-fixture/' /tmp/source.env
if bash /materialize.sh >/tmp/failure.log 2>&1; then exit 1; fi
grep -q 'Argon2id' /tmp/failure.log
! grep -q 'retired-bcrypt-fixture\|synthetic-fixture-api-key' /tmp/failure.log
chmod 0644 /tmp/source.env
if bash /materialize.sh >/tmp/failure.log 2>&1; then exit 1; fi
grep -q 'group/other accessible' /tmp/failure.log
echo '[admin-credentials] private files, retired capability removal and invalid hash/source rejection passed'
SH
