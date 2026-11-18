package formatter

import (
	"fmt"
	"strings"
	"time"
)

func formatBroadcastDuration(d time.Duration) string {
	minutes := int64(d / time.Minute)
	if minutes < 1 {
		return "1분 미만"
	}

	days, rem := minutes/(24*60), minutes%(24*60)
	hours, mins := rem/60, rem%60

	parts := make([]string, 0, 3)

	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d일", days))
	}

	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d시간", hours))
	}

	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d분", mins))
	}

	return strings.Join(parts, " ")
}
