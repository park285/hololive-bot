package constants

import "time"

// AIInputLimits: 패키지 변수다.
var AIInputLimits = struct {
	MaxQueryLength int
}{
	MaxQueryLength: 500,
}

// RetryBudgetConfig: 전역 재시도 예산 설정입니다.
var RetryBudgetConfig = struct {
	MaxRetriesPerMinute int
	Enabled             bool
}{
	MaxRetriesPerMinute: 100,
	Enabled:             true,
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

// APIRateLimitConfig: /api/holo 엔드포인트에 적용되는 IP 기반 슬라이딩 윈도우 레이트 리밋 설정입니다.
var APIRateLimitConfig = struct {
	Enabled bool
	Limit   int
	Window  time.Duration
}{
	Enabled: true,
	Limit:   60, // 분당 60회
	Window:  time.Minute,
}

// MajorEventConfig: 대형 행사 알림 설정입니다.
var MajorEventConfig = struct {
	TrustedSourceDomains   []string
	TrustedSocialAccounts  []string
	SearchSourceSites      []string
	SearchOfficialAccounts []string
	SearchPartnerKeywords  []string
	ScheduleHourKST        int
	ScheduleWeekday        time.Weekday
	MonthlyScheduleHourKST int
	MonthlyScheduleDay     int
}{
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
	ScheduleHourKST:        9,
	ScheduleWeekday:        time.Monday,
	MonthlyScheduleHourKST: 10,
	MonthlyScheduleDay:     1,
}
