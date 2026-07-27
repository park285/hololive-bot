package settings

import "time"

type ScraperBackfillConfig struct {
	Enabled           bool
	ShortsEnabled     bool
	ShortsInterval    time.Duration
	CommunityEnabled  bool
	CommunityInterval time.Duration
	LiveEnabled       bool
	LiveInterval      time.Duration
	TargetGroup       string
}

func DefaultScraperBackfillConfig() ScraperBackfillConfig {
	return ScraperBackfillConfig{
		Enabled:           false,
		ShortsEnabled:     true,
		ShortsInterval:    5 * time.Minute,
		CommunityEnabled:  true,
		CommunityInterval: 10 * time.Minute,
		LiveEnabled:       true,
		LiveInterval:      3 * time.Minute,
		TargetGroup:       "notification",
	}
}
