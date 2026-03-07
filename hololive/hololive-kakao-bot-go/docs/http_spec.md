# Hololive Bot API 스펙

> 마지막 업데이트: 2026-03-08

클라이언트(Tauri 앱, Admin Dashboard)가 호출하는 bot 통합 API 기준 명세입니다.

## 기본 정보

| 항목 | 값 |
|------|-----|
| Base URL | `http://localhost:30001` (또는 배포 환경 도메인) |
| Prefix | `/api/holo` |
| 인증 | 기본적으로 `X-API-Key` 헤더 필요. 단, `GET /api/holo/streams/live`, `GET /api/holo/streams/upcoming` 는 인증 불필요 |
| 프로토콜 | HTTP/2 Cleartext (H2C) |

---

## 목차

1. [스트림 API](#1-스트림-api)
2. [채널 API](#2-채널-api)
3. [멤버 API](#3-멤버-api)
4. [알람 API](#4-알람-api)
5. [통계 API](#5-통계-api)
6. [프로필 API](#6-프로필-api)
7. [설정 API](#7-설정-api)
8. [헬스체크](#8-헬스체크)

---

## 공통 응답 형식

### 성공 응답
```json
{
  "status": "ok",
  "데이터필드": { ... }
}
```

### 에러 응답
```json
{
  "error": "에러 메시지",
  "message": "추가 설명(선택)",
  "code": "machine_readable_code(선택)",
  "hint": "복구 가이드(선택)"
}
```

### HTTP 상태 코드
| 코드 | 설명 |
|------|------|
| 200 | 성공 |
| 201 | 생성 성공 |
| 400 | 잘못된 요청 (파라미터 오류) |
| 401 | 미인증 (API Key 없음) |
| 403 | 권한 없음 (잘못된 API Key) |
| 404 | 리소스 없음 |
| 409 | 상태 충돌 (중복 생성/진행 중 작업) |
| 410 | 제거된(legacy) API 사용 |
| 500 | 서버 오류 |
| 503 | 일시적 준비 안 됨 (백그라운드 동기화 대기) |

**인증 실패 응답 예시:**
```json
{
  "error": "unauthorized",
  "message": "API key required"
}
```

```json
{
  "error": "forbidden",
  "message": "invalid API key"
}
```

> `409`는 현재 중복 생성/동시 실행 충돌에 사용됩니다 (예: room 중복 생성, major event trigger 중복 실행).

---

## 1. 스트림 API

### 1.1 생방송 목록 조회

현재 진행 중인 Hololive 생방송 목록을 반환합니다.

> 인증 불필요

```
GET /api/holo/streams/live?org={ORG}
```

**파라미터:**
| 이름 | 타입 | 필수 | 기본값 | 설명 |
|------|------|------|--------|------|
| org | string | ❌ | `hololive` | 조회 대상 org (`hololive`, `vspo`, `stellive`, `indie`, `all`) |

> `org` 파라미터가 전달되었는데 빈 문자열/공백이면 `400` 반환

**응답:**
```json
{
  "status": "ok",
  "org": "hololive",
  "streams": [
    {
      "id": "dQw4w9WgXcQ",
      "title": "【雑談】お話しよう！",
      "channelId": "UC1DCedRgGHBdm81E1llLhOQ",
      "channelName": "Miko Ch.",
      "status": "live",
      "startScheduled": "2026-01-04T10:00:00Z",
      "startActual": "2026-01-04T10:05:00Z",
      "thumbnail": "https://i.ytimg.com/vi/dQw4w9WgXcQ/mqdefault.jpg",
      "link": "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
      "channel": {
        "id": "UC1DCedRgGHBdm81E1llLhOQ",
        "name": "Miko Ch.",
        "englishName": "Sakura Miko",
        "photo": "https://yt3.ggpht.com/..."
      }
    }
  ]
}
```

---

### 1.2 예정 방송 목록 조회

향후 24시간 내 예정된 방송 목록을 반환합니다.

> 인증 불필요

```
GET /api/holo/streams/upcoming?org={ORG}
```

**파라미터:**
| 이름 | 타입 | 필수 | 기본값 | 설명 |
|------|------|------|--------|------|
| org | string | ❌ | `hololive` | 조회 대상 org (`hololive`, `vspo`, `stellive`, `indie`, `all`) |

> `org` 파라미터가 전달되었는데 빈 문자열/공백이면 `400` 반환

**응답:**
```json
{
  "status": "ok",
  "org": "hololive",
  "streams": [
    {
      "id": "video_id",
      "title": "방송 제목",
      "status": "upcoming",
      "startScheduled": "2026-01-04T15:00:00Z",
      ...
    }
  ]
}
```

---

## 2. 채널 API

### 2.1 채널 조회 (배치)

여러 채널 정보를 한 번에 조회합니다. (최대 100개)

```
GET /api/holo/channels?channelIds={ID1},{ID2},{ID3}...
```

**파라미터:**
| 이름 | 타입 | 필수 | 설명 |
|------|------|------|------|
| channelIds | string | ✅ | 쉼표로 구분된 채널 ID 목록 |

> `channelIds`는 최대 100개까지 허용되며 초과 시 `400` 반환

**응답:**
```json
{
  "status": "ok",
  "channels": [
    {
      "id": "UC1DCedRgGHBdm81E1llLhOQ",
      "name": "Sakura Miko",
      "photo": "https://yt3.ggpht.com/..."
    }
  ]
}
```

---

### 2.2 레거시 단일 `channelId` 동작

`channelId` 단일 조회는 제거되었습니다.

```
GET /api/holo/channels?channelId={CHANNEL_ID}
```

**응답:** `410 Gone`
```json
{
  "error": "Legacy channelId query is no longer supported",
  "hint": "use channelIds query parameter"
}
```

---

### 2.3 채널 검색

이름으로 Hololive 채널을 검색합니다.

```
GET /api/holo/channels/search?q={QUERY}
```

**파라미터:**
| 이름 | 타입 | 필수 | 설명 |
|------|------|------|------|
| q | string | ✅ | 검색 쿼리 (영문명/일본어명) |

**응답:**
```json
{
  "status": "ok",
  "channels": [
    {
      "id": "UC1DCedRgGHBdm81E1llLhOQ",
      "name": "Miko Ch. さくらみこ",
      "englishName": "Sakura Miko",
      ...
    }
  ]
}
```

---

## 3. 멤버 API

### 3.1 멤버 목록 조회

DB에 등록된 모든 멤버 목록을 반환합니다.

```
GET /api/holo/members
```

**응답:**
```json
{
  "status": "ok",
  "members": [
    {
      "id": 1,
      "name": "Sakura Miko",
      "channelId": "UC1DCedRgGHBdm81E1llLhOQ",
      "nameJa": "さくらみこ",
      "nameKo": "사쿠라 미코",
      "isGraduated": false,
      "aliases": {
        "ko": ["미코", "미코짱"],
        "ja": ["みこ", "みっころね"]
      }
    }
  ]
}
```

---

### 3.2 멤버 추가

```
POST /api/holo/members
Content-Type: application/json

{
  "name": "New Member",
  "channelId": "UCxxxxxxx",
  "nameJa": "日本語名",
  "nameKo": "한국어명",
  "isGraduated": false,
  "aliases": {
    "ko": [],
    "ja": []
  }
}
```

**응답 코드:**
- `201`: 생성 성공

**응답 예시 (201):**
```json
{
  "status": "ok",
  "message": "Member added successfully"
}
```

---

### 3.3 별칭 추가

```
POST /api/holo/members/:id/aliases
Content-Type: application/json

{
  "type": "ko",       // "ko" 또는 "ja"
  "alias": "새별칭"
}
```

---

### 3.4 별칭 삭제

```
DELETE /api/holo/members/:id/aliases
Content-Type: application/json

{
  "type": "ko",
  "alias": "삭제할별칭"
}
```

---

### 3.5 졸업 상태 변경

```
PATCH /api/holo/members/:id/graduation
Content-Type: application/json

{
  "isGraduated": true
}
```

---

### 3.6 채널 ID 변경

```
PATCH /api/holo/members/:id/channel
Content-Type: application/json

{
  "channelId": "UC새채널ID"
}
```

---

### 3.7 멤버 이름 변경

```
PATCH /api/holo/members/:id/name
Content-Type: application/json

{
  "name": "New Name"
}
```

---

## 4. 알람 API

### 4.1 알람 목록 조회

```
GET /api/holo/alarms
```

**응답:**
```json
{
  "status": "ok",
  "alarms": [
    {
      "roomId": "room123",
      "channelId": "UC1DCedRgGHBdm81E1llLhOQ",
      "memberName": "Sakura Miko",
      "createdAt": "2026-01-01T00:00:00Z"
    }
  ]
}
```

---

### 4.2 알람 삭제

```
DELETE /api/holo/alarms?roomId={ROOM_ID}&channelId={CHANNEL_ID}
```

---

## 5. 통계 API

### 5.1 봇 통계

```
GET /api/holo/stats
```

**응답:**
```json
{
  "status": "ok",
  "members": 80,
  "alarms": 1234,
  "rooms": 567,
  "version": "1.2.0",
  "uptime": "72h30m15s"
}
```

---

### 5.2 채널 통계 (YouTube)

```
GET /api/holo/stats/channels
```

**응답:**
```json
{
  "status": "ok",
  "stats": {
    "UC1DCedRgGHBdm81E1llLhOQ": {
      "subscriberCount": 2000000,
      "viewCount": 500000000
    }
  }
}
```

**오류 응답:**
- `500`: 캐시/DB 조회 실패
- `503`: 스냅샷 미준비

```json
{
  "error": "Channel stats snapshot not ready",
  "code": "channel_stats_snapshot_not_ready",
  "hint": "retry later after background poller sync"
}
```

---

### 5.3 마일스톤 목록

```
GET /api/holo/milestones
```

**오류 응답:**
- `503`: Stats repository 미초기화 (`{"error":"Stats repository not available"}`)
- `500`: 조회 처리 실패

---

### 5.4 마일스톤 근접 멤버

```
GET /api/holo/milestones/near
```

**오류 응답:**
- `503`: Stats repository 미초기화 (`{"error":"Stats repository not available"}`)
- `500`: 조회 처리 실패

---

### 5.5 마일스톤 통계

```
GET /api/holo/milestones/stats
```

**오류 응답:**
- `503`: Stats repository 미초기화 (`{"error":"Stats repository not available"}`)
- `500`: 조회 처리 실패

---

## 6. 프로필 API

### 6.1 프로필 조회 (채널 ID)

```
GET /api/holo/profiles?channelId={CHANNEL_ID}
```

**응답:**
```json
{
  "status": "ok",
  "profile": {
    "slug": "sakura-miko",
    "english_name": "Sakura Miko",
    "japanese_name": "さくらみこ",
    "catchphrase": "Elite Miko!",
    "description": "...",
    "data_entries": [
      { "label": "Birthday", "value": "March 5" }
    ],
    "social_links": [
      { "label": "Twitter", "url": "https://twitter.com/sakuramiko35" }
    ],
    "official_url": "https://hololive.hololivepro.com/talents/sakura-miko/"
  },
  "translated": {
    "display_name": "사쿠라 미코",
    "catchphrase": "엘리트 미코!",
    "summary": "홀로라이브 소속 ...",
    "highlights": ["엘리트", "GTA"],
    "data": [
      { "label": "생일", "value": "3월 5일" }
    ]
  }
}
```

> 번역 데이터 로드 실패 시 부분 성공으로 내려가지 않고 `500`을 반환합니다.

---

### 6.2 프로필 조회 (이름)

```
GET /api/holo/profiles/name?name={ENGLISH_NAME}
```

---

## 7. 설정 API

### 7.1 설정 조회

```
GET /api/holo/settings
```

---

### 7.2 설정 업데이트

```
POST /api/holo/settings
Content-Type: application/json

{
  "key": "value"
}
```

---

### 7.3 LLM 스케줄러 설정/실행 트리거

```
POST /api/holo/settings/llm
Content-Type: application/json

{
  "memberNewsWeeklyRunNow": true
}
```

**요청 필드:**
| 이름 | 타입 | 필수 | 설명 |
|------|------|------|------|
| memberNewsWeeklyRunNow | boolean | ✅ | `true`일 때 member news 주간 다이제스트 즉시 실행 |

> `majorEventScrapeHourKST`, `majorEventScrapeRunNow`는 2026-03-01부터 제거되었습니다 (Rust scraper 소유).

**응답 예시 (200):**
```json
{
  "status": "ok",
  "message": "LLM settings updated",
  "runtime": {
    "membernews_weekly_run_now": {
      "published": true
    }
  }
}
```

**검증 에러 예시 (400):**
- `memberNewsWeeklyRunNow must be true when provided`

**Legacy 에러 예시 (410):**
- `majorEventScrape* controls are no longer supported; major event scraping is owned by hololive-rs`

---

### 7.4 활동 로그

```
GET /api/holo/logs
```

---

### 7.5 방 이름 매핑

```
POST /api/holo/names/room
Content-Type: application/json

{
  "roomId": "room123",
  "name": "테스트 방"
}
```

---

### 7.6 사용자 이름 매핑

```
POST /api/holo/names/user
Content-Type: application/json

{
  "userId": "user123",
  "name": "테스트 사용자"
}
```

---

## 8. 헬스체크

### 8.1 서버 상태

```
GET /health
```

> **참고**: 인증 불필요

**응답:**
```json
{
  "status": "ok",
  "version": "1.2.0",
  "uptime": "72h30m15s",
  "goroutines": 42
}
```

---

### 8.2 Prometheus 메트릭

```
GET /metrics
```

> **참고**: 인증 불필요

---

## 인증 예시

### curl

```bash
curl "http://localhost:30001/api/holo/streams/live"
```

### TypeScript (Tauri 앱)

```typescript
const response = await fetch('http://localhost:30001/api/holo/streams/live', {
});
const data = await response.json();
```

### Go

```go
req, _ := http.NewRequest("GET", "http://localhost:30001/api/holo/streams/live", nil)
resp, _ := http.DefaultClient.Do(req)
```

---

## 변경 이력

| 날짜 | 변경 내용 |
|------|----------|
| 2026-01-04 | 초기 문서 작성 |
| 2026-01-04 | `/users/live` Holodex 내부 메서드 추가 (`GetChannelsLiveStatus`) |
| 2026-02-28 | `POST /api/holo/settings/llm` 추가 (LLM scheduler 제어) |
| 2026-03-01 | channel 단일 `channelId` 조회 제거(410), fail-fast 응답 정책 반영 |
| 2026-03-08 | `GET /api/holo/streams/live`, `GET /api/holo/streams/upcoming` 공개 조회로 전환 |
