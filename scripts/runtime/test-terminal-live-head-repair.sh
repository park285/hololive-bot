#!/usr/bin/env bash
set -euo pipefail
script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
maintenance_dir=$(cd "$script_dir/../maintenance" && pwd)
repo_root=$(cd "$script_dir/../.." && pwd)
work=$(mktemp -d)
name="iris-terminal-head-test-$$"
cid=
cleanup() {
  if [[ -n "$cid" ]]; then docker rm -f -v "$cid" >/dev/null; fi
  rm -rf "$work"
}
trap cleanup EXIT
cid=$(docker run -d --pull=never --name "$name" --network=none --memory=512m --cpus=2 \
  --label io.iris.observability.logs=exclude \
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
# 운영과 같은 runner로 전역 잠금·수명 모델 갱신 함수·FK를 적용한다. 컨테이너 내부 Unix socket의 trust 인증이라 TLS와 비밀번호는 쓰지 않는다.
(cd "$repo_root" && CGO_ENABLED=0 go build -trimpath -o "$work/db-migrate" ./hololive/hololive-api/cmd/db-migrate)
docker cp "$work/db-migrate" "$cid:/tmp/db-migrate"
docker exec -e PGHOST=/var/run/postgresql -e PGPORT=5432 -e PGDATABASE=hololive -e PGUSER=postgres \
  -e PGPASSWORD=unused-local-trust -e PGSSLMODE=disable "$cid" /tmp/db-migrate >"$work/migration.log" 2>&1 || {
  cat "$work/migration.log" >&2
  exit 1
}
psql_test <<'SQL'
INSERT INTO alarm_dispatch_events(event_key,payload_hash,alarm_type,payload)
VALUES('repair-protected-event',repeat('a',64),'LIVE','{}');
INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id)
VALUES(repeat('b',64),'repair-protected-group','repair-room','repair-protected-request');
INSERT INTO alarm_dispatch_deliveries(id,event_id,room_id,dedupe_key,dispatch_group_key,send_unit_id,status,quarantined_at)
SELECT 1,e.id,'repair-room','repair-protected-delivery','repair-protected-group',u.id,'quarantined',now()
FROM alarm_dispatch_events e CROSS JOIN alarm_dispatch_send_units u;
INSERT INTO youtube_live_sessions(video_id,channel_id,status,ended_at,title) VALUES
 ('a','repair-channel','ENDED','2026-08-16','preserve a'),('b','repair-channel','ENDED','2026-08-16','preserve b'),
 ('live','repair-channel','LIVE',NULL,'preserve live'),('upcoming','repair-channel','UPCOMING',NULL,'preserve upcoming'),
 ('conflict','repair-channel','ENDED','2026-08-16','preserve conflict'),('recent','repair-channel','ENDED','2026-08-16','preserve recent'),
 ('candidate','repair-channel','ENDED','2026-08-16','preserve candidate');
INSERT INTO youtube_live_reconciliation_heads(video_id,status,updated_at,last_live_positive_at,last_live_positive_seen_at)
SELECT video_id,'LIVE','2026-08-16','2026-08-15','2026-08-15 00:01:00+00' FROM youtube_live_sessions WHERE video_id<>'upcoming';
INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,ended_at,negative_eligible,scope_covers)
VALUES('candidate','repair-channel','EXPLICIT_END',1,'2026-08-16','2026-08-16','2026-08-16','2026-08-16',true,true);
UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-08-18' WHERE video_id='conflict';
UPDATE youtube_live_reconciliation_heads SET updated_at='2026-09-01' WHERE video_id='recent';
UPDATE youtube_live_reconciliation_heads SET end_candidate_kind='EXPLICIT_END',end_candidate_observation_id=1,next_end_check_at='2026-08-16' WHERE video_id='candidate';
-- fixture는 원본을 직접 썼으므로 writer처럼 바꾼 key로 수명 모델을 맞춘다.
SELECT refresh_youtube_live_runtime_state(ARRAY(SELECT video_id FROM youtube_live_sessions));
SQL
psql_test <"$maintenance_dir/preview-terminal-live-heads.sql" >"$work/preview.json"
read -r count digest < <(python3 - "$work/preview.json" <<'PY'
import json,sys
d=json.load(open(sys.argv[1]));assert d['count']==2
assert {x['head']['video_id'] for x in d['rows']}=={'a','b'}
print(d['count'],d['digest'])
PY
)
baseline=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h")
runtime_baseline=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(s) ORDER BY video_id)::text) FROM youtube_live_runtime_state s")
test "$(psql_test -c "SELECT count(*) FROM youtube_live_runtime_state WHERE video_id IN('a','b')")" = 2
# 복구 표에 사용자 trigger가 하나라도 있으면 아무 것도 바꾸지 않고 거절한다.
psql_test -c 'CREATE FUNCTION unreviewed_repair_trigger() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$'
psql_test -c 'CREATE TRIGGER unreviewed_runtime_guard BEFORE UPDATE ON youtube_live_reconciliation_heads FOR EACH STATEMENT EXECUTE FUNCTION unreviewed_repair_trigger()'
if psql_test -v expected_count="$count" -v expected_digest="$digest" \
  <"$maintenance_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'unreviewed trigger was accepted' >&2; exit 1
