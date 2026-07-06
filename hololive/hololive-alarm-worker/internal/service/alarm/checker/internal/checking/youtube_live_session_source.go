package checking

import (
	"context"
	"strings"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

const (
	defaultPersistedLiveSessionRecentWindow  = 15 * time.Minute
	defaultPersistedUpcomingSessionLookahead = 30 * time.Minute
	persistedAlarmDispatchEventLiveType      = string(domain.AlarmTypeLive)
)

type PgYouTubeLiveSessionSourceOptions struct {
	LiveRecentWindow  time.Duration
	UpcomingLookahead time.Duration
}

type PgYouTubeLiveSessionSource struct {
	pool              *pgxpool.Pool
	liveRecentWindow  time.Duration
	upcomingLookahead time.Duration
}

func NewPgYouTubeLiveSessionSource(postgres database.Client) YouTubeLiveSessionSource {
	return NewPgYouTubeLiveSessionSourceWithOptions(postgres, PgYouTubeLiveSessionSourceOptions{})
}

func NewPgYouTubeLiveSessionSourceWithOptions(
	postgres database.Client,
	options PgYouTubeLiveSessionSourceOptions,
) YouTubeLiveSessionSource {
	if postgres == nil {
		return nil
	}
	pool := postgres.GetPool()
	if pool == nil {
		return nil
	}
	return newPgYouTubeLiveSessionSource(pool, options)
}

func newPgYouTubeLiveSessionSource(
	pool *pgxpool.Pool,
	options PgYouTubeLiveSessionSourceOptions,
) *PgYouTubeLiveSessionSource {
	if options.LiveRecentWindow <= 0 {
		options.LiveRecentWindow = defaultPersistedLiveSessionRecentWindow
	}
	if options.UpcomingLookahead <= 0 {
		options.UpcomingLookahead = defaultPersistedUpcomingSessionLookahead
	}
	return &PgYouTubeLiveSessionSource{
		pool:              pool,
		liveRecentWindow:  options.LiveRecentWindow,
		upcomingLookahead: options.UpcomingLookahead,
	}
}

func (s *PgYouTubeLiveSessionSource) LoadRecentSessions(
	ctx context.Context,
	channelIDs []string,
	now time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	if s == nil || s.pool == nil || len(channelIDs) == 0 {
		return nil, nil
	}

	uniqueChannelIDs := UniqueStrings(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return nil, nil
	}

	liveSince := now.UTC().Add(-s.effectiveLiveRecentWindow())
	upcomingUntil := now.UTC().Add(s.effectiveUpcomingLookahead())

	var rows []domain.YouTubeLiveSession
	if err := pgxscan.Select(ctx, s.pool, &rows, `
		SELECT video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at,
		       live_first_seen_at, topic_id, thumbnail_url, last_seen_at
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		  AND (
		      (status = $2 AND last_seen_at >= $3)
		      OR (status = $4 AND scheduled_start_time >= $5 AND scheduled_start_time <= $6 AND last_seen_at >= $7)
		  )
		ORDER BY last_seen_at DESC
	`, uniqueChannelIDs, domain.LiveStatusLive, liveSince, domain.LiveStatusUpcoming, now.UTC(), upcomingUntil, liveSince); err != nil {
		return nil, err
	}

	sessions := make([]PersistedYouTubeLiveSession, 0, len(rows))
	for _, row := range rows {
		stream := streamFromYouTubeLiveSession(&row)
		if stream == nil {
			continue
		}
		sessions = append(sessions, PersistedYouTubeLiveSession{
			Stream:          stream,
			LastSeenAt:      row.LastSeenAt.UTC(),
			LiveFirstSeenAt: utcTimeValue(row.LiveFirstSeenAt),
		})
	}
	return sessions, nil
}

func (s *PgYouTubeLiveSessionSource) LoadRecentLiveChannelIDs(
	ctx context.Context,
	channelIDs []string,
	now time.Time,
) ([]string, error) {
	if s == nil || s.pool == nil || len(channelIDs) == 0 {
		return nil, nil
	}

	uniqueChannelIDs := UniqueStrings(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return nil, nil
	}

	liveSince := now.UTC().Add(-s.effectiveLiveRecentWindow())

	var rows []string
	if err := pgxscan.Select(ctx, s.pool, &rows, `
		SELECT DISTINCT channel_id
		FROM youtube_live_sessions
		WHERE channel_id = ANY($1)
		  AND status = $2
		  AND last_seen_at >= $3
		ORDER BY channel_id
	`, uniqueChannelIDs, domain.LiveStatusLive, liveSince); err != nil {
		return nil, err
	}
	return UniqueStrings(rows), nil
}

func (s *PgYouTubeLiveSessionSource) effectiveLiveRecentWindow() time.Duration {
	if s.liveRecentWindow > 0 {
		return s.liveRecentWindow
	}
	return defaultPersistedLiveSessionRecentWindow
}

func (s *PgYouTubeLiveSessionSource) effectiveUpcomingLookahead() time.Duration {
	if s.upcomingLookahead > 0 {
		return s.upcomingLookahead
	}
	return defaultPersistedUpcomingSessionLookahead
}

func (s *PgYouTubeLiveSessionSource) RecentlyDispatchedStreamIDs(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	if s == nil || s.pool == nil || len(streamIDs) == 0 {
		return result, nil
	}

	streamIDs = UniqueStrings(streamIDs)
	if len(streamIDs) == 0 {
		return result, nil
	}

	var rows []string
	if err := pgxscan.Select(ctx, s.pool, &rows, `
		SELECT DISTINCT stream_id
		FROM alarm_dispatch_events
		WHERE alarm_type = $1::alarm_type
		  AND stream_id = ANY($2)
		  AND created_at >= $3
	`, persistedAlarmDispatchEventLiveType, streamIDs, since.UTC()); err != nil {
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

func (s *PgYouTubeLiveSessionSource) RecentlySentLiveStreamRooms(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
) (map[string]map[string]struct{}, error) {
	result := make(map[string]map[string]struct{})
	streamIDs, ok := s.normalizedStreamIDs(streamIDs)
	if !ok {
		return result, nil
	}

	rows, err := s.queryRecentlySentLiveStreamRooms(ctx, streamIDs, since)
	if err != nil {
		return nil, err
	}
	return sentLiveStreamRoomsByStreamID(rows), nil
}

func (s *PgYouTubeLiveSessionSource) normalizedStreamIDs(streamIDs []string) ([]string, bool) {
	if s == nil || s.pool == nil || len(streamIDs) == 0 {
		return nil, false
	}
	streamIDs = UniqueStrings(streamIDs)
	return streamIDs, len(streamIDs) > 0
}

type sentLiveStreamRoomRow struct {
	StreamID string
	RoomID   string
}

func (s *PgYouTubeLiveSessionSource) queryRecentlySentLiveStreamRooms(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
) ([]sentLiveStreamRoomRow, error) {
	var rows []sentLiveStreamRoomRow
	if err := pgxscan.Select(ctx, s.pool, &rows, `
		SELECT e.stream_id, d.room_id
		FROM alarm_dispatch_events AS e
		JOIN alarm_dispatch_deliveries AS d ON d.event_id = e.id
		WHERE e.alarm_type = $1::alarm_type
		  AND e.stream_id = ANY($2)
		  AND d.status = $3
		  AND d.sent_at >= $4
	`, persistedAlarmDispatchEventLiveType, streamIDs, string(dispatchoutbox.StatusSent), since.UTC()); err != nil {
		return nil, err
	}
	return rows, nil
}

func sentLiveStreamRoomsByStreamID(rows []sentLiveStreamRoomRow) map[string]map[string]struct{} {
	result := make(map[string]map[string]struct{})
	for _, row := range rows {
		streamID := strings.TrimSpace(row.StreamID)
		roomID := strings.TrimSpace(row.RoomID)
		if streamID == "" || roomID == "" {
			continue
		}
		if result[streamID] == nil {
			result[streamID] = make(map[string]struct{})
		}
		result[streamID][roomID] = struct{}{}
	}
	return result
}

func streamFromYouTubeLiveSession(row *domain.YouTubeLiveSession) *domain.Stream {
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
	link := domain.YouTubeWatchURL(videoID)
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
	if topicID := strings.TrimSpace(row.TopicID); topicID != "" {
		stream.TopicID = &topicID
	}
	if thumbnailURL := strings.TrimSpace(row.ThumbnailURL); thumbnailURL != "" {
		stream.Thumbnail = &thumbnailURL
	}
	return stream
}

func persistedLiveStatusToStreamStatus(status domain.LiveStatus) (domain.StreamStatus, bool) {
	switch status {
	case domain.LiveStatusLive:
		return domain.StreamStatusLive, true
	case domain.LiveStatusUpcoming:
		return domain.StreamStatusUpcoming, true
	case domain.LiveStatusEnded:
		return "", false
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

func utcTimeValue(value *time.Time) time.Time {
	if value == nil || value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}
