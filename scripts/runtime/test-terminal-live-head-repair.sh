#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
work=$(mktemp -d)
name="iris-terminal-head-test-$$"
cid=
cleanup() {
  if [[ -n "$cid" ]]; then docker rm -f -v "$cid" >/dev/null; fi
  rm -rf "$work"
}
trap cleanup EXIT
cid=$(docker run -d --pull=never --name "$name" --network=none --memory=512m --cpus=2 \
  --tmpfs /var/lib/postgresql -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_DB=hololive postgres:18.6-alpine)
ready=false
for ((i=0;i<60;i++)); do
  if docker exec "$cid" sh -c 'test "$(cat /proc/1/comm)" = postgres' >/dev/null 2>&1 &&
    docker exec "$cid" psql -U postgres -d hololive -X -qAt -c 'SELECT 1' >/dev/null 2>&1; then
    ready=true; break
  fi
  sleep 0.2
done
[[ "$ready" == true ]]
psql_test() { docker exec -i "$cid" psql -U postgres -d hololive -X -qAt -v ON_ERROR_STOP=1 "$@"; }
psql_test <<'SQL'
CREATE TABLE youtube_live_sessions(video_id text PRIMARY KEY, status text, ended_at timestamptz, payload text);
CREATE TABLE youtube_live_reconciliation_heads(
 video_id text PRIMARY KEY, status text, ended_at timestamptz, updated_at timestamptz,
 last_live_positive_at timestamptz, last_upcoming_positive_at timestamptz,
 end_reason text, end_candidate_kind text, end_candidate_observation_id bigint,
 next_end_check_at timestamptz, sentinel jsonb);
CREATE TABLE alarm_dispatch_deliveries(id integer PRIMARY KEY, status text);
INSERT INTO alarm_dispatch_deliveries VALUES(1,'quarantined');
INSERT INTO youtube_live_sessions VALUES
 ('a','ENDED','2026-08-16','preserve a'),('b','ENDED','2026-08-16','preserve b'),
 ('live','LIVE',NULL,'preserve live'),('upcoming','UPCOMING',NULL,'preserve upcoming'),
 ('conflict','ENDED','2026-08-16','preserve conflict'),('recent','ENDED','2026-08-16','preserve recent'),
 ('candidate','ENDED','2026-08-16','preserve candidate');
INSERT INTO youtube_live_reconciliation_heads(video_id,status,updated_at,last_live_positive_at,sentinel)
SELECT video_id,'LIVE','2026-08-16','2026-08-15','{"evidence":"preserve"}' FROM youtube_live_sessions WHERE video_id<>'upcoming';
UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-08-18' WHERE video_id='conflict';
UPDATE youtube_live_reconciliation_heads SET updated_at='2026-09-01' WHERE video_id='recent';
UPDATE youtube_live_reconciliation_heads SET end_candidate_kind='EXPLICIT_END',end_candidate_observation_id=1,next_end_check_at='2026-08-16' WHERE video_id='candidate';
SQL
psql_test <"$script_dir/preview-terminal-live-heads.sql" >"$work/preview.json"
read -r count digest < <(python3 - "$work/preview.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]));assert d['count']==2
assert {x['head']['video_id'] for x in d['rows']}=={'a','b'}
print(d['count'],d['digest'])
PY
)
baseline=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h")
if psql_test -v expected_count=2 -v expected_digest=00000000000000000000000000000000 \
  <"$script_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'stale digest was accepted' >&2; exit 1
fi
test "$baseline" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h")"
if psql_test -v expected_count=1 -v expected_digest="$digest" \
  <"$script_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'wrong count was accepted' >&2; exit 1
fi
# 스냅샷 이후 새 positive가 도착하면 같은 대상 ID라도 전체 거절한다.
psql_test -c "UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-08-20' WHERE video_id='a'"
if psql_test -v expected_count="$count" -v expected_digest="$digest" \
  <"$script_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'new positive was overwritten' >&2; exit 1
fi
test "$(psql_test -c "SELECT count(*) FROM youtube_live_reconciliation_heads WHERE status='ENDED'")" = 0
psql_test -c "UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-08-15' WHERE video_id='a'"
protected=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(p) ORDER BY video_id)::text) FROM youtube_live_sessions p")
others=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h WHERE video_id NOT IN('a','b')")
psql_test -v expected_count="$count" -v expected_digest="$digest" <"$script_dir/reconcile-terminal-live-heads.sql" >"$work/applied"
test "$(psql_test -c "SELECT count(*) FROM youtube_live_reconciliation_heads WHERE status='ENDED' AND ended_at='2026-08-16' AND end_reason IS NULL AND sentinel->>'evidence'='preserve'")" = 2
test "$protected" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(p) ORDER BY video_id)::text) FROM youtube_live_sessions p")"
test "$others" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h WHERE video_id NOT IN('a','b')")"
test "$(psql_test -c "SELECT status FROM alarm_dispatch_deliveries WHERE id=1")" = quarantined
if psql_test -v expected_count="$count" -v expected_digest="$digest" \
  <"$script_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'already applied snapshot was accepted' >&2; exit 1
fi
echo 'PASS: exact repair, stale/count/new-positive refusal, canonical/non-target/dispatch preservation'
