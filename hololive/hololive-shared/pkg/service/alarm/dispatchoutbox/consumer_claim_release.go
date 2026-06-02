package dispatchoutbox

import (
	"context"
	"fmt"
	"strings"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

// claimKeyPrefix는 dedup 마커(notified:claim:, notified:claim:event:) SSOT prefix다.
// queue.Consumer와 동일하게 이 prefix만 삭제 대상으로 허용해, dispatch claim 등
// 다른 키가 잘못 해제되는 것을 막는다.
const claimKeyPrefix = contractsalarm.NotifyClaimKeyPrefix

// ClaimKeyReleaser는 Consumer가 dedup claim 키를 삭제할 때 의존하는 narrow interface다.
// cache.Client(god interface)가 그대로 만족한다. nil이면 ReleaseClaimKeys는 no-op로
// 동작해 기존 PG 모드 동작(삭제 없이 TTL 만료 의존)을 보존한다.
type ClaimKeyReleaser interface {
	DelMany(ctx context.Context, keys []string) (int64, error)
}

// WithClaimKeyReleaser는 PG 모드 dispatch consumer가 DLQ/drop 종료 후 dedup claim 키를
// 실제로 삭제하도록 Valkey releaser를 주입한다. 주입하지 않으면 ReleaseClaimKeys는
// no-op로 남아 claim 키는 NotificationSent TTL 만료로만 정리된다.
func WithClaimKeyReleaser(releaser ClaimKeyReleaser) ConsumerOption {
	return func(c *Consumer) {
		if releaser != nil {
			c.claimReleaser = releaser
		}
	}
}

// ReleaseClaimKeys는 전달이 확정 종료된(DLQ/quarantine/drop) envelope의 dedup claim 키를
// 삭제한다. 호출부(alarm_dispatch_runner)는 MoveToDLQ 이후의 DLQ envelope에 대해서만 이를
// 호출하므로, 정상 전달 경로에서는 claim 키가 삭제되지 않아 재알림이 발생하지 않는다.
// releaser가 주입되지 않으면 no-op로 남아 기존 PG 모드 동작(TTL 만료 의존)을 보존한다.
func (c *Consumer) ReleaseClaimKeys(ctx context.Context, claimKeys []string) error {
	if c == nil || c.claimReleaser == nil {
		return nil
	}

	filtered := make([]string, 0, len(claimKeys))
	for _, key := range claimKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" && strings.HasPrefix(trimmed, claimKeyPrefix) {
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
