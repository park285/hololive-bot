#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

"${root}/scripts/ci/check-postgres-capacity.sh" >/dev/null
cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/policy.tsv"
sed -i '/^youtube-producer|/d' "${tmp}/policy.tsv"
if "${root}/scripts/ci/check-postgres-capacity.sh" "${root}/deploy/compose/docker-compose.prod.yml" "${tmp}/policy.tsv" >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted an incomplete AP inventory" >&2
	exit 1
fi
grep -q 'owner inventory mismatch' "${tmp}/out"

cp "${root}/deploy/compose/docker-compose.prod.yml" "${tmp}/compose.yml"
cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/low-limit-policy.tsv"
sed -i 's/max_connections=60/max_connections=57/' "${tmp}/compose.yml"
sed -i 's/@server-limit|60/@server-limit|57/' "${tmp}/low-limit-policy.tsv"
if "${root}/scripts/ci/check-postgres-capacity.sh" "${tmp}/compose.yml" "${tmp}/low-limit-policy.tsv" >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a reserve below the required floor" >&2
	exit 1
fi
grep -q 'connection budget exhausted' "${tmp}/out"

cat >"${tmp}/safe.env" <<'ENV'
BOT_POSTGRES_POOL_MAX_CONNS=3
YOUTUBE_PRODUCER_POSTGRES_POOL_MAX_CONNS=8
ENV
"${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/safe.env" >"${tmp}/out"
grep -q "source=target-env:${tmp}/safe.env" "${tmp}/out"
grep -q 'allocated=52 reserve=8' "${tmp}/out"
"${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/safe.env" --target-env-only >"${tmp}/out"
grep -q 'allocated=52 reserve=8' "${tmp}/out"

: >"${tmp}/default.env"
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/default.env" --target-env-only \
  --scale=youtube-producer=2 >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted producer scale 2 above the server budget" >&2
	exit 1
fi
grep -q 'max=60 allocated=61 reserve=-1' "${tmp}/out"

for invalid_scale in \
  '--scale=youtube-producer' \
  '--scale=youtube-producer=two' \
  '--scale=unknown-service=2'; do
	if "${root}/scripts/ci/check-postgres-capacity.sh" \
	  "${root}/deploy/compose/docker-compose.prod.yml" \
	  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
	  "${tmp}/default.env" --target-env-only \
	  "${invalid_scale}" >"${tmp}/out" 2>&1; then
		echo "capacity gate accepted invalid scale override: ${invalid_scale}" >&2
		exit 1
	fi
done

if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/default.env" --target-env-only \
  --scale=hololive-api=1 --scale=hololive-api=1 >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted duplicate scale overrides" >&2
	exit 1
fi
grep -q 'duplicate scale override for service: hololive-api' "${tmp}/out"

cat >"${tmp}/scaled-heterogeneous.env" <<'ENV'
BOT_POSTGRES_POOL_MAX_CONNS=3
ENV
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/scaled-heterogeneous.env" --target-env-only \
  --scale=hololive-api=2 >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a heterogeneous override for a scaled service" >&2
	exit 1
fi
grep -q 'shared by multiple independently rendered instances' "${tmp}/out"

cat >"${tmp}/heterogeneous-ap.env" <<'ENV'
YOUTUBE_PRODUCER_POSTGRES_POOL_MAX_CONNS=7
ENV
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/heterogeneous-ap.env" --target-env-only >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a per-host override for a multi-instance owner" >&2
	exit 1
fi
grep -q 'shared by multiple independently rendered instances' "${tmp}/out"
if grep -q 'YOUTUBE_PRODUCER_POSTGRES_POOL_MAX_CONNS=7' "${tmp}/out"; then
	echo "capacity gate disclosed the rejected target value" >&2
	exit 1
fi

cat >"${tmp}/malformed.env" <<'ENV'
BOT_POSTGRES_POOL_MAX_CONNS=not-a-number
ENV
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/malformed.env" --target-env-only >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a malformed single-instance override" >&2
	exit 1
fi
grep -q 'BOT_POSTGRES_POOL_MAX_CONNS must be a positive integer' "${tmp}/out"
if grep -q 'not-a-number' "${tmp}/out"; then
	echo "capacity gate disclosed the rejected target value" >&2
	exit 1
fi

cat >"${tmp}/unsafe.env" <<'ENV'
BOT_POSTGRES_POOL_MAX_CONNS=50
YOUTUBE_PRODUCER_POSTGRES_POOL_MAX_CONNS=8
ENV
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/unsafe.env" >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted target-rendered overrides above the server budget" >&2
	exit 1
fi
grep -q 'connection budget exhausted' "${tmp}/out"

echo "ok: PostgreSQL capacity gate rejects unsafe and heterogeneous target overrides"
