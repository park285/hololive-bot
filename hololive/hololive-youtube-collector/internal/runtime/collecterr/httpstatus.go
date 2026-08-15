package collecterr

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func FromStatus(source string, status int, retryAfter string, now time.Time) error {
	message := fmt.Sprintf("%s status %d", source, status)
	if status != http.StatusTooManyRequests {
		return New(Failed, message)
	}
	if retryAt, ok := parseRetryAfter(retryAfter, now); ok {
		return CooldownUntil(message, retryAt)
	}
	return New(Cooldown, message)
}

func parseRetryAfter(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return time.Time{}, false
		}
		return now.UTC().Add(time.Duration(seconds) * time.Second), true
	}
	for _, layout := range []string{time.RFC1123, time.RFC1123Z, time.RFC850, time.ANSIC} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}
