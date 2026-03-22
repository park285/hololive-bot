// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package constants

import "time"

// APIConfig: 패키지 변수다.
var APIConfig = struct {
	HolodexBaseURL       string
	HolodexTimeout       time.Duration
	PerAttemptTimeout    time.Duration
	MaxRetryAttempts     int
	MaxResponseBodyBytes int64
}{
	HolodexBaseURL:       "https://holodex.net/api/v2",
	HolodexTimeout:       25 * time.Second, // 동시 요청 제한 적용으로 안정성 향상 (15s → 25s)
	PerAttemptTimeout:    20 * time.Second, // 시도별 context timeout (외부 API 지연 스파이크 흡수)
	MaxRetryAttempts:     3,
	MaxResponseBodyBytes: 2 << 20, // 2MiB
}

// HolodexTransportConfig: Holodex HTTP Transport 설정입니다.
// 동시 요청 시 커넥션 풀 고갈 방지를 위해 디폴트(MaxIdleConnsPerHost=2)보다 높게 설정한다.
var HolodexTransportConfig = struct {
	MaxConnsPerHost     int
	MaxIdleConnsPerHost int
	IdleConnTimeout     time.Duration
}{
	MaxConnsPerHost:     50, // 최대 동시 연결 수 (maxConcurrency와 동일)
	MaxIdleConnsPerHost: 50, // 유휴 커넥션 유지 수
	IdleConnTimeout:     30 * time.Second,
}

var HolodexConcurrencyConfig = struct {
	MaxConcurrentRequests int
	OrgAllParallelism     int
	RequestDelay          time.Duration
}{
	MaxConcurrentRequests: 2,
	OrgAllParallelism:     2,
	RequestDelay:          500 * time.Millisecond,
}

// HolodexDistributedRateLimitConfig: 멀티 인스턴스 환경에서 Holodex 요청 총량을 제한합니다.
var HolodexDistributedRateLimitConfig = struct {
	Enabled    bool
	Limit      int
	Window     time.Duration
	KeyPrefix  string
	BucketBase string
}{
	Enabled:    true,
	Limit:      10,
	Window:     time.Second,
	KeyPrefix:  "ratelimit:sliding",
	BucketBase: "holodex:api",
}

// HolodexAPIParams: Holodex API 호출 시 사용하는 파라미터 상수입니다.
var HolodexAPIParams = struct {
	OrgHololive         string
	OrgVSpo             string
	OrgStellive         string
	OrgIndie            string
	OrgAll              string
	StatusLive          string
	StatusUpcoming      string
	TypeStream          string
	TypeVtuber          string
	MaxUpcomingHours    int
	DefaultChannelLimit int
	MaxPaginationOffset int
	SyncTargetOrgs      []string // 동기화 대상 org 목록
	AllowedFilterOrgs   []string // 필터 허용 org 목록
}{
	OrgHololive:         "Hololive",
	OrgVSpo:             "VSpo",
	OrgStellive:         "Stellive",
	OrgIndie:            "Independents",
	OrgAll:              "all",
	StatusLive:          "live",
	StatusUpcoming:      "upcoming",
	TypeStream:          "stream",
	TypeVtuber:          "vtuber",
	MaxUpcomingHours:    168,
	DefaultChannelLimit: 50,
	MaxPaginationOffset: 500,
	SyncTargetOrgs:      []string{"Hololive", "VSpo", "Stellive"},
	AllowedFilterOrgs:   []string{"Hololive", "VSpo", "Independents", "Stellive"},
}

