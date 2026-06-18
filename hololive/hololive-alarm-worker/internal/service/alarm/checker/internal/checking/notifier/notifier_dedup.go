package notifier

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func (n *Notifier) claimDedup(ctx context.Context, payload *sendInput) (claimKeys []string, claimed bool, err error) {
	category := keys.NotificationCategory(
		n.dedupService.TargetMinutesSnapshot(),
		payload.notification.MinutesUntil,
	)
	notifyKey := keys.BuildNotifyClaimKey(
		payload.notification.RoomID,
		payload.streamID,
		payload.startScheduled,
		category,
	)
	logicalKey := keys.BuildLogicalEventClaimKey(
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream.ID,
		payload.notification.Stream.Title,
		payload.startScheduled,
		category,
	)

	notifyClaimed, logicalClaimed := n.dedupService.TryClaimPair(
		ctx, notifyKey, logicalKey, constants.CacheTTL.NotificationSent,
	)

	if !notifyClaimed {
		if logicalClaimed {
			n.releaseClaimsBestEffort(ctx, []string{logicalKey}, "release logical claim after notification dedup skip")
		}
		return nil, false, nil
	}
	if !logicalClaimed {
		n.releaseClaimsBestEffort(ctx, []string{notifyKey}, "release notification claim after logical dedup skip")
		return nil, false, nil
	}

	claimKeys = compactClaimKeys(notifyKey, logicalKey)
	scheduleClaimKeys, scheduleClaimed, err := n.claimScheduleChangeDedup(ctx, payload)
	if err != nil {
		n.releaseClaimsBestEffort(ctx, append(claimKeys, scheduleClaimKeys...), "failed to release claims after schedule change claim error")
		return nil, false, fmt.Errorf("claim schedule change: %w", err)
	}
	if !scheduleClaimed {
		n.releaseClaimsBestEffort(ctx, append(claimKeys, scheduleClaimKeys...), "failed to release claims after schedule change dedup skip")
		return nil, false, nil
	}

	return append(claimKeys, scheduleClaimKeys...), true, nil
}

func (n *Notifier) claimScheduleChangeDedup(ctx context.Context, payload *sendInput) (claimKeys []string, claimed bool, err error) {
	if payload == nil || payload.notification == nil {
		return nil, true, nil
	}
	if strings.TrimSpace(payload.notification.ScheduleChangeMessage) == "" {
		return nil, true, nil
	}

	claimKeys, claimed, err = n.dedupService.TryClaimNotificationScheduleChange(
		ctx,
		payload.notification.RoomID,
		payload.channelID,
		payload.notification.Stream,
		payload.notification.ScheduleChangePreviousStart,
	)
	if err != nil {
		return claimKeys, false, fmt.Errorf("claim notification schedule change: %w", err)
	}
	if !claimed {
		return claimKeys, false, nil
	}

	return claimKeys, true, nil
}

func compactClaimKeys(rawKeys ...string) []string {
	if len(rawKeys) == 0 {
		return nil
	}

	compacted := make([]string, 0, len(rawKeys))
	for _, key := range rawKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}

		compacted = append(compacted, key)
	}

	return compacted
}
