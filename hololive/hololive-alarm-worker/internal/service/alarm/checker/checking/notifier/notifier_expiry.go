package notifier

import (
	"context"
	"time"
)

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
