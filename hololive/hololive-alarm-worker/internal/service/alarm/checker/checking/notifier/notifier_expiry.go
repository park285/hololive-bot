package notifier

import (
	"context"
	"time"
)

// Claim 대기와 병렬 prepare 합류 뒤에도 미발행 후보가 시작 시각을 넘기지 않게 한다.
func splitExpiredUpcoming(items []claimedSend, now time.Time) (active, expired []claimedSend) {
	active = make([]claimedSend, 0, len(items))
	for _, item := range items {
		notification := item.payload.notification
		if notification.Stream != nil && notification.Stream.IsUpcoming() && !item.payload.startScheduled.After(now) {
			expired = append(expired, item)
			continue
		}

		active = append(active, item)
	}

	return active, expired
}

func (n *Notifier) releaseExpiredClaims(ctx context.Context, items []claimedSend) {
	var keys []string

	for _, item := range items {
		keys = append(keys, item.claimKeys...)
	}

	n.releaseClaimsBestEffort(ctx, keys, "failed to release expired upcoming claims")
}
