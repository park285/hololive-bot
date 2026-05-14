package checker

import (
	"context"
	"strings"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"gorm.io/gorm"
)

const (
	persistedLiveSessionRecentWindow    = sharedconstants.LiveCatchupWindow
	persistedUpcomingSessionLookahead   = 30 * time.Minute
	persistedAlarmDispatchEventLiveType = string(domain.AlarmTypeLive)
)

type PgYouTubeLiveSessionSource struct {
	db *gorm.DB
}

func NewPgYouTubeLiveSessionSource(postgres database.Client) YouTubeLiveSessionSource {
	if postgres == nil {
		return nil
	}
	db := postgres.GetGormDB()
	if db == nil {
		return nil
	}
	return &PgYouTubeLiveSessionSource{db: db}
}

func (s *PgYouTubeLiveSessionSource) LoadRecentSessions(
	ctx context.Context,
	channelIDs []string,
	now time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	if s == nil || s.db == nil || len(channelIDs) == 0 {
		return nil, nil
	}

	uniqueChannelIDs := uniqueStrings(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return nil, nil
	}

	liveSince := now.UTC().Add(-persistedLiveSessionRecentWindow)
	upcomingUntil := now.UTC().Add(persistedUpcomingSessionLookahead)

	var rows []domain.YouTubeLiveSession
	err := s.db.WithContext(ctx).
		Where(
			`channel_id IN ? AND (
				(status = ? AND last_seen_at >= ?)
				OR (status = ? AND scheduled_start_time >= ? AND scheduled_start_time <= ? AND last_seen_at >= ?)
			)`,
			uniqueChannelIDs,
			domain.LiveStatusLive,
			liveSince,
			domain.LiveStatusUpcoming,
			now.UTC(),
			upcomingUntil,
			liveSince,
		).
		Order("last_seen_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	sessions := make([]PersistedYouTubeLiveSession, 0, len(rows))
	for _, row := range rows {
		stream := streamFromYouTubeLiveSession(row)
		if stream == nil {
			continue
		}
		sessions = append(sessions, PersistedYouTubeLiveSession{
			Stream:     stream,
			LastSeenAt: row.LastSeenAt.UTC(),
		})
	}
	return sessions, nil
}

func (s *PgYouTubeLiveSessionSource) RecentlyDispatchedStreamIDs(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if s == nil || s.db == nil || len(streamIDs) == 0 {
		return result, nil
	}

	streamIDs = uniqueStrings(streamIDs)
	if len(streamIDs) == 0 {
		return result, nil
	}

	var rows []string
	err := s.db.WithContext(ctx).
		Table("alarm_dispatch_events").
		Distinct("stream_id").
		Where("alarm_type = ? AND stream_id IN ? AND created_at >= ?", persistedAlarmDispatchEventLiveType, streamIDs, since.UTC()).
		Pluck("stream_id", &rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		streamID := strings.TrimSpace(row)
		if streamID == "" {
			continue
		}
		result[streamID] = struct{}{}
	}
	return result, nil
}

func streamFromYouTubeLiveSession(row domain.YouTubeLiveSession) *domain.Stream {
	videoID := strings.TrimSpace(row.VideoID)
	channelID := strings.TrimSpace(row.ChannelID)
	if videoID == "" || channelID == "" {
		return nil
	}

	status, ok := persistedLiveStatusToStreamStatus(row.Status)
	if !ok {
		return nil
	}

	title := strings.TrimSpace(row.Title)
	if title == "" {
		title = "YouTube 라이브"
	}
	link := "https://youtube.com/watch?v=" + videoID
	stream := &domain.Stream{
		ID:             videoID,
		Title:          title,
		ChannelID:      channelID,
		ChannelName:    channelID,
		Status:         status,
		StartScheduled: utcTimePtr(row.ScheduledStartTime),
		StartActual:    utcTimePtr(row.StartedAt),
		Link:           &link,
		Channel:        &domain.Channel{ID: channelID, Name: channelID},
	}
	return stream
}

func persistedLiveStatusToStreamStatus(status domain.LiveStatus) (domain.StreamStatus, bool) {
	switch status {
	case domain.LiveStatusLive:
		return domain.StreamStatusLive, true
	case domain.LiveStatusUpcoming:
		return domain.StreamStatusUpcoming, true
	default:
		return "", false
	}
}

func utcTimePtr(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	utc := value.UTC()
	return &utc
}
