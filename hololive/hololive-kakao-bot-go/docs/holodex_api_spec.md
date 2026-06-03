# Holodex API 엔드포인트 스펙 문서

> 마지막 업데이트: 2026-03-06

## 목차
1. [개요](#개요)
2. [엔드포인트 목록](#엔드포인트-목록)
3. [상세 스펙](#상세-스펙)
4. [사용 예시](#사용-예시)

---

## 개요

이 문서는 `hololive-kakao-bot-go` 프로젝트에서 사용하는 Holodex API 엔드포인트를 정의합니다.

**기본 정보**
- Base URL: `https://holodex.net/api/v2`
- 인증: `X-APIKEY` 헤더
- User-Agent: `Linux; api.capu.blog`

---

## 엔드포인트 목록

| 메서드 | 엔드포인트 | Go 함수 | 설명 |
|--------|------------|---------|------|
| GET | `/live` | `GetLiveStreams` | 현재 생방송 중인 Hololive 스트림 조회 |
| GET | `/live` | `GetUpcomingStreams` | 예정된 Hololive 스트림 조회 |
| GET | `/live` | `GetChannelSchedule` | 특정 채널의 방송 일정 조회 |
| GET | `/users/live` | `GetChannelsLiveStatus` | **[NEW]** 특정 채널들의 빠른 상태 조회 |
| GET | `/channels` | `SearchChannels` | Hololive 채널 검색 |
| GET | `/channels` | `fetchHololiveChannelList` | 전체 Hololive 채널 목록 |
| GET | `/channels/{id}` | `GetChannel` | 특정 채널 상세 정보 |

---

## 상세 스펙

### 1. `/users/live` - 채널 생방송 상태 빠른 조회 (NEW)

> Holodex에서 권장하는 고성능 엔드포인트

#### 1.1 기본 정보

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/users/live` |
| Go 함수 | `holodex.Service.GetChannelsLiveStatus(ctx, channelIDs []string)` |
| 캐시 TTL | 30초 |

#### 1.2 요청 파라미터

| 파라미터 | 타입 | 필수 | 설명 |
|----------|------|------|------|
| `channels` | string | 예 | 쉼표로 구분된 YouTube 채널 ID 목록 |

#### 1.3 응답

```json
[
  {
    "id": "video_id",
    "title": "스트림 제목",
    "channel_id": "UCxxxxx",
    "status": "live" | "upcoming",
    "start_scheduled": "2026-01-04T10:00:00Z",
    "start_actual": "2026-01-04T10:05:00Z",
    "channel": {
      "id": "UCxxxxx",
      "name": "채널명",
      "english_name": "English Name",
      "type": "vtuber",
      "photo": "https://..."
    }
  }
]
```

#### 1.4 `/live` vs `/users/live` 비교

| 기능 | `/live` | `/users/live` |
|------|:-------:|:-------------:|
| `org` 필터 | 지원 | 미지원 |
| `status` 필터 | 지원 | 미지원 |
| `sort` / `order` | 지원 | 미지원 |
| `max_upcoming_hours` | 지원 | 미지원 |
| `channels` 목록 | 미지원 | 필수 |
| **응답 속도** | 보통 | **빠름** |
| **API 비용** | 보통 | **저렴** |
| **반환 결과** | 필터링된 결과 | live + upcoming 모두 |

#### 1.5 적합한 사용 시나리오

1. **알림 체크** - 구독 채널의 생방송 상태 빠른 확인
2. **대시보드** - 고정된 채널 목록의 실시간 상태
3. **Tauri 앱** - 빈번한 폴링이 필요한 클라이언트
4. **배치 폴링** - 최소 API 오버헤드로 고빈도 조회

#### 1.6 주의사항

**제약사항**
- `org`, `status`, `sort` 파라미터 **미지원**
- 항상 `live`와 `upcoming` 상태 모두 반환
- 클라이언트에서 추가 필터링 필요 시 직접 구현

**fallback 동작**
- `/users/live` 호출 실패 시 **retryable 오류(5xx/timeout/circuit/key rotation)** 에서만 fallback 시도
- fallback은 채널별 **YouTube producer** 결과만 사용
- live-status batch fallback에서는 **공식 스케줄 페이지를 재조회하지 않음**

---

### 2. `/live` - 스트림 조회

#### 2.1 GetLiveStreams

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/live` |
| Go 함수 | `holodex.Service.GetLiveStreams(ctx)` |
| 캐시 TTL | `constants.CacheTTL.LiveStreams` |

**요청 파라미터:**
- `org`: `Hololive`
- `status`: `live`
- `type`: `stream`

#### 2.2 GetUpcomingStreams

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/live` |
| Go 함수 | `holodex.Service.GetUpcomingStreams(ctx, hours int)` |
| 캐시 TTL | `constants.CacheTTL.UpcomingStreams` |

**요청 파라미터:**
- `org`: `Hololive`
- `status`: `upcoming`
- `type`: `stream`
- `max_upcoming_hours`: (최대 168)
- `order`: `asc`
- `sort`: `start_scheduled`

#### 2.3 GetChannelSchedule

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/live` |
| Go 함수 | `holodex.Service.GetChannelSchedule(ctx, channelID, hours, includeLive)` |
| 캐시 TTL | `constants.CacheTTL.ChannelSchedule` |

**요청 파라미터:**
- `channel_id`: 채널 ID
- `status`: `live,upcoming` (includeLive=true) 또는 `upcoming`
- `type`: `stream`
- `max_upcoming_hours`: 시간

**fallback 메모:**
- Holodex `/live` 호출이 retryable 오류로 실패하면 scraper fallback 사용
- scraper는 **YouTube producer 우선**, 해당 scraper 오류일 때만 **공식 스케줄 페이지**로 한 번 더 내려감

---

### 3. `/channels` - 채널 조회

#### 3.1 SearchChannels

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/channels` |
| Go 함수 | `holodex.Service.SearchChannels(ctx, query)` |
| 캐시 TTL | `constants.CacheTTL.ChannelSearch` |

**요청 파라미터:**
- `org`: `Hololive`
- `type`: `vtuber`
- `limit`: `50`

> 주의: `name` 파라미터는 Holodex API에서 무시됨 → 클라이언트 사이드 필터링 적용

#### 3.2 fetchHololiveChannelList

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/channels` |
| Go 함수 | `holodex.Service.fetchHololiveChannelList(ctx)` (내부용) |
| 캐시 TTL | 5분 |

**요청 파라미터:**
- `org`: `Hololive`
- `type`: `vtuber`
- `limit`: `200`

#### 3.3 GetChannel

| 항목 | 값 |
|------|-----|
| HTTP Method | `GET` |
| Endpoint | `/channels/{channelId}` |
| Go 함수 | `holodex.Service.GetChannel(ctx, channelID)` |
| 캐시 TTL | `constants.CacheTTL.ChannelInfo` |

**fallback 메모:**
- retryable 오류(5xx/timeout/circuit/key rotation)에서만 YouTube producer fallback 시도
- 4xx 등 non-retryable 오류는 그대로 반환
- fallback 자체가 실패하면 `(nil, nil)`로 숨기지 않고 명시적 에러 반환

---

## 사용 예시

### curl 명령어

#### /users/live (빠른 상태 조회)
```bash
curl -s -H "X-APIKEY: {API_KEY}" \
  "https://holodex.net/api/v2/users/live?channels=UC1DCedRgGHBdm81E1llLhOQ,UCl_gCybOJRIgOXw6Qb4qJzQ" | jq
```

#### /live (생방송 조회)
```bash
curl -s -H "X-APIKEY: {API_KEY}" \
  "https://holodex.net/api/v2/live?org=Hololive&status=live&type=stream" | jq
```

#### /live (예정 방송 조회)
```bash
curl -s -H "X-APIKEY: {API_KEY}" \
  "https://holodex.net/api/v2/live?org=Hololive&status=upcoming&type=stream&max_upcoming_hours=24&order=asc&sort=start_scheduled" | jq
```

#### /channels (채널 조회)
```bash
curl -s -H "X-APIKEY: {API_KEY}" \
  "https://holodex.net/api/v2/channels?org=Hololive&type=vtuber&limit=50" | jq
```

### Go 코드 예시

```go
// 빠른 생방송 상태 조회 (권장)
channelIDs := []string{"UC1DCedRgGHBdm81E1llLhOQ", "UCl_gCybOJRIgOXw6Qb4qJzQ"}
streams, err := holodexSvc.GetChannelsLiveStatus(ctx, channelIDs)
if err != nil {
    return err
}

// live 상태만 필터링 (클라이언트 사이드)
var liveStreams []*domain.Stream
for _, s := range streams {
    if s.Status == domain.StreamStatusLive {
        liveStreams = append(liveStreams, s)
    }
}
```

---

## 라이선스 준수

Holodex API 사용 시 Section 6 (Attribution) 준수 필요:
1. Holodex 식별 및 링크 제공
2. 소스 코드에 라이선스 고지
3. 무보증 고지

> `THE HOLODEX API IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND.`
