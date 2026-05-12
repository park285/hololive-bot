# Contract Map

이 문서는 내부 서비스 간 계약의 SSOT입니다.

| Contract | Provider | Consumers | Type | Path/Event/Queue | Package | Version | Runbook |
|---|---|---|---|---|---|---|---|
| membernews.digest | llm-scheduler | bot | HTTP JSON | `POST /internal/membernews/digest` | `contracts/membernews` | v1 | `runbooks/llm-scheduler.md` |
| membernews.subscription | llm-scheduler | bot | HTTP JSON | `/internal/membernews/subscriptions` | `contracts/membernews`, `contracts/subscription` | v1 | `runbooks/llm-scheduler.md` |
| majorevent.subscription | llm-scheduler | bot | HTTP JSON | `/internal/majorevent/subscriptions` | `contracts/majorevent`, `contracts/subscription` | v1 | `runbooks/llm-scheduler.md` |
| trigger.manual | llm-scheduler | admin-api | HTTP JSON | `/internal/trigger/*` | `contracts/trigger` | v1 | `runbooks/llm-scheduler.md` |
| alarm.dispatch | alarm-worker | dispatcher-go | Valkey queue | `alarm:dispatch:queue` | `contracts/alarm` | v1 | `runbooks/dispatcher-go.md` |
| config.update | admin-api | bot, alarm-worker, llm-scheduler | Valkey PubSub | `config:update` | `contracts/settings` | v1 | `QUEUE_AND_PUBSUB_CONTRACTS.md` |
| iris.webhook | Iris | bot | external HTTP/H3 | `/webhook/iris` | `iris-client-go` | external | `runbooks/bot.md` |
| iris.control | bot/dispatcher/llm-scheduler | Iris | external HTTP/H3 | Iris API | `iris-client-go` | external | `runbooks/iris-connectivity.md` |
