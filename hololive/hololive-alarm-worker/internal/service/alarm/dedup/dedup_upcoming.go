package dedup

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (s *Service) MarkUpcomingEventNotified(ctx context.Context, roomID, channelID string, stream *domain.Stream) error {
	if stream == nil {
		return nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	key := keys.BuildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)
	data := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.cache.Set(ctx, key, data, NotificationSentTTL); err != nil {
		return fmt.Errorf("mark upcoming event notified: set cache key: %w", err)
	}

	if err := s.MarkLogicalScheduleObserved(ctx, roomID, channelID, stream); err != nil {
		return fmt.Errorf("mark upcoming event notified: mark logical schedule observed: %w", err)
	}

	return nil
}

func (s *Service) RecentlyNotifiedUpcomingRooms(
	ctx context.Context,
	roomIDs []string,
	channelID string,
	stream *domain.Stream,
	window time.Duration,
) (recent map[string]struct{}, failed map[string]error) {
	recent = make(map[string]struct{})
	failed = make(map[string]error)

	if len(roomIDs) == 0 || stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() || window <= 0 {
		return recent, failed
	}

	markerKeys := make([]string, len(roomIDs))
	cmds := make([]valkey.Completed, len(roomIDs))

	for i, roomID := range roomIDs {
		markerKeys[i] = keys.BuildUpcomingEventKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled)
		cmds[i] = s.cache.B().Get().Key(markerKeys[i]).Build()
	}

	results := s.cache.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		err := fmt.Errorf("recently notified upcoming rooms: unexpected result count %d for %d rooms", len(results), len(cmds))

		for _, roomID := range roomIDs {
			failed[roomID] = err
		}

		return recent, failed
	}

	for i, roomID := range roomIDs {
		notified, err := s.upcomingMarkerWithin(results[i], markerKeys[i], window)
		if err != nil {
			failed[roomID] = fmt.Errorf("recently notified upcoming rooms: %w", err)

			continue
		}

		if notified {
			recent[roomID] = struct{}{}
		}
	}

	return recent, failed
}

func (s *Service) upcomingMarkerWithin(result valkey.ValkeyResult, key string, window time.Duration) (bool, error) {
	raw, err := result.AsBytes()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("get upcoming event marker: %w", err)
	}

	var data UpcomingEventNotifiedData

	if err = jsonv2.Unmarshal(raw, &data); err != nil {
		return false, fmt.Errorf("decode upcoming event marker: %w", err)
	}

	if data.NotifiedAt == "" {
		return false, nil
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		s.logger.Warn("upcoming event notified timestamp corrupted, treating as not notified",
			slog.String("upcoming_key_token", privacylog.Pseudonym(key)),
			slog.String("notified_at", data.NotifiedAt),
			slog.Any("error", err),
		)

		return false, nil
	}

	return time.Since(notifiedAt) <= window, nil
}
