#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

"${root}/scripts/ci/check-postgres-capacity.sh" >"${tmp}/out"
# reserve는 superuser 예약 3을 뺀 비슈퍼유저 여유다: 60 - 3 - 55 = 2.
grep -q 'max=60 superuser_reserved=3 allocated=55 reserve=2' "${tmp}/out"
cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/policy.tsv"
sed -i '/^youtube-collector|/d' "${tmp}/policy.tsv"
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

cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/no-superuser-policy.tsv"
sed -i '/^@superuser-reserved|/d' "${tmp}/no-superuser-policy.tsv"
if "${root}/scripts/ci/check-postgres-capacity.sh" "${root}/deploy/compose/docker-compose.prod.yml" "${tmp}/no-superuser-policy.tsv" >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a policy without the superuser reservation pin" >&2
	exit 1
fi
grep -q 'policy must pin exactly one @superuser-reserved' "${tmp}/out"

# compose가 superuser 예약을 바꾸면 policy pin과 어긋나므로 거부해야 한다.
cp "${root}/deploy/compose/docker-compose.prod.yml" "${tmp}/superuser-compose.yml"
sed -i 's/^\(\s*\)- "max_connections=60"$/&\n\1- "-c"\n\1- "superuser_reserved_connections=5"/' "${tmp}/superuser-compose.yml"
grep -q 'superuser_reserved_connections=5' "${tmp}/superuser-compose.yml"
if "${root}/scripts/ci/check-postgres-capacity.sh" "${tmp}/superuser-compose.yml" "${root}/scripts/ci/postgres-capacity-policy.tsv" >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a compose superuser reservation that differs from the policy pin" >&2
	exit 1
fi
grep -q 'holo-postgres superuser_reserved_connections=\[5\], want \[3\]' "${tmp}/out"

cat >"${tmp}/safe.env" <<'ENV'
BOT_POSTGRES_POOL_MAX_CONNS=3
YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=8
ENV
"${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/safe.env" >"${tmp}/out"
grep -q "source=target-env:${tmp}/safe.env" "${tmp}/out"
grep -q 'allocated=54 reserve=3' "${tmp}/out"
"${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/safe.env" --target-env-only >"${tmp}/out"
grep -q 'allocated=54 reserve=3' "${tmp}/out"

: >"${tmp}/default.env"
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/default.env" --target-env-only \
  --scale=youtube-collector=2 >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted producer scale 2 above the server budget" >&2
	exit 1
fi
grep -q 'max=60 superuser_reserved=3 allocated=63 reserve=-6' "${tmp}/out"

for invalid_scale in \
  '--scale=youtube-collector' \
  '--scale=youtube-collector=two' \
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
YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=7
ENV
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/heterogeneous-ap.env" --target-env-only >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a per-host override for a multi-instance owner" >&2
	exit 1
fi
grep -q 'shared by multiple independently rendered instances' "${tmp}/out"
if grep -q 'YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=7' "${tmp}/out"; then
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
YOUTUBE_COLLECTOR_POSTGRES_POOL_MAX_CONNS=8
ENV
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${root}/scripts/ci/postgres-capacity-policy.tsv" \
  "${tmp}/unsafe.env" >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted target-rendered overrides above the server budget" >&2
	exit 1
fi
grep -q 'connection budget exhausted' "${tmp}/out"

# superuser 예약이 늘면 같은 할당에서도 비슈퍼유저 여유가 하한 아래로 내려간다.
cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/superuser4-policy.tsv"
sed -i 's/^@superuser-reserved|3$/@superuser-reserved|4/' "${tmp}/superuser4-policy.tsv"
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${tmp}/superuser4-policy.tsv" \
  "${tmp}/default.env" --target-env-only >"${tmp}/out" 2>&1; then
	echo "capacity gate ignored the superuser reservation when computing reserve" >&2
	exit 1
fi
grep -q 'max=60 superuser_reserved=4 allocated=55 reserve=1, want reserve >= 2' "${tmp}/out"

source "${root}/scripts/deploy/lib/postgres-capacity.sh"
postgres_capacity_assert_policy_target "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/default.env" >"${tmp}/shell-out"
grep -q 'max=60 superuser_reserved=3 allocated=55 reserve=2' "${tmp}/shell-out"
if postgres_capacity_assert_policy_target "${tmp}/superuser4-policy.tsv" "${tmp}/default.env" >"${tmp}/shell-out" 2>&1; then
	echo "deployment capacity preflight ignored the superuser reservation when computing reserve" >&2
	exit 1
fi
grep -q 'max=60 superuser_reserved=4 allocated=55 reserve=1, want reserve >= 2' "${tmp}/shell-out"
if postgres_capacity_assert_policy_target "${tmp}/no-superuser-policy.tsv" "${tmp}/default.env" >"${tmp}/shell-out" 2>&1; then
	echo "deployment capacity preflight accepted a policy without the superuser reservation pin" >&2
	exit 1
fi
grep -q 'policy must pin exactly one @superuser-reserved' "${tmp}/shell-out"
# 앞자리 0은 bash 산술에서 8진수가 되므로 두 gate 모두 형식 오류로 거부한다.
cp "${root}/scripts/ci/postgres-capacity-policy.tsv" "${tmp}/leading-zero-policy.tsv"
sed -i 's/^@superuser-reserved|3$/@superuser-reserved|03/' "${tmp}/leading-zero-policy.tsv"
if postgres_capacity_assert_policy_target "${tmp}/leading-zero-policy.tsv" "${tmp}/default.env" >"${tmp}/shell-out" 2>&1; then
	echo "deployment capacity preflight accepted a zero-padded superuser reservation" >&2
	exit 1
fi
grep -q 'policy has an invalid @superuser-reserved' "${tmp}/shell-out"
if "${root}/scripts/ci/check-postgres-capacity.sh" \
  "${root}/deploy/compose/docker-compose.prod.yml" \
  "${tmp}/leading-zero-policy.tsv" \
  "${tmp}/default.env" --target-env-only >"${tmp}/out" 2>&1; then
	echo "capacity gate accepted a zero-padded superuser reservation" >&2
	exit 1
fi
grep -q 'policy has an invalid @superuser-reserved' "${tmp}/out"

echo "ok: PostgreSQL capacity gate rejects unsafe and heterogeneous target overrides"
