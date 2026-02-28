package constants

import "time"

// CacheTTL: 패키지 변수다.
var CacheTTL = struct {
	LiveStreams        time.Duration
	UpcomingStreams    time.Duration
	ChannelSchedule    time.Duration
	ChannelInfo        time.Duration
	ChannelSearch      time.Duration
	NextStreamInfo     time.Duration
	NotificationSent   time.Duration
	TwitchNotification time.Duration
}{
	LiveStreams:        5 * time.Minute,  // 5분 - 라이브 스트림 목록
	UpcomingStreams:    5 * time.Minute,  // 5분 - 예정 스트림 목록
	ChannelSchedule:    5 * time.Minute,  // 5분 - 채널 스케줄
	ChannelInfo:        20 * time.Minute, // 20분 - 채널 정보
	ChannelSearch:      10 * time.Minute, // 10분 - 채널 검색 결과
	NextStreamInfo:     60 * time.Minute, // 1시간 - 다음 방송 정보
	NotificationSent:   24 * time.Hour,   // 24시간 - 알림 발송 기록
	TwitchNotification: 168 * time.Hour,  // 7일 - Twitch 알림 발송 기록 (stream_id 기반)
}

// MemberCacheDefaults: 패키지 변수다.
var MemberCacheDefaults = struct {
	ValkeyTTL           time.Duration
	WarmUpChunkSize     int
	WarmUpMaxGoroutines int
}{
	ValkeyTTL:           30 * time.Minute,
	WarmUpChunkSize:     50,
	WarmUpMaxGoroutines: 10,
}

// WebSocketConfig: 패키지 변수다.
var WebSocketConfig = struct {
	MaxReconnectAttempts int
	ReconnectDelay       time.Duration
}{
	MaxReconnectAttempts: 5,
	ReconnectDelay:       5 * time.Second,
}

// ValkeyConfig: 패키지 변수다.
var ValkeyConfig = struct {
	ReadyTimeout      time.Duration
	BlockingPoolSize  int
	PipelineMultiplex int
}{
	ReadyTimeout:      5 * time.Second,
	BlockingPoolSize:  100,
	PipelineMultiplex: 4,
}

// AIInputLimits: 패키지 변수다.
var AIInputLimits = struct {
	MaxQueryLength int
}{
	MaxQueryLength: 500,
}

// RetryConfig: 패키지 변수다.
var RetryConfig = struct {
	MaxAttempts int
	BaseDelay   time.Duration
	Jitter      time.Duration
}{
	MaxAttempts: 3,
	BaseDelay:   500 * time.Millisecond,
	Jitter:      250 * time.Millisecond,
}

// CircuitBreakerConfig: 패키지 변수다.
var CircuitBreakerConfig = struct {
	FailureThreshold    int
	ResetTimeout        time.Duration
	RateLimitTimeout    time.Duration
	HealthCheckInterval time.Duration
	HealthCheckTimeout  time.Duration
}{
	FailureThreshold:    3,                // 3회 연속 실패 시 Circuit OPEN
	ResetTimeout:        30 * time.Second, // 기본 재시도 대기 시간 (30초)
	RateLimitTimeout:    1 * time.Hour,    // 429 Rate Limit 전용 타임아웃 (1시간)
	HealthCheckInterval: 10 * time.Minute, // Health Check 주기 (10분)
	HealthCheckTimeout:  10 * time.Second, // Health Check 타임아웃 (10초)
}

// RetrySchedulerConfig: Holodex 실패 요청 지연 재시도 설정입니다.
var RetrySchedulerConfig = struct {
	Delay   time.Duration
	Timeout time.Duration
	MaxSize int
}{
	Delay:   35 * time.Second, // CircuitBreakerConfig.ResetTimeout(30s) + 5s
	Timeout: 30 * time.Second,
	MaxSize: 10, // 3 org × 2 method + 여유
}

// PaginationConfig: 패키지 변수다.
var PaginationConfig = struct {
	ItemsPerPage   int
	Timeout        time.Duration
	MaxEmbedFields int
}{
	ItemsPerPage:   10,              // 페이지당 항목 수
	Timeout:        3 * time.Minute, // 페이지네이션 타임아웃
	MaxEmbedFields: 25,              // Discord Embed 필드 최대 개수
}

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
	RequestDelay          time.Duration
}{
	MaxConcurrentRequests: 2,
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
	Window:     3 * time.Second,
	KeyPrefix:  "ratelimit:sliding",
	BucketBase: "youtube:scraper",
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
	OrgIndie:            "Indie",
	OrgAll:              "all",
	StatusLive:          "live",
	StatusUpcoming:      "upcoming",
	TypeStream:          "stream",
	TypeVtuber:          "vtuber",
	MaxUpcomingHours:    168,
	DefaultChannelLimit: 50,
	MaxPaginationOffset: 500,
	SyncTargetOrgs:      []string{"Hololive", "VSpo", "Stellive"},
	AllowedFilterOrgs:   []string{"Hololive", "VSpo", "Indie", "Stellive"},
}