// YouTubeConfig: 패키지 변수다.
var YouTubeConfig = struct {
	DailyQuotaLimit       int
	SearchQuotaCost       int
	ChannelsQuotaCost     int
	MaxChannelsPerCall    int
	MaxConcurrentRequests int
	SearchMaxResults      int
	QuotaSafetyMargin     int
	CacheExpiration       time.Duration
	MaxPageBodyBytes      int64         // YouTube HTML 페이지 최대 응답 바디 크기
	ScraperHTTPTimeout    time.Duration // 스크래퍼 HTTP 클라이언트 전체 타임아웃 (요청 1회)
	ScraperDialTimeout    time.Duration // 스크래퍼 Dial/TLS 핸드셰이크 타임아웃
	ScraperHeaderTimeout  time.Duration // 스크래퍼 응답 헤더 대기 타임아웃
	ScraperPhaseTimeout   time.Duration // 스크래핑 전체 타임아웃 (HTTP context 취소와 독립)
	APIFallbackTimeout    time.Duration // API 폴백 타임아웃 (HTTP context 취소와 독립)
	CacheSaveTimeout      time.Duration // 캐시 저장 타임아웃 (fire-and-forget용)
	CommunityMissingTTL   time.Duration // 커뮤니티 탭 미지원 채널 재검증 주기
	VideoRSSBackoffTTL    time.Duration // videos HTML 5xx 시 RSS 우선 전환 유지 시간
}{
	DailyQuotaLimit:       10000,
	SearchQuotaCost:       100,
	ChannelsQuotaCost:     1,
	MaxChannelsPerCall:    20,
	MaxConcurrentRequests: 3,
	SearchMaxResults:      10,
	QuotaSafetyMargin:     2000,
	CacheExpiration:       2 * time.Hour,
	MaxPageBodyBytes:      8 << 20,          // 8MiB (일부 채널 페이지의 대형 초기 JSON 대응)
	ScraperHTTPTimeout:    15 * time.Second, // VPN/SOCKS 불안정 시 장시간 블로킹 완화
	ScraperDialTimeout:    5 * time.Second,  // 프록시/원격 연결 지연의 빠른 실패
	ScraperHeaderTimeout:  12 * time.Second, // 헤더 수신 지연(blackhole) 조기 감지
	ScraperPhaseTimeout:   45 * time.Second, // 69채널 × 세마포어5 = 14 batch + 안전마진
	APIFallbackTimeout:    30 * time.Second, // 배치 50개 × 2 batch + 여유
	CacheSaveTimeout:      5 * time.Second,  // 캐시 저장용
	CommunityMissingTTL:   24 * time.Hour,   // /posts 404 채널은 하루 후 재검증
	VideoRSSBackoffTTL:    6 * time.Hour,    // 5xx 반복 채널은 6시간 RSS 우선
}

// YouTubeScraperRateLimitConfig: 단일 인스턴스 기준 YouTube HTML 스크래퍼 요청 간격입니다.
var YouTubeScraperRateLimitConfig = struct {
	RequestInterval time.Duration
}{
	RequestInterval: 3 * time.Second,
}

// YouTubeScraperDistributedRateLimitConfig: 멀티 인스턴스 환경에서 YouTube HTML 스크래퍼 총량을 제한합니다.
var YouTubeScraperDistributedRateLimitConfig = struct {
	Enabled    bool
	Limit      int
	Window     time.Duration
	KeyPrefix  string
	BucketBase string
}{
	Enabled:    true,
	Limit:      1,
	Window:     YouTubeScraperRateLimitConfig.RequestInterval,
	KeyPrefix:  "ratelimit:sliding",
	BucketBase: "youtube:scraper",
}

// OfficialScheduleConfig: 패키지 변수다.
var OfficialScheduleConfig = struct {
	BaseURL      string
	Timeout      time.Duration
	CacheExpiry  time.Duration
	PageCacheTTL time.Duration
}{
	BaseURL:      "https://schedule.hololive.tv",
	Timeout:      15 * time.Second,
	CacheExpiry:  30 * time.Minute,
	PageCacheTTL: 15 * time.Second,
}

// OfficialProfileConfig: 패키지 변수다.
var OfficialProfileConfig = struct {
	BaseURL        string
	UserAgent      string
	AcceptLanguage string
	RequestTimeout time.Duration
	DelayBetween   time.Duration
	OutputFile     string
}{
	BaseURL:        "https://hololive.hololivepro.com/talents",
	UserAgent:      "Mozilla/5.0 (compatible; HololiveKakaoBot/1.0; +https://hololive.hololivepro.com)",
	AcceptLanguage: "ja,en;q=0.8,ko;q=0.6",
	RequestTimeout: 15 * time.Second,
	DelayBetween:   350 * time.Millisecond,
	OutputFile:     "internal/domain/data/official_profiles_raw.json",
}

// TwitchConfig: Twitch API 설정입니다.
var TwitchConfig = struct {
	BaseURL            string
	AuthURL            string
	Timeout            time.Duration
	PollInterval       time.Duration
	TokenRefreshSkew   time.Duration
	MarkerTTL          time.Duration
	MaxUsersPerRequest int
}{
	BaseURL:            "https://api.twitch.tv/helix",
	AuthURL:            "https://id.twitch.tv/oauth2/token",
	Timeout:            10 * time.Second,
	PollInterval:       60 * time.Second,
	TokenRefreshSkew:   5 * time.Minute,
	MarkerTTL:          7 * 24 * time.Hour,
	MaxUsersPerRequest: 100,
}

// ChzzkConfig: Chzzk API 조회 전략 설정입니다.
var ChzzkConfig = struct {
	MaxLivesPageSize          int
	BatchLookupThreshold      int
	MaxConcurrentStatusChecks int
}{
	MaxLivesPageSize:          20,
	BatchLookupThreshold:      4,
	MaxConcurrentStatusChecks: 4,
}

// IndieChannelIDs: 개인세 VTuber 채널 ID 목록 (Holodex /users/live API용)
var IndieChannelIDs = []string{
	"UCrV1Hf5r8P148idjoSfrGEQ", // 結城さくな (Yuuki Sakuna)
	"UCxsZ6NCzjU_t4YSxQLBcM5A", // 사메코 사바 (Sameko Saba)
}
