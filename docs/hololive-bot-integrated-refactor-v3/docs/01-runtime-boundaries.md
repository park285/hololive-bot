# 01. Runtime Boundaries

## 원칙

리팩토링은 runtime ownership을 흐리게 만들면 실패입니다. 각 runtime은 자기 책임만 가져야 합니다.

## bot

소유:
- Kakao/Iris webhook ingress
- command routing
- user-facing reply orchestration

금지:
- alarm scheduler loop
- proactive dispatch queue consume
- admin control plane

리팩토링 방향:
- `MessageIngress`는 입력 정규화만 담당
- `CommandRouter`는 command 실행과 실행 로그의 단일 책임 지점
- external API client 호출은 command handler/service로 이동
- 실패 로그 중복 제거

## alarm-worker

소유:
- alarm checker
- scheduler
- dispatch queue publish
- proactive notification egress
- YouTube outbox dispatch final send

금지:
- Kakao command parsing
- YouTube scraping/outbox production
- LLM summary generation

리팩토링 방향:
- scheduler loop와 platform check 분리
- check 실패와 dispatch 실패 분리
- outbox claim/render/send/finalize 분리
- egress owner lease 상태를 로그에 남김

## dispatcher-go

소유:
- legacy queue consume lifecycle
- retry queue / DLQ movement
- explicit profile일 때만 Iris send

금지:
- default production proactive egress
- alarm mutation
- LLM/news scheduling

리팩토링 방향:
- render/send/mark/retry/dlq/quarantine 이벤트 분리
- queue payload 원문 로그 금지
- room, envelope count, retry attempt 중심 로그

## llm-scheduler

소유:
- major event/member news scheduling
- LLM summary
- notification intent production

금지:
- Kakao webhook ingress
- final proactive Iris/Kakao egress
- alarm checker loop

리팩토링 방향:
- prompt build/provider call/result validate/intent write 분리
- prompt 원문 로그 금지
- response 전문 로그 금지

## stream-ingester

소유:
- photo sync
- ingestion-adjacent runtime

금지:
- dedicated YouTube scraping ownership
- proactive notification egress

## youtube-scraper

소유:
- YouTube scraping/polling
- `youtube_notification_outbox` production

금지:
- final Iris/Kakao send
- alarm check
- queue consume
