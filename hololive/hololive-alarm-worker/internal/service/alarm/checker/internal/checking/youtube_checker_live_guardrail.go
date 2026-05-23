package checking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (c *YouTubeChecker) observePersistedLiveGuardrails(
	ctx context.Context,
	sessions []PersistedYouTubeLiveSession,
	subscriberMap map[string][]string,
	now time.Time,
) {
	if c.persistedLiveSource == nil {
		return
	}

	since := now.Add(-persistedLiveDispatchRecentWindow)
	metas := persistedLiveGuardrailMetas(sessions, subscriberMap, now)
	if len(metas) == 0 {
		return
	}

	streamIDs := make([]string, 0, len(metas))
	for _, meta := range metas {
		streamIDs = append(streamIDs, meta.streamID)
	}

	evidence, err := c.recentLiveDispatchEvidence(ctx, streamIDs, since)
	if err != nil {
		observeYouTubeLiveGuardrail("dispatch_check_error")
		c.logger.Warn("YouTube live guardrail dispatch check failed",
			slog.Any("error", err),
		)
		return
	}

	for _, meta := range metas {
		c.observePersistedLiveGuardrailMeta(meta, evidence, since)
	}
}

func (c *YouTubeChecker) observePersistedLiveGuardrailMeta(
	meta persistedLiveGuardrailMeta,
	evidence recentLiveDispatchEvidence,
	since time.Time,
) {
	sentRooms := evidence.sentRoomsByStreamID[meta.streamID]
	missingRooms := missingLiveDeliveryRooms(meta.rooms, sentRooms)
	if len(missingRooms) == 0 {
		observeYouTubeLiveGuardrail("has_recent_delivery")
		return
	}
	if len(sentRooms) > 0 {
		c.logPartialLiveDeliveryGuardrail(meta, sentRooms, missingRooms, since)
		return
	}
	if evidence.deliveryCheckFailed {
		c.observeStreamLevelLiveGuardrailMeta(meta, evidence, since)
		return
	}
	if _, ok := evidence.pgDispatchedStreamIDs[meta.streamID]; ok {
		c.logMissingLiveDeliveryGuardrail(meta, since)
		return
	}
	if _, ok := evidence.valkeyNotifiedStreamIDs[meta.streamID]; ok {
		observeYouTubeLiveGuardrail("has_recent_notified")
		return
	}
	c.logMissingLiveDispatchGuardrail(meta, since)
}

func (c *YouTubeChecker) observeStreamLevelLiveGuardrailMeta(
	meta persistedLiveGuardrailMeta,
	evidence recentLiveDispatchEvidence,
	since time.Time,
) {
	if _, ok := evidence.pgDispatchedStreamIDs[meta.streamID]; ok {
		observeYouTubeLiveGuardrail("has_recent_dispatch")
		return
	}
	if _, ok := evidence.valkeyNotifiedStreamIDs[meta.streamID]; ok {
		observeYouTubeLiveGuardrail("has_recent_notified")
		return
	}
	c.logMissingLiveDispatchGuardrail(meta, since)
}

func (c *YouTubeChecker) logPartialLiveDeliveryGuardrail(
	meta persistedLiveGuardrailMeta,
	sentRooms map[string]struct{},
	missingRooms []string,
	since time.Time,
) {
	observeYouTubeLiveGuardrail("partial_recent_delivery")
	c.logger.Warn("alarm.youtube.live_guardrail.partial_delivery",
		slog.String("stream_id", meta.streamID),
		slog.String("channel_id", meta.channelID),
		slog.Time("last_seen_at", meta.lastSeenAt.UTC()),
		slog.Time("dispatch_since", since.UTC()),
		slog.Int("subscriber_rooms", len(meta.rooms)),
		slog.Int("sent_rooms", len(sentRooms)),
		slog.Int("missing_rooms", len(missingRooms)),
	)
}

func (c *YouTubeChecker) logMissingLiveDeliveryGuardrail(meta persistedLiveGuardrailMeta, since time.Time) {
	observeYouTubeLiveGuardrail("missing_recent_delivery")
	c.logger.Warn("alarm.youtube.live_guardrail.missing_delivery",
		slog.String("stream_id", meta.streamID),
		slog.String("channel_id", meta.channelID),
		slog.Time("last_seen_at", meta.lastSeenAt.UTC()),
		slog.Time("dispatch_since", since.UTC()),
		slog.Int("subscriber_rooms", len(meta.rooms)),
	)
}

func (c *YouTubeChecker) logMissingLiveDispatchGuardrail(meta persistedLiveGuardrailMeta, since time.Time) {
	observeYouTubeLiveGuardrail("missing_recent_dispatch")
	c.logger.Warn("alarm.youtube.live_guardrail.missing_dispatch",
		slog.String("stream_id", meta.streamID),
		slog.String("channel_id", meta.channelID),
		slog.Time("last_seen_at", meta.lastSeenAt.UTC()),
		slog.Time("dispatch_since", since.UTC()),
		slog.Int("subscriber_rooms", len(meta.rooms)),
	)
}

type recentLiveDispatchEvidence struct {
	pgDispatchedStreamIDs   map[string]struct{}
	valkeyNotifiedStreamIDs map[string]struct{}
	sentRoomsByStreamID     map[string]map[string]struct{}
	deliveryCheckFailed     bool
}

func (c *YouTubeChecker) recentlyDispatchedOrNotifiedLiveStreamIDs(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
) (map[string]struct{}, error) {
	evidence, err := c.recentLiveDispatchEvidence(ctx, streamIDs, since)
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{}, len(evidence.pgDispatchedStreamIDs)+len(evidence.valkeyNotifiedStreamIDs))
	mergeStringSet(result, evidence.pgDispatchedStreamIDs)
	mergeStringSet(result, evidence.valkeyNotifiedStreamIDs)
	return result, nil
}

