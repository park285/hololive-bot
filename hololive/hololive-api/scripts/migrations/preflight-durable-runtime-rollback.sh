#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 || "$1" != "--ingress-quiesced" ]]; then
  echo "usage: $0 --ingress-quiesced" >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${root}/libpq-connection.sh"
require_libpq_service

counts="$(psql -w -X -v ON_ERROR_STOP=1 -At -F '|' <<'SQL'
SELECT
  (SELECT count(*) FROM bot_webhook_inbox WHERE status IN ('pending', 'processing', 'retry')),
  (SELECT count(*) FROM bot_command_executions WHERE status = 'claimed'),
  (SELECT count(*) FROM bot_reply_outbox WHERE status IN (
    'pending', 'submitting', 'accepted', 'retryable_pre_dispatch', 'outcome_unknown', 'manual_review'
  )),
  (SELECT count(*) FROM bot_webhook_heads);
SQL
)"

IFS='|' read -r inbox_active command_active outbox_active heads_active <<<"${counts}"
for value in "${inbox_active}" "${command_active}" "${outbox_active}" "${heads_active}"; do
  [[ "${value}" =~ ^[0-9]+$ ]] || { echo "durable rollback preflight returned an invalid count" >&2; exit 1; }
done

if (( inbox_active != 0 || command_active != 0 || outbox_active != 0 || heads_active != 0 )); then
  echo "durable rollback blocked: inbox=${inbox_active} commands=${command_active} outbox=${outbox_active} heads=${heads_active}" >&2
  echo "keep the durable runtime deployed and roll forward until every count is zero" >&2
  exit 1
fi

echo "durable rollback preflight passed: ingress asserted quiesced and all durable backlogs are empty"
