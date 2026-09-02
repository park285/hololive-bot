package collecterr

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func FromStatus(source string, status int, retryAfter string, now time.Time) error {
	message := fmt.Sprintf("%s status %d", source, status)
	if status == http.StatusTooManyRequests {
		if err := WithRetry(New(Cooldown, ClassCooldown, message), ParseRetryAfter(retryAfter, now)); err != nil {
			return fmt.Errorf("with retry: %w", err)
		}

		return nil
	}

	if status == http.StatusServiceUnavailable {
		hint := ParseRetryAfter(retryAfter, now)
		if hint.Kind() == RetryDefault {
			return New(Failed, ClassTransient, message)
		}

		if err := WithRetry(New(Cooldown, ClassCooldown, message), hint); err != nil {
			return fmt.Errorf("with retry: %w", err)
		}

		return nil
	}

	code, class := statusFailure(status)

	return New(code, class, message)
}

func statusFailure(status int) (code ErrorCode, class FailureClass) {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusGatewayTimeout:
		return Failed, ClassTransient
	case http.StatusUnauthorized, http.StatusForbidden:
		return Configuration, ClassConfiguration
	}

	if status >= 300 && status < 400 {
		return Failed, ClassProtocol
	}

	return Internal, ClassInternal
}

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
