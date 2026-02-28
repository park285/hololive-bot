# YouTube HTML Scraper

YouTube Data API v3 quota 절약을 위한 HTML 스크래핑 기반 채널 정보 추출기.

## 개요

PHP 기반 [YouTube-operational-API](https://github.com/Benjamin-Loison/YouTube-operational-API)를 Go로 포팅하여 `hololive-kakao-bot-go`에 통합.

**포팅 기준일**: 2026-01-19  
**원본 소스**: `/home/kapu/gemini/llm/youtube-operational-api-test/channels.php`

---

## 구현된 기능

### 1. `GetChannelStats(ctx, channelID)` → `*ChannelStats`

채널 통계 정보 조회 (YouTube `/about` 페이지에서 추출)

| 필드 | 타입 | 설명 | 예시 |
|------|------|------|------|
| `ChannelID` | `string` | 채널 ID | `UC1DCedRgGHBdm81E1llLhOQ` |
| `SubscriberCount` | `int64` | 구독자 수 | `2760000` |
| `ViewCount` | `int64` | 총 조회수 | `1056229686` |
| `VideoCount` | `int64` | 비디오 수 | `2429` |
| `JoinedDate` | `int64` | 가입일 (Unix timestamp) | `1562025600` |
| `Description` | `string` | 채널 설명 | `"こんぺこ！..."` |
| `Country` | `string` | 국가 | `"Japan"` |
| `Handle` | `string` | 채널 핸들 | `"@usadapekora"` |

**JSON 경로** (YouTube 2025 구조):
```
onResponseReceivedEndpoints.0.showEngagementPanelEndpoint.engagementPanel
  .engagementPanelSectionListRenderer.content.sectionListRenderer.contents.0
  .itemSectionRenderer.contents.0.aboutChannelRenderer.metadata.aboutChannelViewModel
```

---

### 2. `GetChannelSnippet(ctx, channelID)` → `*ChannelSnippet`

채널 프로필 이미지 조회 (YouTube 채널 메인 페이지에서 추출)

| 필드 | 타입 | 설명 |
|------|------|------|
| `Avatar` | `[]Thumbnail` | 프로필 이미지 (여러 해상도) |
| `Banner` | `[]Thumbnail` | 배너 이미지 (여러 해상도) |

**JSON 경로** (YouTube 2025 구조 - `pageHeaderRenderer`):
```
# Avatar
header.pageHeaderRenderer.content.pageHeaderViewModel.image
  .decoratedAvatarViewModel.avatar.avatarViewModel.image.sources

# Banner
header.pageHeaderRenderer.content.pageHeaderViewModel.banner
  .imageBannerViewModel.image.sources
```

**폴백 경로** (이전 `c4TabbedHeaderRenderer` 구조):
```
header.c4TabbedHeaderRenderer.avatar.thumbnails
header.c4TabbedHeaderRenderer.banner.thumbnails
```

---

### 3. `GetUpcomingEvents(ctx, channelID)` → `[]*UpcomingEvent`

예정/라이브 방송 목록 조회 (채널 Home 탭에서 추출)

| 필드 | 타입 | 설명 |
|------|------|------|
| `VideoID` | `string` | 비디오 ID |
| `Title` | `string` | 방송 제목 |
| `Thumbnail` | `[]Thumbnail` | 썸네일 이미지 |
| `Status` | `string` | 상태 (`"LIVE"`, `"UPCOMING"`, `"DEFAULT"`) |
| `StartTime` | `*int64` | 예정 시작 시간 (Unix timestamp, optional) |
| `ViewCountText` | `string` | 시청자 수 텍스트 |
| `ChannelTitle` | `string` | 채널 이름 |

**탐색 영역**:
1. `channelFeaturedContentRenderer.items` - Featured 영역
2. `shelfRenderer.content.horizontalListRenderer.items` - Shelf 영역
   - `videoRenderer` (새 구조)
   - `gridVideoRenderer` (이전 구조)

**상태 판단 로직**:
```go
// thumbnailOverlays에서 LIVE/UPCOMING 체크
overlay.thumbnailOverlayTimeStatusRenderer.style == "LIVE" || "UPCOMING"

// 또는 upcomingEventData 존재 여부
video.upcomingEventData.Exists() → "UPCOMING"
```

---

## 사용법

```go
import "github.com/kapu/hololive-shared/pkg/service/youtube/scraper"

client := scraper.NewClient()

// 채널 통계 조회
stats, err := client.GetChannelStats(ctx, "UC1DCedRgGHBdm81E1llLhOQ")
fmt.Printf("구독자: %d\n", stats.SubscriberCount)

// 채널 프로필 이미지 조회
snippet, err := client.GetChannelSnippet(ctx, "UC1DCedRgGHBdm81E1llLhOQ")
fmt.Printf("아바타 URL: %s\n", snippet.Avatar[0].URL)

// 예정/라이브 방송 조회
events, err := client.GetUpcomingEvents(ctx, "UCJFZiqLMntJufDCHc6bQixg")
for _, e := range events {
    fmt.Printf("[%s] %s\n", e.Status, e.Title)
}
```

---

## 테스트

```bash
# 단위 테스트 (숫자 파싱 로직)
go test ./internal/service/youtube/scraper/... -v

# 통합 테스트 (실제 YouTube 호출)
go test -tags=integration -v ./internal/service/youtube/scraper/...
```

---

## 숫자 파싱 헬퍼

| 함수 | 입력 예시 | 출력 |
|------|----------|------|
| `parseSubscriberCount` | `"2.76M subscribers"` | `2760000` |
| `parseShortNumber` | `"1.5K"`, `"2.76M"`, `"1B"` | `1500`, `2760000`, `1000000000` |
| `parseViewCount` | `"1,056,229,686 views"` | `1056229686` |
| `parseVideoCount` | `"2,429 videos"` | `2429` |
| `parseJoinedDate` | `"Joined Jul 2, 2019"` | `1562025600` (Unix) |

---

## 주의사항

1. **Rate Limiting**: YouTube IP 차단 가능성 있음 - 요청 간격 조절 권장
2. **User-Agent**: 브라우저와 유사한 User-Agent 사용 (Chrome 130)
3. **Accept-Language**: `en`으로 고정하여 일관된 텍스트 포맷 보장
4. **구조 변경**: YouTube 페이지 구조 변경 시 JSON 경로 업데이트 필요

---

## 파일 구조

```
scraper/
├── client.go      # HTTP 클라이언트 (Client 구조체, fetchPage)
├── channel.go     # GetChannelStats, GetChannelSnippet
├── videos.go      # GetUpcomingEvents, GetRecentVideos, GetPopularVideos
├── community.go   # GetCommunityPosts
├── playlists.go   # GetPlaylists
├── shorts.go      # GetShorts
├── parser.go      # ytInitialData 추출 및 숫자 파싱 헬퍼
├── types.go       # 타입 정의 (ChannelStats, Video, CommunityPost, Playlist, Short)
├── parser_test.go # 단위 테스트 (숫자 파싱)
├── client_test.go # 통합 테스트 (실제 YouTube 호출, -tags=integration)
└── README.md      # 이 문서
```


---

## 서비스 통합

### YouTube Service 통합 (`internal/service/youtube/service.go`)

채널 통계 조회 시 스크래퍼를 우선 사용하여 API quota를 절약합니다.

```go
// GetChannelStatistics 호출 시:
// 1. 병렬 스크래핑 (채널당 1회 HTTP 요청)
// 2. 스크래핑 실패 채널만 YouTube Data API로 폴백
```

### Holodex 폴백 통합 (`internal/service/holodex/scraper.go`)

Holodex API 실패 시 3단계 폴백 체계로 안정성 확보:

```
폴백 순서:
1. Holodex API (주요 소스)
2. YouTube HTML 스크래핑 (1차 폴백) ← 이 scraper 패키지 사용
3. 홀로라이브 공식 스케줄 페이지 (2차 폴백)
```

---

## 라이브러리 의존성

| 용도 | 라이브러리 | 버전 |
|-----|----------|------|
| JSON 경로 탐색 | `github.com/tidwall/gjson` | v1.18.0 |
| HTTP 클라이언트 | 표준 `net/http` | - |

---

## 참고 자료

- [YouTube-operational-API (PHP 원본)](https://github.com/Benjamin-Loison/YouTube-operational-API)
- [gjson 문서](https://github.com/tidwall/gjson)
- YouTube 페이지 구조 분석 (2026-01-19 기준):
  - `aboutChannelViewModel`: 구독자 수, 조회수, 비디오 수
  - `pageHeaderRenderer`: 아바타, 배너
  - `channelFeaturedContentRenderer`: 라이브/예정 방송

