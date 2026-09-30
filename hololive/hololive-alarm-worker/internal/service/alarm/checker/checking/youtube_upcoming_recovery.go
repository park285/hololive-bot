package checking

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
)

type upcomingSubscriptionKey struct{ channel, title string }

func withoutUpcomingNotifications(notifications []*domain.AlarmNotification) []*domain.AlarmNotification {
	result := notifications[:0]
	for _, n := range notifications {
		if n == nil || n.Stream == nil || !n.Stream.IsUpcoming() {
			result = append(result, n)
		}
	}

	return result
}

// Go 종료 판단에는 이번 provider 조회의 응답만 전달한다. Canonical 종료·일정 변경은
// Pending SQL이 필드별 실제 관측 시각 > selected_at을 검증하므로 과거 보강 데이터로 다시 판단하지 않는다.
func (c *YouTubeChecker) recoverUpcomingCandidates(ctx context.Context, streams map[string][]*domain.Stream, now time.Time) ([]*domain.AlarmNotification, error) {
	pending, err := c.upcomingCandidates.Pending(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load pending: %w", err)
	}

	result := make([]*domain.AlarmNotification, 0, len(pending))
	subscriptions := make(map[upcomingSubscriptionKey][]string)

	for i := range pending {
		candidate := &pending[i]
		notification := candidate.Notification
		reason := upcomingCandidateTermination(candidate, streams[candidate.ChannelID], now)

		if reason == "" {
			reason, err = c.upcomingSubscriptionTermination(ctx, candidate, subscriptions)
			if err != nil {
				return nil, err
			}
		}

		if reason != "" {
			if err := c.upcomingCandidates.Finish(ctx, candidate.DedupeKey, reason, now); err != nil {
				return nil, fmt.Errorf("terminate pending: %w", err)
			}

			continue
		}

		result = append(result, &notification)
	}

	return result, nil
}

func (c *YouTubeChecker) upcomingSubscriptionTermination(ctx context.Context, candidate *dispatchoutbox.UpcomingCandidate, subscriptions map[upcomingSubscriptionKey][]string) (string, error) {
	notification := &candidate.Notification
	if notification.Stream == nil {
		return "expired", nil
	}

	key := upcomingSubscriptionKey{candidate.ChannelID, notification.Stream.Title}
	rooms, ok := subscriptions[key]

	if !ok {
		// 취소 판단은 cache 누락이 아닌 현재 DB 구독 사실에만 근거한다.
		var err error

		rooms, err = sharedalarm.ResolveEventSubscribers(ctx, nil, c.subscriptionDB, key.channel, key.title, domain.AlarmTypeLive)
		if err != nil {
			return "", fmt.Errorf("verify pending subscriptions: %w", err)
		}

		subscriptions[key] = rooms
	}

	if !slices.Contains(rooms, notification.RoomID) {
		return "subscription_removed", nil
	}

	return "", nil
}

func upcomingCandidateTermination(candidate *dispatchoutbox.UpcomingCandidate, streams []*domain.Stream, now time.Time) string {
	snapshot := candidate.Notification.Stream
	if snapshot == nil || snapshot.StartScheduled == nil || !snapshot.StartScheduled.After(now) {
		return "expired"
	}

	for _, current := range streams {
		if current == nil || current.ID != snapshot.ID {
			continue
		}

		if current.IsLive() || current.IsPast() || current.IsPremiere {
			return "stream_ended"
		}

		if current.StartScheduled != nil && !current.StartScheduled.Equal(*snapshot.StartScheduled) {
			return "schedule_changed"
		}
	}

	// provider 목록 누락만으로 취소를 추정하지 않는다.
	return ""
}

func cloneCurrentProviderStreams(streams map[string][]*domain.Stream) map[string][]*domain.Stream {
	result := make(map[string][]*domain.Stream, len(streams))
	for channel, items := range streams {
		copied := make([]*domain.Stream, 0, len(items))
		for _, item := range items {
			copied = append(copied, CloneStream(item))
		}

		result[channel] = copied
	}

	return result
}
