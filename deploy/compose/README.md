# Compose Files

- `docker-compose.prod.yml`: production baseline for the main host.
- `docker-compose.live-compat.yml`: opt-in compatibility overlay for pre-hardening live wiring.
- `docker-compose.main-ap.yml`: main-host `youtube-producer-c` overlay.
- `docker-compose.main-ap.live-compat.yml`: live-compat overlay for `youtube-producer-c`.
- `docker-compose.osaka.yml`: Osaka AP overlay for `youtube-producer-a`.
- `docker-compose.osaka2.yml`: second Osaka AP overlay for `youtube-producer-d`.
- `docker-compose.seoul.yml`: Seoul AP overlay for `youtube-producer-b`.
- `docker-compose.remote-cache.yml`: optional BuildKit remote cache overlay.

Prefer repository wrappers over raw `docker compose`:

```bash
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml config
./scripts/deploy/compose-redeploy-service.sh <service>
```

## Runtime Env Inputs

`scripts/deploy/compose.sh` injects one Compose interpolation env file with `--env-file`.
The OpenBao default is:

```text
/run/hololive-bot/compose.env
```

Use `COMPOSE_ENV_FILE=<path>` for local tests or transition-period compatibility. The
legacy monolithic `/run/hololive-bot/env` is no longer a production `env_file` default.
AP deploy/rollback wrappers set `COMPOSE_ENV_FILE=/run/hololive-bot/ap-compose.env`
so AP hosts do not receive Iris egress tokens in their Compose interpolation file.

Application-only env is scoped per service:

```text
HOLOLIVE_API_ENV_FILE=/run/hololive-bot/bot.env
HOLOLIVE_ALARM_WORKER_ENV_FILE=/run/hololive-bot/alarm-worker.env
HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE=/run/hololive-bot/youtube-producer.env
```

AP overlays use only `youtube-producer.env` for `youtube-producer` instances, so Iris
egress tokens stay out of AP producer containers. Osaka/Seoul AP hosts also use
`ap-compose.env`, which excludes `IRIS_WEBHOOK_TOKEN` and `IRIS_BOT_TOKEN`.
`docker-compose.main-ap.yml` also uses scoped `youtube-producer.env` for
`youtube-producer-c`; it still must not receive Iris egress tokens or the
monolithic Compose env file as an `env_file`.

Deploy this repo-side contract after OpenBao Agent has rendered `compose.env` or
`ap-compose.env` plus the per-service env files for the target host.

## PostgreSQL TLS

`holo-postgres` serves TLS with `ssl=on`. The central OpenBao Agent renders the
server certificate and key under `/run/hololive-bot/postgres-tls/`, outside the
client-mounted `certs/` directory. Certificate renewal sends `SIGHUP` to
`holo-postgres` so PostgreSQL reloads the server material without a container
recreate.

All production PostgreSQL clients use `verify-full` with the CA bundle mounted
at `/run/hololive-bot/certs/postgres-ca.pem`: the five central Go runtimes,
`youtube-producer-c`, `hololive-db-migrate`, Osaka `youtube-producer-a`,
Osaka2 `youtube-producer-d`, and Seoul `youtube-producer-b`. The retired
insecure downgrade ledger stays closed by preserving production paths with
verified TLS and the CA bundle above.
