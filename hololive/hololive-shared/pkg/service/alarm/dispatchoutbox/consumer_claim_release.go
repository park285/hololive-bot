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
	keyspkg.ScheduleTransitionKeyPrefix,
}

// ClaimKeyReleaser는 Consumer가 dedup claim 키를 삭제할 때 의존하는 narrow interface다.
// 이 interface는 cache.Client(god interface)가 그대로 만족한다. NewConsumer의 필수 인자다.
type ClaimKeyReleaser interface {
	DelMany(ctx context.Context, keys []string) (int64, error)
}

// ReleaseClaimKeys는 미발송이 확정된 DLQ/drop delivery의 dedup claim 키를 삭제합니다.
// Consumer의 payload 거절 경로와 alarm_dispatch_runner는 worker 소유권을 검증한
// DLQ 전이가 성공한 뒤에만 호출합니다. 성공·retry·전송 결과 불명에는 호출하지 않습니다.
// 주입된 releaser가 없을 때 삭제 없이 TTL 만료에 기대던 no-op 경로는 지웠다(stack-audit 2026-09-26 T11). 구성 누락은
// 오류로 돌려주어, 미발송 delivery의 dedup 키가 남아 재발송을 막는 상태를 조용히 만들지 않는다.
func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c == nil || c.claimReleaser == nil {
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

	if _, err := c.claimReleaser.DelMany(ctx, filtered); err != nil {
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
