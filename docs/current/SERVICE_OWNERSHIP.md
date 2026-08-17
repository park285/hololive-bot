# Service Ownership

## Scope

현재 3개 app runtime(`hololive-api`, `alarm-worker`, `youtube-collector`)의 책임 경계와 금지 소유 범위를 정리합니다. `hololive-api`는 bot/admin/llm plane을 한 프로세스에서 호스팅합니다. Historical handoff 문서는 `docs/history/runtime-split/`에 보관합니다.

## Ownership Matrix

| Runtime | Owns | Provides | Consumes | Must not own | Detail |
|---|---|---|---|---|---|
| `hololive-api` | Bot plane: Kakao/Iris webhook ingress, command routing, user-facing replies. Admin plane: dashboard-facing admin HTTP control plane + alarm HTTP compatibility facade during migration + `members.photo` Holodex PhotoSync product path. LLM plane: major event/member news scheduling, LLM summaries, internal subscription/trigger APIs. YouTube plane: observation claim/finalize, canonical persist, notification intent, live-end finalizer, retention/replay | Kakao webhook/H3 ingress, Admin API + trigger client facade, temporary alarm HTTP compatibility provider, `membernews`/`majorevent`/`trigger` internal HTTP contracts, YouTube consume | PostgreSQL (`hololive_runtime`), Valkey, Iris, settings Pub/Sub, alarm API, cliproxy/LLM where configured | alarm checking worker, alarm scheduling loops, proactive dispatch queue consumption, proactive notification egress, collector scrape/lease | `services/hololive-api.md` |
| `alarm-worker` | Alarm HTTP provider, alarm checker, alarm scheduler, dispatch queue publishing/consumption, proactive notification egress | Alarm HTTP provider, alarm queue publisher/consumer, YouTube outbox dispatcher | PostgreSQL, Valkey, settings Pub/Sub, Iris | Kakao command routing, YouTube collection, YouTube canonical detection write | `services/alarm-worker.md` |
| `youtube-collector` | AP fleet (`a`/`b`/`c`/`d`) external clients, provider adapters, fixture-backed parsing, normalization, collection target read, DB job lease/fence, bounded scheduling, rate limit/retry/cooldown, checkpoint, observation publish, provider health | Community/content/live/stats/profile/photo/schedule observations for the `hololive-api` YouTube plane | PostgreSQL (`hololive_scraper`) | canonical tables, live transition, domain watermark, notification intent/outbox, profile/photo 최종 선택, proactive egress | `services/youtube-collector.md` |

## Split Rules

- Cross-service APIs must use documented contracts under `docs/current/contracts/` and `hololive/hololive-shared/pkg/contracts/*`.
- Service-to-service `internal` package imports are not allowed as an ownership shortcut.
- Queue/PubSub changes must update `CONTRACT_MAP.md`, `QUEUE_AND_PUBSUB_CONTRACTS.md`, and affected service docs.
- Unclear ownership is marked `검토 필요` in the service doc instead of being silently assigned.
- Runtime binaries must use role-specific config loaders where available (`LoadBotRuntime`, `LoadAlarmWorkerRuntime`, `LoadAdminAPIRuntime`, `LoadLLMSchedulerRuntime`, `LoadYouTubeCollectorRuntime`) so ownership drift fails during startup rather than after queues or egress clients are constructed.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
go test ./hololive/hololive-shared/pkg/config/settings -run 'Runtime|NonEgress|AdminAPI'
```