// IndieChannelIDs: 개인세 VTuber 채널 ID 목록 (Holodex /users/live API용)
var IndieChannelIDs = []string{
	"UCrV1Hf5r8P148idjoSfrGEQ", // 結城さくな (Yuuki Sakuna)
	"UCxsZ6NCzjU_t4YSxQLBcM5A", // 사메코 사바 (Sameko Saba)
}

// OfficialScheduleConfig: 패키지 변수다.
var OfficialScheduleConfig = struct {
	BaseURL     string
	Timeout     time.Duration
	CacheExpiry time.Duration
}{
	BaseURL:     "https://schedule.hololive.tv",
	Timeout:     15 * time.Second,
	CacheExpiry: 30 * time.Minute,
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

// StringLimits: 패키지 변수다.
var StringLimits = struct {
	EmbedTitle       int
	EmbedDescription int
	EmbedFieldName   int
	EmbedFieldValue  int
	StreamTitle      int
	NextStreamTitle  int
}{
	EmbedTitle:       256,
	EmbedDescription: 4096,
	EmbedFieldName:   256,
	EmbedFieldValue:  1024,
	StreamTitle:      100,
	NextStreamTitle:  40,
}

// MQConfig: 패키지 변수다.
var MQConfig = struct {
	ReplyStreamKey           string
	ReplyStreamMaxLen        int64
	ConsumerGroup            string
	ConnWriteTimeout         time.Duration
	BlockingPoolSize         int
	PipelineMultiplex        int
	DialTimeout              time.Duration
	BlockTimeout             time.Duration
	ReadCount                int64
	WorkerCount              int
	IdempotencyProcessingTTL time.Duration
	IdempotencyTTL           time.Duration
	InitRetryCount           int
	RetryDelay               time.Duration
}{
	ReplyStreamKey:           "kakao:bot:reply",
	ReplyStreamMaxLen:        1000,
	ConsumerGroup:            "hololive-bot-group",
	ConnWriteTimeout:         3 * time.Second,
	BlockingPoolSize:         50,
	PipelineMultiplex:        4,
	DialTimeout:              5 * time.Second,
	BlockTimeout:             5 * time.Second,
	ReadCount:                50,
	WorkerCount:              10,
	IdempotencyProcessingTTL: 10 * time.Minute, // 처리 중 락 TTL
	IdempotencyTTL:           24 * time.Hour,
	InitRetryCount:           10,
	RetryDelay:               1 * time.Second,
}

// AppTimeout: 앱 빌드/종료 타임아웃 설정입니다.
var AppTimeout = struct {
	Build    time.Duration
	Shutdown time.Duration
}{
	Build:    30 * time.Second,
	Shutdown: 10 * time.Second,
}

// ServerTimeout: HTTP 서버 타임아웃입니다.
var ServerTimeout = struct {
	ReadHeader     time.Duration
	Read           time.Duration
	Write          time.Duration
	Idle           time.Duration
	MaxHeaderBytes int
}{
	ReadHeader:     5 * time.Second,
	Read:           15 * time.Second,
	Write:          60 * time.Second,
	Idle:           60 * time.Second,
	MaxHeaderBytes: 1 << 20, // 1MiB
}

// ServerConfig: 서버 기본 설정입니다.
var ServerConfig = struct {
	TrustedProxies []string
}{
	TrustedProxies: []string{"127.0.0.1", "::1"},
}

// CORSConfig: CORS 기본 설정입니다.
var CORSConfig = struct {
	AllowMethods []string
	AllowHeaders []string
}{
	AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
	AllowHeaders: []string{
		"Origin", "Content-Type", "Accept", "Authorization",
		// Client Hints 헤더 (실제 기기 정보 수집용)
		"Sec-CH-UA", "Sec-CH-UA-Mobile", "Sec-CH-UA-Platform",
		"Sec-CH-UA-Platform-Version", "Sec-CH-UA-Model",
		"Sec-CH-UA-Arch", "Sec-CH-UA-Bitness", "Sec-CH-UA-Full-Version-List",
	},
}

// RequestTimeout: HTTP 요청 및 서비스 타임아웃 설정
var RequestTimeout = struct {
	AdminRequest      time.Duration
	BotCommand        time.Duration
	BotAlarmCheck     time.Duration
	WebhookProcessing time.Duration
	AlarmService      time.Duration
	DatabasePing      time.Duration
}{
	AdminRequest:      10 * time.Second,
	BotCommand:        10 * time.Second,
	BotAlarmCheck:     2 * time.Minute,
	WebhookProcessing: 30 * time.Second,
	AlarmService:      10 * time.Second,
	DatabasePing:      5 * time.Second,
}

// IrisConnection: Bot 시작 시 Iris 연결 준비 대기 설정입니다.
var IrisConnection = struct {
	ReadyTimeout  time.Duration
	RetryInterval time.Duration
	PingTimeout   time.Duration
}{
	ReadyTimeout:  10 * time.Minute,
	RetryInterval: 2 * time.Second,
	PingTimeout:   3 * time.Second,
}

// IrisWebhookDedupTTL: Iris -> Bot webhook 메시지 중복 처리 방지용 TTL 입니다.
// Iris 측 재시도(단기)에서 동일 메시지 ID를 스킵하기 위한 목적입니다.
var IrisWebhookDedupTTL = 60 * time.Second

// DatabaseConfig: 데이터베이스 연결 설정입니다.
var DatabaseConfig = struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}{
	MaxOpenConns:    25,
	MaxIdleConns:    5,
	ConnMaxLifetime: 5 * time.Minute,
}