fi
psql_test -c 'DROP TRIGGER unreviewed_runtime_guard ON youtube_live_reconciliation_heads; DROP FUNCTION unreviewed_repair_trigger()'
test "$baseline" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h")"
if psql_test -v expected_count=2 -v expected_digest=00000000000000000000000000000000 \
  <"$maintenance_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'stale digest was accepted' >&2; exit 1
fi
test "$baseline" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h")"
test "$runtime_baseline" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(s) ORDER BY video_id)::text) FROM youtube_live_runtime_state s")"
if psql_test -v expected_count=1 -v expected_digest="$digest" \
  <"$maintenance_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'wrong count was accepted' >&2; exit 1
fi
# 스냅샷 이후 새 positive가 도착하면 같은 대상 ID라도 전체 거절한다.
psql_test -c "UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-08-20' WHERE video_id='a'"
if psql_test -v expected_count="$count" -v expected_digest="$digest" \
  <"$maintenance_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'new positive was overwritten' >&2; exit 1
fi
test "$(psql_test -c "SELECT count(*) FROM youtube_live_reconciliation_heads WHERE status='ENDED'")" = 0
psql_test -c "UPDATE youtube_live_reconciliation_heads SET last_live_positive_at='2026-08-15' WHERE video_id='a'"
protected=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(p) ORDER BY video_id)::text) FROM youtube_live_sessions p")
others=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h WHERE video_id NOT IN('a','b')")
runtime_others=$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(s) ORDER BY video_id)::text) FROM youtube_live_runtime_state s WHERE video_id NOT IN('a','b')")
psql_test -v expected_count="$count" -v expected_digest="$digest" <"$maintenance_dir/reconcile-terminal-live-heads.sql" >"$work/applied"
test "$(psql_test -c "SELECT count(*) FROM youtube_live_reconciliation_heads WHERE status='ENDED' AND ended_at='2026-08-16' AND end_reason IS NULL AND last_live_positive_seen_at='2026-08-15 00:01:00+00'")" = 2
test "$(psql_test -c "SELECT count(*) FROM youtube_live_runtime_state WHERE video_id IN('a','b')")" = 0
test "$runtime_others" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(s) ORDER BY video_id)::text) FROM youtube_live_runtime_state s WHERE video_id NOT IN('a','b')")"
test "$protected" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(p) ORDER BY video_id)::text) FROM youtube_live_sessions p")"
test "$others" = "$(psql_test -c "SELECT md5(jsonb_agg(to_jsonb(h) ORDER BY video_id)::text) FROM youtube_live_reconciliation_heads h WHERE video_id NOT IN('a','b')")"
test "$(psql_test -c "SELECT status FROM alarm_dispatch_deliveries WHERE id=1")" = quarantined
if psql_test -v expected_count="$count" -v expected_digest="$digest" \
  <"$maintenance_dir/reconcile-terminal-live-heads.sql" >"$work/rejected" 2>&1; then
  echo 'already applied snapshot was accepted' >&2; exit 1
fi
echo 'PASS: exact repair and synchronous state, stale/count/new-positive/unreviewed-trigger refusal, canonical/non-target/dispatch preservation'
