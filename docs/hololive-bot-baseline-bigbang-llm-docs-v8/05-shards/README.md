# Shard index

아래 shard 문서 중 하나를 Worker에게 배정합니다. 정적 shard가 너무 크면 `docs/hololive-bot-baseline-bigbang-llm-docs-v8/tools/generate-function-budget-shards.py`로 만든 `AUTO-###` card를 우선합니다.

| ID | Title | Risk | File |
|---|---|---|---|
| ADM-01 | ADM-01 — admin-api runtime builder | R5 | `05-shards/ADM/ADM-01-runtime-builder.md` |
| ADM-02 | ADM-02 — admin-api router and middleware | R2 | `05-shards/ADM/ADM-02-router-middleware.md` |
| ADM-03 | ADM-03 — admin-api runtime lifecycle | R5 | `05-shards/ADM/ADM-03-runtime-lifecycle.md` |
| ADM-04 | ADM-04 — admin-api member handlers | R2 | `05-shards/ADM/ADM-04-member-handlers.md` |
| ADM-05 | ADM-05 — admin-api secondary handlers | R2/R3 | `05-shards/ADM/ADM-05-room-milestone-template-settings.md` |
| ADM-06 | ADM-06 — admin-api system stats helper | R3 | `05-shards/ADM/ADM-06-system-stats.md` |
| AW-01 | AW-01 — alarm-worker app runtime | R3/R5 | `05-shards/AW/AW-01-app-runtime.md` |
| AW-02 | AW-02 — Chzzk checker main flow | R3 | `05-shards/AW/AW-02-chzzk-checker-flow.md` |
| AW-03 | AW-03 — Chzzk reflection helpers | R1 | `05-shards/AW/AW-03-chzzk-reflection.md` |
| AW-04 | AW-04 — alarm checker common helpers | R3 | `05-shards/AW/AW-04-checker-common.md` |
| AW-05 | AW-05 — alarm notifier | R3 | `05-shards/AW/AW-05-notifier.md` |
| AW-06 | AW-06 — Twitch/YouTube notification builders | R3 | `05-shards/AW/AW-06-twitch-youtube-builders.md` |
| AW-07 | AW-07 — YouTube checker main flow | R3 | `05-shards/AW/AW-07-youtube-checker-flow.md` |
| AW-08 | AW-08 — alarm runtime scheduler | R5 | `05-shards/AW/AW-08-runtime-scheduler.md` |
| AW-09 | AW-09 — alarm cache recovery scheduler | R5 | `05-shards/AW/AW-09-cache-recovery-scheduler.md` |
| DSP-01 | DSP-01 — dispatcher config load | R3 | `05-shards/DSP/DSP-01-config-load.md` |
| DSP-02 | DSP-02 — dispatcher config validation | R3 | `05-shards/DSP/DSP-02-config-validation.md` |
| DSP-03 | DSP-03 — dispatcher runtime builder | R5 | `05-shards/DSP/DSP-03-runtime-builder.md` |
| DSP-04 | DSP-04 — dispatcher runtime loop | R5 | `05-shards/DSP/DSP-04-runtime-loop.md` |
| DSP-05 | DSP-05 — dispatcher readiness/wakeup/render | R3/R5 | `05-shards/DSP/DSP-05-readiness-wakeup-render.md` |
| DSP-06 | DSP-06 — dispatcher dispatch group | R4 | `05-shards/DSP/DSP-06-dispatch-group.md` |
| DSP-07 | DSP-07 — dispatcher failure plan | R4 | `05-shards/DSP/DSP-07-dispatch-failure-plan.md` |
| DSP-08 | DSP-08 — dispatcher retry backoff | R2 | `05-shards/DSP/DSP-08-retry-backoff.md` |
| KAK-01 | KAK-01 — test_db_integration main | R2 | `05-shards/KAK/KAK-01-test-db-integration-main.md` |
| KAK-02 | KAK-02 — alarm formatters | R1 | `05-shards/KAK/KAK-02-alarm-formatters.md` |
| KAK-03 | KAK-03 — directory/profile/stats/streams formatters | R1 | `05-shards/KAK/KAK-03-other-formatters.md` |
| KAK-04 | KAK-04 — message parsers | R1 | `05-shards/KAK/KAK-04-message-parsers.md` |
| KAK-05 | KAK-05 — bot app bootstrap | R5 | `05-shards/KAK/KAK-05-app-bootstrap.md` |
| KAK-06 | KAK-06 — bot app router/runtime | R5 | `05-shards/KAK/KAK-06-app-router-runtime.md` |
| KAK-07 | KAK-07 — bot core lifecycle/transport | R5 | `05-shards/KAK/KAK-07-bot-core.md` |
| KAK-08 | KAK-08 — command handlers | R2/R3 | `05-shards/KAK/KAK-08-command-handlers.md` |
| KAK-09 | KAK-09 — matcher and stream services | R3/R4 | `05-shards/KAK/KAK-09-matcher-stream-services.md` |
| LLM-01 | LLM-01 — internal route registration | R2 | `05-shards/LLM/LLM-01-internal-routes.md` |
| LLM-02 | LLM-02 — llm scheduler bootstrap | R5 | `05-shards/LLM/LLM-02-bootstrap.md` |
| LLM-03 | LLM-03 — OpenAI client helpers | R3 | `05-shards/LLM/LLM-03-openai-client.md` |
| LLM-04 | LLM-04 — schedulerkit and repositories | R3/R5 | `05-shards/LLM/LLM-04-schedulerkit-repositories.md` |
| LLM-05 | LLM-05 — major event scheduler | R3 | `05-shards/LLM/LLM-05-major-event-scheduler.md` |
| LLM-06 | LLM-06 — major event scraper | R3/R4 | `05-shards/LLM/LLM-06-major-event-scraper.md` |
| LLM-07 | LLM-07 — major event summarizer | R3 | `05-shards/LLM/LLM-07-major-event-summarizer.md` |
| LLM-08 | LLM-08 — membernews filter/service/scheduler | R3/R4 | `05-shards/LLM/LLM-08-membernews-filter-service.md` |
| LLM-09 | LLM-09 — membernews summarizer | R3 | `05-shards/LLM/LLM-09-membernews-summarizer.md` |
| SH-01 | SH-01 — shared internal dbx and retry | R3/R5 | `05-shards/SH/SH-01-internal-dbx-retry.md` |
| SH-02 | SH-02 — shared config package | R3 | `05-shards/SH/SH-02-config.md` |
| SH-03 | SH-03 — shared domain scanner/stats/youtube content | R2 | `05-shards/SH/SH-03-domain-core.md` |
| SH-04 | SH-04 — template sample data | R0 | `05-shards/SH/SH-04-template-sample-data.md` |
| SH-05 | SH-05 — shared providers | R3/R5 | `05-shards/SH/SH-05-providers.md` |
| SH-06 | SH-06 — server middleware/runtime/helpers | R2/R3 | `05-shards/SH/SH-06-server-middleware-runtime.md` |
| SH-07 | SH-07 — ACL and activity services | R3/R4 | `05-shards/SH/SH-07-acl-activity.md` |
| SH-08 | SH-08 — alarm cache warm | R4 | `05-shards/SH/SH-08-alarm-cache-warm.md` |
| SH-09 | SH-09 — alarm dedup | R4 | `05-shards/SH/SH-09-alarm-dedup.md` |
| SH-10 | SH-10 — alarm dispatch outbox | R4 | `05-shards/SH/SH-10-dispatchoutbox.md` |
| SH-11 | SH-11 — alarm queue | R4 | `05-shards/SH/SH-11-alarm-queue.md` |
| SH-12 | SH-12 — alarm targets/tier/client/repository | R3/R4 | `05-shards/SH/SH-12-alarm-targets-tier-client.md` |
| SH-13 | SH-13 — auth service | R4 | `05-shards/SH/SH-13-auth-service.md` |
| SH-14 | SH-14 — cache service | R4 | `05-shards/SH/SH-14-cache-service.md` |
| SH-15 | SH-15 — chzzk/holodex/member/twitch clients | R3/R4 | `05-shards/SH/SH-15-external-api-clients.md` |
| YT-01 | YT-01 — youtube service and scheduler | R3/R4 | `05-shards/YT/YT-01-youtube-service-scheduler.md` |
| YT-02 | YT-02 — youtube outbox | R4 | `05-shards/YT/YT-02-youtube-outbox.md` |
| YT-03 | YT-03 — youtube poller/scraper/tracking | R4 | `05-shards/YT/YT-03-youtube-poller-scraper-tracking.md` |
| ING-01 | ING-01 — stream ingester | R3 | `05-shards/ING/ING-01-stream-ingester.md` |
| SGO-01 | SGO-01 — shared-go | R3 | `05-shards/SGO/SGO-01-shared-go.md` |
| SWP-01 | SWP-01 — final over-budget sweep | R1~R5 | `05-shards/SWP/SWP-01-final-budget-sweep.md` |
| SWP-02 | SWP-02 — baseline residue sweep | R1 | `05-shards/SWP/SWP-02-residue-sweep.md` |
