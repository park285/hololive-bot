# Service Ownership

이 문서는 현재 runtime 기준의 서비스 소유권 SSOT입니다.

## Rules

- 새 기능은 먼저 이 문서에서 owner를 확인한 뒤 구현합니다.
- owner가 명확하지 않은 기능은 PR 전에 owner를 결정합니다.
- 한 서비스가 다른 서비스의 `internal` package를 import해서 소유권을 우회하면 안 됩니다.

## Summary

| Service | Owns | Provides | Consumes | Must not own |
|---|---|---|---|---|
| bot | Kakao/Iris ingress, command routing | `/webhook/iris`, optional internal trigger receiver | Iris, llm-scheduler, alarm API | admin API, alarm checking, dispatch consuming |
| admin-api | Admin control plane | admin HTTP API | llm-scheduler trigger, alarm API, DB/cache | Kakao command parsing, Iris webhook |
| alarm-worker | Alarm checking and queue publishing | alarm API 검토 필요, health | DB/cache, Holodex/Chzzk/Twitch | Iris send, command parsing |
| dispatcher-go | Alarm dispatch queue consuming | health/ready | Valkey, Iris | alarm checking, admin API |
| llm-scheduler | LLM scheduling, member news, major events | trigger/membernews/majorevent internal API | DB/cache, Iris delivery | command parsing, admin dashboard |
| stream-ingester | photo sync, ingestion runtime | health | DB/cache, external fetchers | bot command routing |
| youtube-scraper | YouTube scraping/polling | health | DB/cache, external YouTube/scraper | admin API |
