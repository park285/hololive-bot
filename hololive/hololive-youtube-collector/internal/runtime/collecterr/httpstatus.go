package collecterr

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func ParseRetryAfter(value string, _ time.Time) RetryHint {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultRetryHint()
	}

	if after, ok := parseRetryAfterDeltaSeconds(value); ok {
		hint, err := NewRetryAfterHint(after)
		if err != nil {
			return defaultRetryHint()
		}

		return hint
	}

	parsed, err := http.ParseTime(value)
	if err != nil {
		return defaultRetryHint()
	}

	hint, hintErr := NewRetryAtHint(parsed)
	if hintErr != nil {
		return defaultRetryHint()
	}

	return hint
}

func parseRetryAfterDeltaSeconds(value string) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}

	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}

	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 || seconds > math.MaxInt32 {
		return 0, false
	}

	return time.Duration(seconds) * time.Second, true
}
