package checker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	persistedLiveDispatchRecentWindow = 24 * time.Hour
	persistedLiveGuardrailGraceWindow = 2 * time.Minute
)

type PersistedYouTubeLiveSession struct {
	Stream     *domain.Stream
	LastSeenAt time.Time
}

type YouTubeLiveSessionSource interface {
	LoadRecentSessions(ctx context.Context, channelIDs []string, now time.Time) ([]PersistedYouTubeLiveSession, error)
	LoadRecentLiveChannelIDs(ctx context.Context, channelIDs []string, now time.Time) ([]string, error)
	RecentlyDispatchedStreamIDs(ctx context.Context, streamIDs []string, since time.Time) (map[string]struct{}, error)
	RecentlySentLiveStreamRooms(ctx context.Context, streamIDs []string, since time.Time) (map[string]map[string]struct{}, error)
}

func (c *YouTubeChecker) loadPersistedLiveSessions(
	ctx context.Context,
	dueChannels []string,
	now time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	if c.persistedLiveSource == nil {
		return nil, nil
	}

	sessions, err := c.persistedLiveSource.LoadRecentSessions(ctx, dueChannels, now)
	if err != nil {
		observeYouTubePersistedLiveSessions("load_error", "all", 1)
		return nil, fmt.Errorf("load persisted youtube live sessions: %w", err)
	}

	if len(sessions) == 0 {
		observeYouTubePersistedLiveSessions("empty", "all", 1)
		return nil, nil
	}

	for _, session := range sessions {
		status := "unknown"
		if session.Stream != nil {
			status = session.Stream.Status.String()
		}
		observeYouTubePersistedLiveSessions("loaded", status, 1)
	}
	return sessions, nil
}

func (c *YouTubeChecker) logPersistedLiveSourceError(err error) {
	if err == nil {
		return
	}
	c.logger.Warn("YouTube persisted live session fallback failed",
		slog.Any("error", err),
	)
}

func mergePersistedLiveSessionStreams(
	streamsByChannel map[string][]*domain.Stream,
	sessions []PersistedYouTubeLiveSession,
) map[string]time.Time {
	liveObservedAtByStreamID := make(map[string]time.Time)
	for _, session := range sessions {
		stream, channelID, ok := persistedLiveSessionStreamIdentity(session)
		if !ok {
			continue
		}
		if stream.IsLive() {
			recordLiveObservedAt(liveObservedAtByStreamID, stream.ID, session.LastSeenAt)
		}
		mergePersistedLiveSessionStream(streamsByChannel, channelID, stream)
	}
	return liveObservedAtByStreamID
}

func persistedLiveSessionStreamIdentity(session PersistedYouTubeLiveSession) (*domain.Stream, string, bool) {
	if session.Stream == nil {
		return nil, "", false
	}
	channelID := youtubeStreamChannelID(session.Stream)
	if channelID == "" || session.Stream.ID == "" {
		return nil, "", false
	}
	return session.Stream, channelID, true
}

func recordLiveObservedAt(observed map[string]time.Time, streamID string, lastSeenAt time.Time) {
	if lastSeenAt.IsZero() {
		return
	}
	observed[streamID] = lastSeenAt.UTC()
}

func mergePersistedLiveSessionStream(
	streamsByChannel map[string][]*domain.Stream,
	channelID string,
	stream *domain.Stream,
) {
	streams := streamsByChannel[channelID]
	if existing := findYouTubeStreamByID(streams, stream.ID); existing != nil {
		fillMissingYouTubeStreamFields(existing, stream)
		return
	}
	streamsByChannel[channelID] = append(streams, cloneStream(stream))
}

func findYouTubeStreamByID(streams []*domain.Stream, streamID string) *domain.Stream {
	for _, stream := range streams {
		if stream == nil || stream.ID != streamID {
			continue
		}
		return stream
	}
	return nil
}

func fillMissingYouTubeStreamFields(dst, src *domain.Stream) {
	if dst == nil || src == nil {
		return
	}
	promotePersistedLiveStatus(dst, src)
	fillMissingYouTubeStreamScalarFields(dst, src)
	fillMissingYouTubeStreamTimeFields(dst, src)
	fillMissingYouTubeStreamPointerFields(dst, src)
}

func promotePersistedLiveStatus(dst, src *domain.Stream) {
	if !src.IsLive() || dst.IsLive() {
		return
	}
	dst.Status = src.Status
}

func fillMissingYouTubeStreamScalarFields(dst, src *domain.Stream) {
	if dst.Title == "" {
		dst.Title = src.Title
	}
	if dst.ChannelID == "" {
		dst.ChannelID = youtubeStreamChannelID(src)
	}
	if dst.ChannelName == "" {
		dst.ChannelName = src.ChannelName
	}
	if dst.Status == "" {
		dst.Status = src.Status
	}
}

func fillMissingYouTubeStreamTimeFields(dst, src *domain.Stream) {
	dst.StartScheduled = firstTimePtr(dst.StartScheduled, src.StartScheduled)
	dst.StartActual = firstTimePtr(dst.StartActual, src.StartActual)
}

func fillMissingYouTubeStreamPointerFields(dst, src *domain.Stream) {
	dst.Thumbnail = firstStringPtr(dst.Thumbnail, src.Thumbnail)
	dst.Link = firstStringPtr(dst.Link, src.Link)
	if dst.Channel == nil && src.Channel != nil {
		channel := *src.Channel
		dst.Channel = &channel
	}
}

func firstTimePtr(primary, fallback *time.Time) *time.Time {
	if primary != nil || fallback == nil {
		return primary
	}
	value := fallback.UTC()
	return &value
}

func firstStringPtr(primary, fallback *string) *string {
	if primary != nil || fallback == nil {
		return primary
	}
	value := *fallback
	return &value
}

func liveObservedAt(stream *domain.Stream, observedMaps ...map[string]time.Time) *time.Time {
	if stream == nil || stream.ID == "" || len(observedMaps) == 0 {
		return nil
	}
	observedAt, ok := observedMaps[0][stream.ID]
	if !ok || observedAt.IsZero() {
		return nil
	}
	observedAt = observedAt.UTC()
	return &observedAt
}
