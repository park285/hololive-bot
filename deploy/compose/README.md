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
