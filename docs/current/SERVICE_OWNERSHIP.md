# Service Ownership

## Scope

현재 3개 app runtime(`hololive-api`, `alarm-worker`, `youtube-producer`)의 책임 경계와 금지 소유 범위를 정리합니다. `hololive-api`는 bot/admin/llm plane을 한 프로세스에서 호스팅합니다. Historical handoff 문서는 `docs/history/runtime-split/`에 보관합니다.

## Ownership Matrix

| Runtime | Owns | Provides | Consumes | Must not own | Detail |
|---|---|---|---|---|---|
| `hololive-api` | Bot plane: Kakao/Iris webhook ingress, command routing, user-facing replies. Admin plane: dashboard-facing admin HTTP control plane + alarm HTTP compatibility facade during migration. LLM plane: major event/member news scheduling, LLM summaries, internal subscription/trigger APIs | Kakao webhook/H3 ingress, Admin API + trigger client facade, temporary alarm HTTP compatibility provider, `membernews`/`majorevent`/`trigger` internal HTTP contracts | PostgreSQL, Valkey, Iris, settings Pub/Sub, alarm API, cliproxy/LLM where configured | alarm checking worker, alarm scheduling loops, proactive dispatch queue consumption, proactive notification egress | `services/hololive-api.md` |
| `alarm-worker` | Alarm HTTP provider, alarm checker, alarm scheduler, dispatch queue publishing/consumption, proactive notification egress | Alarm HTTP provider, alarm queue publisher/consumer, YouTube outbox dispatcher | PostgreSQL, Valkey, settings Pub/Sub, Iris | Kakao command routing | `services/alarm-worker.md` |
| `youtube-producer` | YouTube producer AP runtime: primary/backfill polling, outbox production, active-active poll coordination (Seoul b + main-host c + Osaka host-native a/d), readiness, and Holodex photo sync (AP-C, singleton lease) | YouTube poller/outbox production, active-active coordination/readiness, photo sync runtime | PostgreSQL, Valkey | bot command routing, proactive notification egress, alarm dispatch queue consumption | `services/youtube-producer.md` |

## Split Rules

- Cross-service APIs must use documented contracts under `docs/current/contracts/` and `hololive/hololive-shared/pkg/contracts/*`.
- Service-to-service `internal` package imports are not allowed as an ownership shortcut.
- Queue/PubSub changes must update `CONTRACT_MAP.md`, `QUEUE_AND_PUBSUB_CONTRACTS.md`, and affected service docs.
- Unclear ownership is marked `검토 필요` in the service doc instead of being silently assigned.
- Runtime binaries must use role-specific config loaders where available (`LoadBotRuntime`, `LoadAlarmWorkerRuntime`, `LoadAdminAPIRuntime`, `LoadLLMSchedulerRuntime`, `LoadYouTubeProducerRuntime`) so ownership drift fails during startup rather than after queues or egress clients are constructed.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
go test ./hololive/hololive-shared/pkg/config/internal/settings -run 'Runtime|NonEgress|AdminAPI'
```
