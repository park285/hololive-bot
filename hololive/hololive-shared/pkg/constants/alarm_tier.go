package constants

import "time"

// Tier 판정 구간 (timeToStart 기준 - 이 값 이하이면 해당 Tier로 분류)
const (
	// Tier1Window: 45분 이내 - 1분 간격 폴링
	Tier1Window = 45 * time.Minute
	// Tier2Window: 3시간 이내 - 3분 간격 폴링
	Tier2Window = 3 * time.Hour
	// Tier3Window: 12시간 이내 - 10분 간격 폴링
	Tier3Window = 12 * time.Hour
)

// Tier 폴링 간격
const (
	// Tier1Interval: 1분
	Tier1Interval = 1 * time.Minute
	// Tier2Interval: 3분
	Tier2Interval = 3 * time.Minute
	// Tier3Interval: 10분
	Tier3Interval = 10 * time.Minute
	// Tier4Interval: 15분 (원거리 폴백)
	Tier4Interval = 15 * time.Minute
)

// 예정 없음 / 전체 갱신 간격
const (
	// NoUpcomingInterval: 예정 없거나 시작 시간 불명 시 기본 폴링 간격
	NoUpcomingInterval = 5 * time.Minute
	// FullRefreshInterval: Tier 무시 전체 채널 강제 체크 주기
	FullRefreshInterval = 5 * time.Minute
	// RecentlyNotifiedWindow: 알림 발송 후 고빈도 폴링 유지 시간
	RecentlyNotifiedWindow = 15 * time.Minute
	// LiveCatchupSuppressWindow: 예정 알림 직후 동일 이벤트 catch-up 억제 구간
	LiveCatchupSuppressWindow = 15 * time.Minute
)

// 로컬 폴백 dedup (Valkey 장애 시)
const (
	// LocalFallbackDedupTTL: 로컬 폴백 dedup TTL
	LocalFallbackDedupTTL = 10 * time.Minute
	// LocalFallbackCleanupMaxKeys: 로컬 dedup 맵 최대 키 수
	LocalFallbackCleanupMaxKeys = 4096
)

// DefaultTargetMinutes: 기본 알림 대상 분 목록 (내림차순)
var DefaultTargetMinutes = []int{5, 3, 1}
