package tier

import "time"

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
	noUpcomingInterval  = 1 * time.Minute
	fullRefreshInterval = 5 * time.Minute
)

const LiveCatchupWindow = fullRefreshInterval + time.Minute

const LiveCatchupSuppressWindow = 15 * time.Minute
