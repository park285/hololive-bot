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

## Shared Package Retention

`hololive-shared/pkg`는 외부 안정 API 전체가 아니라 monorepo 내부 cross-runtime 계약면입니다. 단일 runtime만 소비하는 실행 구현은 해당 module의 `internal/`로 이동하지만, 다음 범주는 shared에 남습니다.

- 진성 다중 소비자: `service/delivery`, `service/notification/alarmservice`, `service/scraper/**`, `service/youtube/outbox/{analytics,telemetry}`, `service/youtube/poller/runtime/scheduler`.
- producer/consumer 양측 계약면: `service/youtube/outbox/{store,format,deliverysql,dispatchstate}`.
- shared 내부 소비 그래프가 여러 runtime에 걸치는 기반 패키지: `service/youtube/{admission,batchrepo,poller/runtime,tracking/observation}`, `service/youtube/outbox/timeline`.
- alarm HTTP migration facade가 공동으로 사용하는 계약·handler: `service/alarm/{checker,queue,handoff,dispatchoutbox}`. 이 범주는 facade 제거 뒤에도 실제 다중 소비가 남는지 다시 확인하며 자동 삭제하지 않습니다.

YouTube dispatcher와 poller 구현처럼 단일 owner로 확정된 코드는 각각 `hololive-alarm-worker/internal/egress/youtubedispatch`와 `hololive-youtube-collector/internal/runtime/pollers`가 소유합니다. public package 잔류는 구현 ownership을 공유한다는 뜻이 아니며, 새 single-owner 실행 구현을 `hololive-shared/pkg`에 추가할 근거로 사용할 수 없습니다.

## Validation

```bash
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
go test ./hololive/hololive-shared/pkg/config/settings -run 'Runtime|NonEgress|AdminAPI'
```