// DatabaseDefaults: PostgreSQL 기본값이다. (env 미설정 시)
var DatabaseDefaults = struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}{
	Host:     "postgres",
	Port:     5432,
	User:     "hololive_runtime",
	Password: "",         // 반드시 환경변수로 설정 필요
	Database: "hololive", // hololive-kakao-bot-go 전용 DB
}

// RedisKeys: Redis/Valkey 키 이름 상수입니다.
var RedisKeys = struct {
	AlarmMemberNames string
}{
	AlarmMemberNames: "alarm:member_names",
}

// MajorEventConfig: 대형 행사 알림 설정입니다.
var MajorEventConfig = struct {
	EventRSSURL             string
	NewsRSSURL              string
	NewsRSSURLEn            string
	TrustedSourceDomains    []string
	TrustedSocialAccounts   []string
	SearchSourceSites       []string
	SearchOfficialAccounts  []string
	SearchPartnerKeywords   []string
	RequestTimeout          time.Duration
	SentKeyTTL              time.Duration
	ScheduleHourKST         int
	ScheduleWeekday         time.Weekday
	MaxRetries              int
	RetryDelay              time.Duration
	MaxPages                int
	IncrementalCursorLimit  int
	PageDelay               time.Duration
	UserAgent               string
	LinkCheckBatchSize      int
	LinkCheckTimeout        time.Duration
	LinkCheckStaleAfter     time.Duration
	ScrapeUpsertConcurrency int
	ScrapeHourKST           int
	ScrapeRetryDelays       []time.Duration
	MonthlyScheduleHourKST  int
	MonthlyScheduleDay      int
}{
	EventRSSURL:  "https://hololive.hololivepro.com/events/feed/",
	NewsRSSURL:   "https://hololive.hololivepro.com/news/feed/",
	NewsRSSURLEn: "https://hololive.hololivepro.com/en/news/feed/",
	TrustedSourceDomains: []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"cover-corp.com",
		"hololive.tv",
		"schedule.hololive.tv",
		"shop.hololivepro.com",
		"hololive-official-cardgame.com",
		"aniplustv.com",
		"aniplus.co.kr",
		"animate.co.jp",
		"lawson.co.jp",
	},
	TrustedSocialAccounts: []string{
		"hololivetv",
		"hololive_en",
		"hololive_id",
		"holostarsen",
		"hololive_ocg_en",
		"aniplus_shop",
		"v_square_kr",
		"agf_korea",
	},
	SearchSourceSites: []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"x.com",
		"twitter.com",
		"schedule.hololive.tv",
		"shop.hololivepro.com",
		"hololive-official-cardgame.com",
		"aniplustv.com",
		"aniplus.co.kr",
	},
	SearchOfficialAccounts: []string{
		"hololivetv",
		"hololive_en",
		"hololive_id",
		"HOLOSTARSen",
		"hololive_OCG_EN",
		"ANIPLUS_SHOP",
		"v_square_kr",
	},
	SearchPartnerKeywords: []string{
		"ANIPLUS",
		"V-SQUARE",
		"AGF Korea",
		"collaboration cafe",
	},
	RequestTimeout:          30 * time.Second,
	SentKeyTTL:              8 * 24 * time.Hour,
	ScheduleHourKST:         9,
	ScheduleWeekday:         time.Monday,
	MaxRetries:              4,
	RetryDelay:              1 * time.Second,
	MaxPages:                20,
	IncrementalCursorLimit:  200,
	PageDelay:               2 * time.Second,
	UserAgent:               "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36",
	LinkCheckBatchSize:      200,
	LinkCheckTimeout:        8 * time.Second,
	LinkCheckStaleAfter:     72 * time.Hour,
	ScrapeUpsertConcurrency: 8,
	ScrapeHourKST:           6,
	ScrapeRetryDelays: []time.Duration{
		30 * time.Minute,
		2 * time.Hour,
		6 * time.Hour,
	},
	MonthlyScheduleHourKST: 10,
	MonthlyScheduleDay:     1,
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
