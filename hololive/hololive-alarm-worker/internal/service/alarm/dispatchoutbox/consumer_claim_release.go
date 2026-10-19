package dispatchoutbox

import (
	"context"
	"errors"
	"fmt"
	"strings"

	keyspkg "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// claimKeyPrefixes는 dispatch envelope에 보존되는 알림 및 일정 변경 dedup 마커의 SSOT prefix다.
// 이 prefix만 삭제 대상으로 허용해, dispatch claim 등
// 다른 키가 잘못 해제되는 것을 막는다.
var claimKeyPrefixes = [...]string{
	keyspkg.NotifyClaimKeyPrefix,
	keyspkg.RoomScheduleTransitionKeyPrefix,
	keyspkg.LogicalScheduleTransitionKeyPrefix,
}

// ClaimKeyReleaser는 Consumer가 dedup claim 키를 삭제할 때 의존하는 narrow interface다.
// 이 interface는 cache.Client(god interface)가 그대로 만족한다. NewConsumer의 필수 인자다.
type ClaimKeyReleaser interface {
	DelMany(ctx context.Context, keys []string) (int64, error)
}

// ReleaseClaimKeys는 소유권이 확인된 DLQ/drop 전이 후 미발송 delivery의 dedup claim을 삭제합니다.
// 성공·retry·결과 불명에는 호출하지 않습니다. 삭제를 맡을 releaser가 없으면 오류를 반환합니다.
func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c == nil {
		return errors.New("release claim keys: claim key releaser is not configured")
	}

	return releaseClaimKeys(ctx, c.claimReleaser, claimKeys)
}

// releaseClaimKeys는 허용 prefix의 dedup claim 키만 releaser로 삭제한다. 삭제를 맡을 releaser가 없으면 오류다.
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
