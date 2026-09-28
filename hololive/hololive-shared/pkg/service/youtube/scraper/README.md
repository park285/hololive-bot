# YouTube HTML Scraper

YouTube Data API v3 quota 절약을 위한 HTML 스크래핑 기반 채널 정보 추출기.

## 개요

PHP 기반 [YouTube-operational-API](https://github.com/Benjamin-Loison/YouTube-operational-API)를 Go로 포팅하여 `hololive-kakao-bot-go`에 통합.

**포팅 기준일**: 2026-01-19  
**원본 소스**: `/home/kapu/gemini/llm/youtube-operational-api-test/channels.php`

---

## 구현된 기능

### 1. `GetRecentVideos(ctx, channelID, maxResults)` → `[]*Video`

채널 `/videos` 탭 HTML에서 최근 영상 목록을 추출합니다. 원천은 이 HTML 하나이며, 조회·파싱 실패나 parser drift는
RSS 결과나 빈 성공으로 바꾸지 않고 오류로 돌려줍니다(`DEC-20260926-hololive-source-fallbacks-retirement`).
RSS feed는 `GetRecentVideoPublishedTimes`의 게시 시각 보강에만 씁니다.

채널 snippet(아바타·배너)과 Home 탭 예정/라이브 이벤트 조회(`GetChannelSnippet`, `GetUpcomingEvents`)는 Holodex
채널·일정 폴백 전용이었고, 그 폴백과 함께 삭제했습니다.

---

## 사용법

```go
import scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"

// runtime이 읽은 appConfig.YouTube를 넘긴다. HTTP timeout, 본문 상한, 상태 TTL, bucket 접두사가 여기서 온다.
client := scraper.NewClient(appConfig.YouTube, scraper.WithRateLimiter(sharedRL))

// 최근 영상 조회(HTML 단일 원천, 실패는 오류)
videos, err := client.GetRecentVideos(ctx, "UCJFZiqLMntJufDCHc6bQixg", 10)
for _, v := range videos {
    fmt.Printf("%s %s\n", v.VideoID, v.Title)
}
```

---

## 테스트

```bash
# 단위 테스트 (scraper 패키지 전체)
go test ./pkg/service/youtube/scraper/... -v

# 통합 테스트 (실제 YouTube 호출)
go test -tags=integration -v ./pkg/service/youtube/scraper/...
```

---

## 숫자 파싱 헬퍼

| 함수 | 입력 예시 | 출력 |
|------|----------|------|
| `parseShortNumber` | `"1.5K"`, `"2.76M"`, `"1B"` | `1500`, `2760000`, `1000000000` |
| `parseViewCount` | `"1,056,229,686 views"` | `1056229686` |
| `parseVideoCount` | `"2,429 videos"` | `2429` |

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
├── videos.go      # GetRecentVideos, GetPopularVideos
├── yt_initial_data.go # ytInitialData root package wrapper
├── internal/initialdata/ # ytInitialData 추출/후보 점수화
├── alerts.go      # alertRenderer 처리
├── stats_parser.go # 숫자 파싱 헬퍼
├── recent_videos_parser.go # recent videos 파서
├── community.go   # GetCommunityPosts
├── playlists.go   # GetPlaylists
├── shorts.go      # GetShorts
├── types.go       # 타입 정의 (Video, CommunityPost, Playlist, Short)
├── parser_test.go # ytInitialData/숫자 파싱 테스트
├── recent_videos_parser_test.go # bounded scan 회귀 테스트
├── client_test.go # 통합 테스트 (실제 YouTube 호출, -tags=integration)
└── README.md      # 이 문서
```


---

## 서비스 통합

### Holodex 조회와의 관계

Holodex 채널·채널 일정·live-status 조회는 이 scraper를 보조 원천으로 쓰지 않습니다. Holodex 원천 실패는 오류로
돌려주고, YouTube HTML이나 공식 일정 페이지로 보충하거나 그 결과를 캐시하지 않습니다
(`DEC-20260926-hololive-source-fallbacks-retirement`, `DEC-20260926-hololive-live-status-scraper-fallback-removal`).

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
