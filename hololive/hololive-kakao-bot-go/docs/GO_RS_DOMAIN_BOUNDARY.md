# Go/Rust 도메인 경계 문서

> 2026-03-07 운영 기준. hololive-kakao-bot-go와 hololive-rs 간 역할 분류.

---

## 1. 운영 상태

| 영역 | Rust | Go | 운영 상태 |
|------|------|-----|----------|
| RSS 피드 수집 | scraper-app | - | Rust ON |
| YouTube 폴링 (알림용) | alarm-app YouTubeChecker | ~~CheckUpcomingStreams~~ | Rust ON, Go OFF |
| Chzzk 폴링 (알림용) | alarm-app ChzzkChecker | ~~CheckChzzkStreams~~ | Rust ON, Go OFF |
| Twitch 폴링 (알림용) | alarm-app TwitchChecker | ~~CheckTwitchStreams~~ | Rust OFF, Go OFF |
| 알림 Dedup + 큐 발행 | Notifier → LPUSH | - | Rust ON |
| 알림 큐 소비 + 발송 | dispatcher-app | - | Rust ON |
| 알람 CRUD | - | AlarmService (bot 내부 API) | Go ON |
| 명령어 핸들링 | - | bot/ + command/ | Go ON |
| Admin REST API | - | server/ | Go ON |
| YouTube 통계/스크래핑 | - | youtube-producer runtime | Go ON |
| YouTube 캐시 조회 | - | holodex.Service (Holodex API) | Go ON |
| LLM 스케줄러 | - | majorevent/ + membernews/ | Go ON |
| Delivery Outbox | - | delivery.Dispatcher | Go ON |

## 2. 공유 상태 (Valkey)

| 키 패턴 | 쓰기 | 읽기 | 프로토콜 |
|---------|------|------|---------|
| `alarm:dispatch:queue` | **Rust** LPUSH | **Rust** BRPOP | JSON AlarmQueueEnvelope (v1) |
| `alarm:channel_registry` | **Rust** | Go (보조) | SET |
| `alarm:channel_subscribers:*` | **Go** | Go | SET of roomIDs |
| `notified:claim:*` | **Rust** SET | **Rust** DEL | String flag |
| `notified:{streamID}` | **Go** | Go | HASH |
| `notified:chzzk:live:*` | **Rust** | Rust | String flag |
| `notified:twitch:live:*` | **Rust** | Rust | String flag |
| 캐시 키 (live/upcoming/channel) | **Go** | Go | JSON |

## 3. 공유 DB 테이블

| 테이블 | Rust | Go | 분리 후 Go 소유 서비스 |
|--------|------|-----|----------------------|
| `major_events` | **Write** | Read | llm-scheduler (read), admin-api (read) |
| `alarms` | - | **Write** | hololive-bot |
| `youtube_stats` | - | **Write** | youtube-producer |
| `members`, `channels` | Read | **Write** | Bot (유지) |
| `member_news*` | - | **Write** | llm-scheduler |
| `delivery_outbox` | - | **Write** | llm-scheduler |

## 4. Go 인터페이스 경계

P0에서 정의한 소비자별 분리 인터페이스 (`internal/domain/interfaces.go`):

| 인터페이스 | 소비자 | 구현체 |
|-----------|--------|--------|
| `AlarmCRUD` | Bot 커맨드, Admin API | `notification.AlarmService` |
| `AlarmDispatchState` | `youtube.Scheduler` | `notification.AlarmService` |
| `StreamProvider` | Bot 커맨드, Admin API | `holodex.Service` |

## 5. 레거시 코드 정리 상태

정리 완료:
- Go alarm checking 코드 제거 (`CheckUpcomingStreams` / `CheckChzzkStreams` / `CheckTwitchStreams` 경로 삭제)
- `AlarmChecker` 인터페이스 제거
- Admin/Bot의 `majorEventScrape*` 설정 API(Go 스크래퍼 제어) 제거

유지:
- Rust alarm-app → Rust dispatcher(queue consumer) 경계 유지
- Go llm-scheduler는 major event **요약/발송**만 담당 (스크래핑은 Rust 소유)

## 6. 확정 아키텍처 (하이브리드)

```
┌─ Rust ──────────────────────────────────────────────┐
│  scraper-app: RSS → major_events DB                  │
│  alarm-app:   폴링 → dedup → LPUSH queue             │
│  dispatcher-app: BRPOP → 렌더 → Iris 발송             │
└──────────────────────────────────────────────────────┘
              ↓ alarm:dispatch:queue (Rust 내부 소비)
┌─ Go ────────────────────────────────────────────────┐
│                                                      │
│  hololive-bot (명령어 + 웹훅)                         │
│    Iris 웹훅 → 커맨드 라우팅 → 응답                    │
│    Alarm CRUD API (/internal/alarm/*)                │
│                                                      │
│  admin-api (관리)                                     │
│    REST API + Auth + WebSocket                       │
│    설정 → Pub/Sub, 트리거 → HTTP 내부 API             │
│    Alarm CRUD → hololive-bot HTTP 클라이언트          │
│                                                      │
│  llm-scheduler (LLM 기능)                             │
│    MajorEvent/MemberNews 스케줄러 + Delivery          │
│                                                      │
│  youtube-producer (ingestion 단독 소유)                 │
│    YouTube 통계 + 스크래핑 + PhotoSync                 │
└──────────────────────────────────────────────────────┘
```

## 7. 아키텍처 확정 (2026-03-01)

하이브리드 구조를 최종 아키텍처로 확정. Go → Rust 전면 전환 계획(Phase 2~6)은 폐기.
- **근거**: bot/youtube-producer/admin/llm-sched는 Go net/http 생태계(h2c, SOCKS5, HTTP/2 토글, per-host 풀)에 강하게 의존
- **Rust 소유**: alarm-checker, scraper-rss, dispatcher (compute 집약)
- **Go 소유**: bot, youtube-producer, admin-api, llm-scheduler (네트워크 집약)
- **운영 메모 (2026-03-07)**: ingestion fallback은 `hololive-bot`에서 제거되었고, 현재는 `youtube-producer`만 ingestion 런타임을 소유한다.
