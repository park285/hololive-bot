package apiclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
)

// rememberRetryAfter는 호출자의 취소와 무관하게 동일 클라이언트의 429 대기를 보존합니다.
func (c *APIClient) rememberRetryAfter(header string) {
	until, ok := parseRetryAfter(header, time.Now())
	if !ok {
		return
	}

	c.cooldownMu.Lock()
	defer c.cooldownMu.Unlock()

	if until.After(c.cooldownUntil) {
		c.cooldownUntil = until
	}
}

func parseRetryAfter(header string, now time.Time) (time.Time, bool) {
	value := strings.TrimSpace(header)
	if value == "" {
		return time.Time{}, false
	}

	if seconds, err := strconv.ParseUint(value, 10, 63); err == nil {
		const maxSeconds = uint64((1<<63 - 1) / time.Second)

		if seconds > maxSeconds {
			return time.Time{}, false
		}

		return now.Add(time.Duration(seconds) * time.Second), true
	}

	until, err := http.ParseTime(value)
	if err != nil {
		return time.Time{}, false
	}

	return until, true
}

func (c *APIClient) waitForCooldown(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("holodex cooldown canceled: %w", err)
		}

		delay := c.cooldownRemaining()

		if delay <= 0 {
			return nil
		}

		if !retry.Sleep(ctx, delay) {
			return fmt.Errorf("holodex cooldown canceled: %w", ctx.Err())
		}

		// 대기 중 다른 응답이 더 긴 대기를 지정했을 수 있으므로 다시 확인합니다.
	}
}

func (c *APIClient) cooldownRemaining() time.Duration {
	c.cooldownMu.Lock()
	defer c.cooldownMu.Unlock()

	return time.Until(c.cooldownUntil)
}
