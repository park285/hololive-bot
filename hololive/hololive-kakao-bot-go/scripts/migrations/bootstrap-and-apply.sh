#!/bin/sh
# Copyright (c) 2025 Kapu
#
# Permission is hereby granted, free of charge, to any person obtaining a copy
# of this software and associated documentation files (the "Software"), to deal
# in the Software without restriction, including without limitation the rights
# to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
# copies of the Software, and to permit persons to whom the Software is
# furnished to do so, subject to the following conditions:
#
# The above copyright notice and this permission notice shall be included in
# all copies or substantial portions of the Software.
#
# THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
# IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
# FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
# AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
# LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
# OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
# SOFTWARE.

set -eu

PGHOST="${PGHOST:-postgres}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-hololive}"
MIGRATIONS_DIR="${MIGRATIONS_DIR:-/migrations}"

POSTGRES_ADMIN_USER="${POSTGRES_ADMIN_USER:-postgres_admin}"
POSTGRES_ADMIN_PASSWORD="${POSTGRES_ADMIN_PASSWORD:-${PGPASSWORD:-}}"
HOLOLIVE_SCRAPER_USER="${HOLOLIVE_SCRAPER_USER:-hololive_scraper}"
HOLOLIVE_SCRAPER_PASSWORD="${HOLOLIVE_SCRAPER_PASSWORD:-${POSTGRES_ADMIN_PASSWORD}}"

if [ -z "${POSTGRES_ADMIN_PASSWORD}" ]; then
  echo "POSTGRES_ADMIN_PASSWORD is required for role bootstrap" >&2
  exit 1
fi

if [ -z "${HOLOLIVE_SCRAPER_PASSWORD}" ]; then
  echo "HOLOLIVE_SCRAPER_PASSWORD is required for scraper role bootstrap" >&2
  exit 1
fi

echo "==> bootstrap scraper role (${HOLOLIVE_SCRAPER_USER}) on ${PGDATABASE}@${PGHOST}:${PGPORT}"
PGPASSWORD="${POSTGRES_ADMIN_PASSWORD}" psql \
  -v ON_ERROR_STOP=1 \
  -h "${PGHOST}" \
  -p "${PGPORT}" \
  -U "${POSTGRES_ADMIN_USER}" \
  -d postgres \
  --set=hololive_db="${PGDATABASE}" \
  --set=hololive_scraper="${HOLOLIVE_SCRAPER_USER}" \
  --set=hololive_scraper_password="${HOLOLIVE_SCRAPER_PASSWORD}" <<'EOSQL'
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', :'hololive_scraper', :'hololive_scraper_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'hololive_scraper') \gexec

SELECT format(
  'ALTER ROLE %I WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION INHERIT PASSWORD %L',
  :'hololive_scraper', :'hololive_scraper_password'
) \gexec

SELECT format('GRANT CONNECT ON DATABASE %I TO %I', :'hololive_db', :'hololive_scraper') \gexec

\connect :hololive_db
SELECT format('GRANT USAGE ON SCHEMA public TO %I', :'hololive_scraper') \gexec
EOSQL

echo "==> apply migrations with PGUSER=${PGUSER:-hololive_migrator}"
exec /bin/sh "${MIGRATIONS_DIR}/apply-all.sh"
