# Compose Files

- `docker-compose.prod.yml`: production baseline for the main host.
- `docker-compose.live-compat.yml`: opt-in compatibility overlay for pre-hardening live wiring.
- `docker-compose.main-ap.yml`: main-host `youtube-producer-c` overlay.
- `docker-compose.main-ap.live-compat.yml`: live-compat overlay for `youtube-producer-c`.
- `docker-compose.osaka.yml`: Osaka AP overlay for `youtube-producer-a`.
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
HOLOLIVE_BOT_ENV_FILE=/run/hololive-bot/bot.env
HOLOLIVE_ALARM_WORKER_ENV_FILE=/run/hololive-bot/alarm-worker.env
HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE=/run/hololive-bot/youtube-producer.env
```

AP overlays use only `youtube-producer.env` for `youtube-producer` instances, so Iris
egress tokens stay out of AP producer containers. Osaka/Seoul AP hosts also use
`ap-compose.env`, which excludes `IRIS_WEBHOOK_TOKEN` and `IRIS_BOT_TOKEN`.
`docker-compose.main-ap.yml` keeps `youtube-producer-c` without an `env_file`; it
receives only explicit `environment:` values from the base compose and overlay.

Do not deploy this repo-side contract to a host until OpenBao Agent renders
`compose.env` or `ap-compose.env` and the per-service env files for that host.
