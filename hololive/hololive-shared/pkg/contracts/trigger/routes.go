package trigger

const (
	// BasePath: internal trigger API route group
	BasePath = "/internal/trigger"
)

const (
	// Relative routes (for router group registration)
	MajorEventWeeklyRoute  = "/majorevent-weekly"
	MajorEventMonthlyRoute = "/majorevent-monthly"
	MemberNewsWeeklyRoute  = "/membernews-weekly"
)

const (
	// Absolute paths (for HTTP client calls)
	MajorEventWeeklyPath  = BasePath + MajorEventWeeklyRoute
	MajorEventMonthlyPath = BasePath + MajorEventMonthlyRoute
	MemberNewsWeeklyPath  = BasePath + MemberNewsWeeklyRoute
)
