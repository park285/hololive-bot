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

	sessions, err := s.queryPersistedRecentSessions(ctx, uniqueChannelIDs, now)
	if err != nil {
		return nil, err
	}
	if err := s.attachOfficialCollaboTalentNames(ctx, sessions); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *PgYouTubeLiveSessionSource) queryPersistedRecentSessions(
	ctx context.Context,
	channelIDs []string,
	now time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	liveSince := now.UTC().Add(-s.effectiveLiveRecentWindow())
	upcomingUntil := now.UTC().Add(s.effectiveUpcomingLookahead())

	var rows []domain.YouTubeLiveSession
	if err := pgxscan.Select(ctx, s.pool, &rows, mustSQL("youtube_live_session_source_0085_01.sql"), channelIDs, domain.LiveStatusLive, liveSince, domain.LiveStatusUpcoming, now.UTC(), upcomingUntil, liveSince); err != nil {
		return nil, err
	}
	return persistedSessionsFromLiveRows(rows), nil
}

func persistedSessionsFromLiveRows(rows []domain.YouTubeLiveSession) []PersistedYouTubeLiveSession {
	sessions := make([]PersistedYouTubeLiveSession, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		stream := streamFromYouTubeLiveSession(row)
		if stream == nil {
			continue
		}
		sessions = append(sessions, PersistedYouTubeLiveSession{
			Stream:          stream,
			LastSeenAt:      row.LastSeenAt.UTC(),
			LiveFirstSeenAt: utcTimeValue(row.LiveFirstSeenAt),
		})
	}
	return sessions
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
	if err := pgxscan.Select(ctx, s.pool, &rows, mustSQL("youtube_live_session_source_0132_02.sql"), uniqueChannelIDs, domain.LiveStatusLive, liveSince); err != nil {
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
	if err := pgxscan.Select(ctx, s.pool, &rows, mustSQL("youtube_live_session_source_0175_03.sql"), persistedAlarmDispatchEventLiveType, streamIDs, since.UTC()); err != nil {
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
	if err := pgxscan.Select(ctx, s.pool, &rows, mustSQL("youtube_live_session_source_0231_04.sql"), persistedAlarmDispatchEventLiveType, streamIDs, string(dispatchoutbox.StatusSent), since.UTC()); err != nil {
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
		IsPremiere:     row.IsPremiere != nil && *row.IsPremiere,
	}
	if topicID := strings.TrimSpace(row.TopicID); topicID != "" {
		stream.TopicID = &topicID
	}
	if thumbnailURL := strings.TrimSpace(row.ThumbnailURL); thumbnailURL != "" {
		stream.Thumbnail = &thumbnailURL
	}
	return stream
}

type officialScheduleCollaboTalentRow struct {
	VideoID            string   `db:"video_id"`
	CollaboTalentNames []string `db:"collabo_talent_names"`
}

func (s *PgYouTubeLiveSessionSource) attachOfficialCollaboTalentNames(
	ctx context.Context,
	sessions []PersistedYouTubeLiveSession,
) error {
	videoIDs := persistedSessionVideoIDs(sessions)
	if len(videoIDs) == 0 {
		return nil
	}
	namesByVideo, err := s.loadOfficialCollaboTalentNames(ctx, videoIDs)
	if err != nil {
		return err
	}
	for i := range sessions {
		stream := sessions[i].Stream
		if stream == nil {
			continue
		}
		names, ok := namesByVideo[stream.ID]
		if !ok {
			continue
		}
		stream.CollaboTalentNames = names
	}
	return nil
}

func persistedSessionVideoIDs(sessions []PersistedYouTubeLiveSession) []string {
	videoIDs := make([]string, 0, len(sessions))
	for i := range sessions {
		if sessions[i].Stream == nil {
			continue
		}
		videoID := strings.TrimSpace(sessions[i].Stream.ID)
		if videoID == "" {
			continue
		}
		videoIDs = append(videoIDs, videoID)
	}
	return UniqueStrings(videoIDs)
}

func (s *PgYouTubeLiveSessionSource) loadOfficialCollaboTalentNames(
	ctx context.Context,
	videoIDs []string,
) (map[string][]string, error) {
	var rows []officialScheduleCollaboTalentRow
	if err := pgxscan.Select(ctx, s.pool, &rows, mustSQL("youtube_schedule_collabo_talents_0232_05.sql"), videoIDs); err != nil {
		return nil, err
	}
	namesByVideo := make(map[string][]string, len(rows))
	for i := range rows {
		videoID := strings.TrimSpace(rows[i].VideoID)
		if videoID == "" {
			continue
		}
		namesByVideo[videoID] = cloneStringSlice(rows[i].CollaboTalentNames)
	}
	return namesByVideo, nil
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
