package youtube

import (
	"fmt"
	"time"
)

type ChannelStats struct {
	ChannelID       string
	ChannelTitle    string
	SubscriberCount uint64
	VideoCount      uint64
	ViewCount       uint64
	Timestamp       time.Time
}

type QuotaExceededError struct {
	Used      int
	Limit     int
	Requested int
	ResetTime time.Time
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("YouTube API quota exceeded: used %d/%d (requested %d more), resets at %s",
		e.Used, e.Limit, e.Requested, e.ResetTime.Format(time.RFC3339))
}
