package checking

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

const (
	persistedLiveSessionRecentWindow    = 15 * time.Minute
	persistedUpcomingSessionLookahead   = 30 * time.Minute
	persistedAlarmDispatchEventLiveType = string(domain.AlarmTypeLive)
)

type PgYouTubeLiveSessionSource struct {
	pool *pgxpool.Pool
}

func NewPgYouTubeLiveSessionSource(postgres database.Client) YouTubeLiveSessionSource {
	if postgres == nil {
		return nil
	}

	pool := postgres.GetPool()
	if pool == nil {
		return nil
	}

	return &PgYouTubeLiveSessionSource{pool: pool}
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
		return nil, fmt.Errorf("query persisted recent sessions: %w", err)
	}

	if err := s.attachOfficialCollaboTalentNames(ctx, sessions); err != nil {
		return nil, fmt.Errorf("attach official collabo talent names: %w", err)
	}

	return sessions, nil
}

func (s *PgYouTubeLiveSessionSource) queryPersistedRecentSessions(
	ctx context.Context,
	channelIDs []string,
	now time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	liveSince := now.UTC().Add(-persistedLiveSessionRecentWindow)
	upcomingUntil := now.UTC().Add(persistedUpcomingSessionLookahead)

	var rows []domain.YouTubeLiveSession

	if err := dbx.SelectSQL(ctx, s.pool, &rows, "persisted recent sessions", queryPersistedRecentSessionsSQL, channelIDs, domain.LiveStatusLive, liveSince, domain.LiveStatusUpcoming, now.UTC(), upcomingUntil, liveSince); err != nil {
		return nil, fmt.Errorf("select: %w", err)
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

		observedAt := row.ScheduleObservedAt
		if row.Status == domain.LiveStatusLive {
			observedAt = row.StatusObservedAt
		}

		sessions = append(sessions, PersistedYouTubeLiveSession{
			Stream:          stream,
			ObservedAt:      utcTimeValue(observedAt),
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

	liveSince := now.UTC().Add(-persistedLiveSessionRecentWindow)

	var rows []string

	if err := dbx.SelectSQL(ctx, s.pool, &rows, "recent live channel ids", loadRecentLiveChannelIDsSQL, uniqueChannelIDs, domain.LiveStatusLive, liveSince, now.UTC()); err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}

	return UniqueStrings(rows), nil
}

func (s *PgYouTubeLiveSessionSource) RecentlyDispatchedStreamIDs(
	ctx context.Context,
	sinceByStreamID map[string]time.Time,
) (map[string]struct{}, error) {
	result := make(map[string]struct{})

	streamIDs, sinces, ok := s.liveDispatchWindows(sinceByStreamID)
	if !ok {
		return result, nil
	}

	var rows []string

	if err := dbx.SelectSQL(ctx, s.pool, &rows, "recently dispatched stream ids", recentlyDispatchedStreamIDsSQL, persistedAlarmDispatchEventLiveType, streamIDs, sinces); err != nil {
		return nil, fmt.Errorf("select: %w", err)
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

func (s *PgYouTubeLiveSessionSource) RecentlySettledLiveStreamRooms(
	ctx context.Context,
	sinceByStreamID map[string]time.Time,
) (LiveStreamRoomSettlements, error) {
	streamIDs, sinces, ok := s.liveDispatchWindows(sinceByStreamID)
	if !ok {
		return newLiveStreamRoomSettlements(), nil
	}

	rows, err := s.queryRecentlySettledLiveStreamRooms(ctx, streamIDs, sinces)
	if err != nil {
		return LiveStreamRoomSettlements{}, fmt.Errorf("query recently settled live stream rooms: %w", err)
	}

	return settledLiveStreamRooms(rows), nil
}

func (s *PgYouTubeLiveSessionSource) liveDispatchWindows(sinceByStreamID map[string]time.Time) ([]string, []time.Time, bool) {
	if s == nil || s.pool == nil || len(sinceByStreamID) == 0 {
		return nil, nil, false
	}

	streamIDs := make([]string, 0, len(sinceByStreamID))
	sinces := make([]time.Time, 0, len(sinceByStreamID))

	for _, streamID := range slices.Sorted(maps.Keys(sinceByStreamID)) {
		if streamID == "" {
			continue
		}

		streamIDs = append(streamIDs, streamID)
		sinces = append(sinces, sinceByStreamID[streamID].UTC())
	}

	return streamIDs, sinces, len(streamIDs) > 0
}

func (s *PgYouTubeLiveSessionSource) normalizedStreamIDs(streamIDs []string) ([]string, bool) {
	if s == nil || s.pool == nil || len(streamIDs) == 0 {
		return nil, false
	}

	streamIDs = UniqueStrings(streamIDs)

	return streamIDs, len(streamIDs) > 0
}

type settledLiveStreamRoomRow struct {
	StreamID string
	RoomID   string
	Sent     bool
}

var terminalFailedDeliveryStatuses = []string{
	string(dispatchoutbox.StatusDLQ),
	string(dispatchoutbox.StatusQuarantined),
	string(dispatchoutbox.StatusCancelled),
}

func (s *PgYouTubeLiveSessionSource) queryRecentlySettledLiveStreamRooms(
	ctx context.Context,
	streamIDs []string,
	sinces []time.Time,
) ([]settledLiveStreamRoomRow, error) {
	var rows []settledLiveStreamRoomRow

	if err := dbx.SelectSQL(ctx, s.pool, &rows, "recently settled live stream rooms", queryRecentlySettledLiveStreamRoomsSQL, persistedAlarmDispatchEventLiveType, streamIDs, string(dispatchoutbox.StatusSent), sinces, terminalFailedDeliveryStatuses, alarmkeys.NotificationCategoryLiveCatchup); err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}

	return rows, nil
}

func settledLiveStreamRooms(rows []settledLiveStreamRoomRow) LiveStreamRoomSettlements {
	result := newLiveStreamRoomSettlements()

	for _, row := range rows {
		streamID := strings.TrimSpace(row.StreamID)
		roomID := strings.TrimSpace(row.RoomID)

		if streamID == "" || roomID == "" {
			continue
		}

		target := result.TerminalFailed

		if row.Sent {
			target = result.Sent
		}

		addStreamRoom(target, streamID, roomID)
	}

	return result
}

func addStreamRoom(roomsByStreamID map[string]map[string]struct{}, streamID, roomID string) {
	if roomsByStreamID[streamID] == nil {
		roomsByStreamID[streamID] = make(map[string]struct{})
	}

	roomsByStreamID[streamID][roomID] = struct{}{}
}

func (s *PgYouTubeLiveSessionSource) LoadConfirmedPremiereIDs(
	ctx context.Context,
	videoIDs []string,
) (map[string]struct{}, error) {
	result := make(map[string]struct{})
	videoIDs, ok := s.normalizedStreamIDs(videoIDs)

	if !ok {
		return result, nil
	}

	var rows []string

	if err := dbx.SelectSQL(ctx, s.pool, &rows, "confirmed premiere ids", loadConfirmedPremiereIDsSQL, videoIDs); err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}

	for _, row := range rows {
		videoID := strings.TrimSpace(row)
		if videoID == "" {
			continue
		}

		result[videoID] = struct{}{}
	}

	return result, nil
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
		return fmt.Errorf("load official collabo talent names: %w", err)
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

	if err := dbx.SelectSQL(ctx, s.pool, &rows, "official collabo talent names", loadOfficialCollaboTalentNamesSQL, videoIDs); err != nil {
		return nil, fmt.Errorf("select: %w", err)
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
