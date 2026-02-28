// Package timeutil provides common time-related utilities.
package timeutil

import (
	"fmt"
	"time"
)

// Common duration constants.
const (
	Day   = 24 * time.Hour
	Week  = 7 * Day
	Month = 30 * Day  // Approximate
	Year  = 365 * Day // Approximate
)

// FormatDuration formats a duration into a human-readable Korean string.
// Examples: "1시간 30분", "2일 3시간", "45초"
func FormatDuration(d time.Duration) string {
	if d < 0 {
		return "0초"
	}

	days := int(d / Day)
	d -= time.Duration(days) * Day
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%d일 %d시간", days, hours)
		}
		return fmt.Sprintf("%d일", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%d시간 %d분", hours, minutes)
		}
		return fmt.Sprintf("%d시간", hours)
	case minutes > 0:
		if seconds > 0 {
			return fmt.Sprintf("%d분 %d초", minutes, seconds)
		}
		return fmt.Sprintf("%d분", minutes)
	default:
		return fmt.Sprintf("%d초", seconds)
	}
}

// FormatDurationCompact formats a duration into a compact string.
// Examples: "1h30m", "2d3h", "45s"
func FormatDurationCompact(d time.Duration) string {
	if d < 0 {
		return "0s"
	}

	days := int(d / Day)
	d -= time.Duration(days) * Day
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	minutes := int(d / time.Minute)
	d -= time.Duration(minutes) * time.Minute
	seconds := int(d / time.Second)

	switch {
	case days > 0:
		if hours > 0 {
			return fmt.Sprintf("%dd%dh", days, hours)
		}
		return fmt.Sprintf("%dd", days)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%dh%dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// TruncateToDay truncates a time to the start of the day in the given location.
func TruncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
