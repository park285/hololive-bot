package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"strings"

	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
)

var claimKeyPrefixes = [...]string{
	alarmkeys.NotifyClaimKeyPrefix,
	alarmkeys.RoomScheduleTransitionKeyPrefix,
	alarmkeys.LogicalScheduleTransitionKeyPrefix,
}

type ClaimKeyReleaser interface {
	DelMany(ctx context.Context, keys []string) (int64, error)
}

func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c == nil {
		return errors.New("release claim keys: claim key releaser is not configured")
	}

	return releaseClaimKeys(ctx, c.claimReleaser, claimKeys)
}

func releaseClaimKeys(ctx context.Context, releaser ClaimKeyReleaser, claimKeys []string) error {
	if releaser == nil {
		return errors.New("release claim keys: claim key releaser is not configured")
	}

	filtered := make([]string, 0, len(claimKeys))
	for _, key := range claimKeys {
		trimmed := strings.TrimSpace(key)
		if isReleasableClaimKey(trimmed) {
			filtered = append(filtered, trimmed)
		}
	}

	if len(filtered) == 0 {
		return nil
	}

	if _, err := releaser.DelMany(ctx, filtered); err != nil {
		return fmt.Errorf("release claim keys: del filtered keys: %w", err)
	}

	observePGClaimReleased(len(filtered))

	return nil
}

func isReleasableClaimKey(key string) bool {
	if key == "" {
		return false
	}

	for _, prefix := range claimKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}