func (c *YouTubeChecker) recentLiveDispatchEvidence(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
) (recentLiveDispatchEvidence, error) {
	evidence := recentLiveDispatchEvidence{
		pgDispatchedStreamIDs:   make(map[string]struct{}),
		valkeyNotifiedStreamIDs: make(map[string]struct{}),
		sentRoomsByStreamID:     make(map[string]map[string]struct{}),
	}
	var errs []error
	var deliveryErrs []error

	c.collectPgLiveDispatchEvidence(ctx, streamIDs, since, &evidence, &errs, &deliveryErrs)
	c.collectValkeyLiveDispatchEvidence(ctx, streamIDs, &evidence, &errs)
	if err := errors.Join(deliveryErrs...); err != nil {
		evidence.deliveryCheckFailed = true
		observeYouTubeLiveGuardrail("delivery_check_error")
		c.logger.Warn("YouTube live guardrail delivery check failed",
			slog.Any("error", err),
		)
	}
	if evidence.hasAny() {
		return evidence, nil
	}
	return evidence, errors.Join(append(errs, deliveryErrs...)...)
}

func (c *YouTubeChecker) collectPgLiveDispatchEvidence(
	ctx context.Context,
	streamIDs []string,
	since time.Time,
	evidence *recentLiveDispatchEvidence,
	errs *[]error,
	deliveryErrs *[]error,
) {
	if c.persistedLiveSource == nil {
		return
	}

	dispatched, err := c.persistedLiveSource.RecentlyDispatchedStreamIDs(ctx, streamIDs, since)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("pg dispatch evidence: %w", err))
	} else {
		mergeStringSet(evidence.pgDispatchedStreamIDs, dispatched)
	}

	sentRooms, err := c.persistedLiveSource.RecentlySentLiveStreamRooms(ctx, streamIDs, since)
	if err != nil {
		*deliveryErrs = append(*deliveryErrs, fmt.Errorf("pg sent delivery evidence: %w", err))
	} else {
		evidence.sentRoomsByStreamID = sentRooms
	}
}

func (c *YouTubeChecker) collectValkeyLiveDispatchEvidence(
	ctx context.Context,
	streamIDs []string,
	evidence *recentLiveDispatchEvidence,
	errs *[]error,
) {
	if c.dedupService == nil {
		return
	}
	notified, err := c.dedupService.RecentlyNotifiedStreamIDs(ctx, streamIDs)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("valkey notified evidence: %w", err))
		return
	}
	mergeStringSet(evidence.valkeyNotifiedStreamIDs, notified)
}

func (e recentLiveDispatchEvidence) hasAny() bool {
	return len(e.pgDispatchedStreamIDs) > 0 ||
		len(e.valkeyNotifiedStreamIDs) > 0 ||
		len(e.sentRoomsByStreamID) > 0
}

func mergeStringSet(dst map[string]struct{}, src map[string]struct{}) {
	for key := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		dst[key] = struct{}{}
	}
}

type persistedLiveGuardrailMeta struct {
	streamID   string
	channelID  string
	lastSeenAt time.Time
	rooms      []string
}

func persistedLiveGuardrailMetas(
	sessions []PersistedYouTubeLiveSession,
	subscriberMap map[string][]string,
	now time.Time,
) []persistedLiveGuardrailMeta {
	metas := make([]persistedLiveGuardrailMeta, 0, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	for _, session := range sessions {
		meta, ok := persistedLiveGuardrailMetaFromSession(session, subscriberMap, seen, now)
		if !ok {
			continue
		}
		metas = append(metas, meta)
	}
	return metas
}

func persistedLiveGuardrailMetaFromSession(
	session PersistedYouTubeLiveSession,
	subscriberMap map[string][]string,
	seen map[string]struct{},
	now time.Time,
) (persistedLiveGuardrailMeta, bool) {
	stream := session.Stream
	if stream == nil || !stream.IsLive() || stream.ID == "" {
		return persistedLiveGuardrailMeta{}, false
	}
	observedAt := persistedLiveGuardrailObservedAt(session)
	if observedAt.IsZero() || now.Sub(observedAt) < persistedLiveGuardrailGraceWindow {
		observeYouTubeLiveGuardrail("pending_grace")
		return persistedLiveGuardrailMeta{}, false
	}
	if _, ok := seen[stream.ID]; ok {
		return persistedLiveGuardrailMeta{}, false
	}
	channelID := youtubeStreamChannelID(stream)
	rooms := subscriberMap[channelID]
	if channelID == "" || len(rooms) == 0 {
		return persistedLiveGuardrailMeta{}, false
	}

	seen[stream.ID] = struct{}{}
	return persistedLiveGuardrailMeta{
		streamID:   stream.ID,
		channelID:  channelID,
		lastSeenAt: session.LastSeenAt,
		rooms:      UniqueStrings(rooms),
	}, true
}

func persistedLiveGuardrailObservedAt(session PersistedYouTubeLiveSession) time.Time {
	if !session.LiveFirstSeenAt.IsZero() {
		return session.LiveFirstSeenAt.UTC()
	}
	if !session.LastSeenAt.IsZero() {
		return session.LastSeenAt.UTC()
	}
	return time.Time{}
}

func missingLiveDeliveryRooms(rooms []string, sentRooms map[string]struct{}) []string {
	uniqueRooms := UniqueStrings(rooms)
	if len(uniqueRooms) == 0 {
		return nil
	}
	missing := make([]string, 0, len(uniqueRooms))
	for _, room := range uniqueRooms {
		if _, ok := sentRooms[room]; ok {
			continue
		}
		missing = append(missing, room)
	}
	return missing
}
