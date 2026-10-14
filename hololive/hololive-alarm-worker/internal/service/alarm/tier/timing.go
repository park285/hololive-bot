package tier

import "time"

// 예정 시작까지 남은 시간 구간입니다. 구간이 가까울수록 점검 주기를 짧게 둡니다.
const (
	tier1Window = 45 * time.Minute
	tier2Window = 3 * time.Hour
	tier3Window = 12 * time.Hour
)

const (
	tier1Interval = 1 * time.Minute
	tier2Interval = 3 * time.Minute
	tier3Interval = 10 * time.Minute
	tier4Interval = 15 * time.Minute
)

const (
	noUpcomingInterval     = 1 * time.Minute
	fullRefreshInterval    = 5 * time.Minute
	recentlyNotifiedWindow = 15 * time.Minute
)

// LiveCatchupWindow는 늦게 관측한 라이브 시작을 catch-up 알림 대상으로 인정하는 범위입니다.
// 전체 갱신 주기 사이에 시작한 라이브를 놓치지 않도록 전체 갱신 주기보다 1분 길게 둡니다.
const LiveCatchupWindow = fullRefreshInterval + time.Minute

// LiveCatchupSuppressWindow는 최근 예정 알림을 받은 방에 라이브 catch-up 알림을 다시 보내지 않는 범위입니다.
const LiveCatchupSuppressWindow = 15 * time.Minute
