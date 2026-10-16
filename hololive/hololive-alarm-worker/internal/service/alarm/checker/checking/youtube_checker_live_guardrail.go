package checking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *YouTubeChecker) observePersistedLiveGuardrails(
	ctx context.Context,
	sessions []PersistedYouTubeLiveSession,
	streamsByChannel map[string][]*domain.Stream,
	subscriberMap map[string][]string,
	now time.Time,
) recentLiveDispatchEvidence {
	evidence := newRecentLiveDispatchEvidence()

	if c.persistedLiveSource == nil {
		return evidence
	}

	sinceByStreamID := currentLiveDispatchEvidenceWindows(streamsByChannel, liveFirstSeenAtByStreamID(sessions), now)

	if len(sinceByStreamID) == 0 {
		return evidence
	}

	evidence, err := c.recentLiveDispatchEvidence(ctx, sinceByStreamID)
	if err != nil {
		observeYouTubeLiveGuardrail("dispatch_check_error")
		c.logger.Warn("YouTube live guardrail dispatch check failed",
			slog.Any("error", err),
		)

		return evidence
	}

	metas := persistedLiveGuardrailMetas(sessions, subscriberMap, now)
	for _, meta := range metas {
		// 병합 후 상태를 LIVE 알림 생성과 같은 기준으로 반영한다. 확정 Premiere도 시작 알림 대상이다.
		stream := findYouTubeStreamByID(streamsByChannel[meta.channelID], meta.streamID)
		if stream != nil && !isLiveCatchupCandidate(stream) {
			continue
		}

		rooms, err := c.guardrailSubscriberRooms(ctx, &meta, streamsByChannel)
		if err != nil {
			observeYouTubeLiveGuardrail("dispatch_check_error")
			c.logger.Warn("YouTube live guardrail subscriber check failed", slog.String("stream_id", meta.streamID), slog.Any("error", err))

			continue
		}

		if len(rooms) == 0 {
			continue
		}

		meta.rooms = rooms
		c.observePersistedLiveGuardrailMeta(&meta, evidence, sinceByStreamID[meta.streamID])
	}

	return evidence
}

// 같은 stream이 여러 번 보이면 가장 이른 하한을 써서 근거를 좁히지 않는다.
func currentLiveDispatchEvidenceWindows(
	streamsByChannel map[string][]*domain.Stream,
	liveFirstSeenAtByStreamID map[string]time.Time,
	now time.Time,
) map[string]time.Time {
	sinceByStreamID := make(map[string]time.Time)

	for _, streams := range streamsByChannel {
		for _, stream := range streams {
			if !isLiveCatchupCandidate(stream) || stream.ID == "" {
				continue
			}

			since := liveDispatchEvidenceSince(stream, liveFirstSeenAtByStreamID[stream.ID], now)
			if existing, ok := sinceByStreamID[stream.ID]; !ok || since.Before(existing) {
				sinceByStreamID[stream.ID] = since
			}
		}
	}

	return sinceByStreamID
}

// 같은 방송의 저장 세션이 여럿이면 가장 이른 첫 관측 시각을 쓴다.
func liveFirstSeenAtByStreamID(sessions []PersistedYouTubeLiveSession) map[string]time.Time {
	firstSeen := make(map[string]time.Time)

	for _, session := range sessions {
		if session.Stream == nil || session.Stream.ID == "" || session.LiveFirstSeenAt.IsZero() {
			continue
		}

		if existing, ok := firstSeen[session.Stream.ID]; !ok || session.LiveFirstSeenAt.Before(existing) {
			firstSeen[session.Stream.ID] = session.LiveFirstSeenAt.UTC()
		}
	}

	return firstSeen
}

func (c *YouTubeChecker) observePersistedLiveGuardrailMeta(
	meta *persistedLiveGuardrailMeta,
	evidence recentLiveDispatchEvidence,
	since time.Time,
) {
	sentRooms := evidence.sentRoomsByStreamID[meta.streamID]
	missingRooms := missingLiveDeliveryRooms(meta.rooms, sentRooms, evidence.terminalFailedRoomsByStreamID[meta.streamID])

	if len(missingRooms) == 0 {
		// catchup의 DLQ·격리·취소는 ledger가 실패로 기록하고 DLQ 지표가 따로 세는 결과다. 매분 경고를 반복하지 않고 지표로만 구분한다.
		if len(missingLiveDeliveryRooms(meta.rooms, sentRooms)) > 0 {
			observeYouTubeLiveGuardrail("terminal_delivery")
		} else {
			observeYouTubeLiveGuardrail("has_recent_delivery")
		}

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
	meta *persistedLiveGuardrailMeta,
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
	meta *persistedLiveGuardrailMeta,
	sentRooms map[string]struct{},
	missingRooms []string,
	since time.Time,
) {
	observeYouTubeLiveGuardrail("partial_recent_delivery")
	c.logger.Warn("alarm.youtube.live_guardrail.partial_delivery",
		slog.String("stream_id", meta.streamID),
		slog.String("channel_id", meta.channelID),
		slog.Time("status_observed_at", meta.observedAt.UTC()),
		slog.Time("dispatch_since", since.UTC()),
		slog.Int("subscriber_rooms", len(meta.rooms)),
		slog.Int("sent_rooms", len(sentRooms)),
		slog.Int("missing_rooms", len(missingRooms)),
	)
}

func (c *YouTubeChecker) logMissingLiveDeliveryGuardrail(meta *persistedLiveGuardrailMeta, since time.Time) {
	observeYouTubeLiveGuardrail("missing_recent_delivery")
	c.logger.Warn("alarm.youtube.live_guardrail.missing_delivery",
		slog.String("stream_id", meta.streamID),
		slog.String("channel_id", meta.channelID),
		slog.Time("status_observed_at", meta.observedAt.UTC()),
		slog.Time("dispatch_since", since.UTC()),
		slog.Int("subscriber_rooms", len(meta.rooms)),
	)
}

func (c *YouTubeChecker) logMissingLiveDispatchGuardrail(meta *persistedLiveGuardrailMeta, since time.Time) {
	observeYouTubeLiveGuardrail("missing_recent_dispatch")
	c.logger.Warn("alarm.youtube.live_guardrail.missing_dispatch",
		slog.String("stream_id", meta.streamID),
		slog.String("channel_id", meta.channelID),
		slog.Time("status_observed_at", meta.observedAt.UTC()),
		slog.Time("dispatch_since", since.UTC()),
		slog.Int("subscriber_rooms", len(meta.rooms)),
	)
}

type recentLiveDispatchEvidence struct {
	pgDispatchedStreamIDs         map[string]struct{}
	valkeyNotifiedStreamIDs       map[string]struct{}
	sentRoomsByStreamID           map[string]map[string]struct{}
	terminalFailedRoomsByStreamID map[string]map[string]struct{}
	deliveryCheckFailed           bool
}

// settledRoomsByStreamID는 catchup을 다시 만들지 않을 방이다. 발송을 마친 방과 ledger가 재발행을 거절하는 실패 종료 방을 합친다.
func (e recentLiveDispatchEvidence) settledRoomsByStreamID() map[string]map[string]struct{} {
	settled := make(map[string]map[string]struct{}, len(e.sentRoomsByStreamID)+len(e.terminalFailedRoomsByStreamID))

	for _, roomsByStreamID := range []map[string]map[string]struct{}{e.sentRoomsByStreamID, e.terminalFailedRoomsByStreamID} {
		for streamID, rooms := range roomsByStreamID {
			for roomID := range rooms {
				addStreamRoom(settled, streamID, roomID)
			}
		}
	}

	return settled
}

func (c *YouTubeChecker) recentLiveDispatchEvidence(
	ctx context.Context,
	sinceByStreamID map[string]time.Time,
) (recentLiveDispatchEvidence, error) {
	evidence := newRecentLiveDispatchEvidence()
	errs := make([]error, 0, 2)
	deliveryErrs := make([]error, 0, 1)

	c.collectPgLiveDispatchEvidence(ctx, sinceByStreamID, &evidence, &errs, &deliveryErrs)
	c.collectValkeyLiveDispatchEvidence(ctx, slices.Sorted(maps.Keys(sinceByStreamID)), &evidence, &errs)

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

func newRecentLiveDispatchEvidence() recentLiveDispatchEvidence {
	return recentLiveDispatchEvidence{
		pgDispatchedStreamIDs:         make(map[string]struct{}),
		valkeyNotifiedStreamIDs:       make(map[string]struct{}),
		sentRoomsByStreamID:           make(map[string]map[string]struct{}),
		terminalFailedRoomsByStreamID: make(map[string]map[string]struct{}),
	}
}

func (c *YouTubeChecker) collectPgLiveDispatchEvidence(
	ctx context.Context,
	sinceByStreamID map[string]time.Time,
	evidence *recentLiveDispatchEvidence,
	errs *[]error,
	deliveryErrs *[]error,
) {
	if c.persistedLiveSource == nil {
		return
	}

	dispatched, err := c.persistedLiveSource.RecentlyDispatchedStreamIDs(ctx, sinceByStreamID)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("pg dispatch evidence: %w", err))
	} else {
		mergeStringSet(evidence.pgDispatchedStreamIDs, dispatched)
	}

	settled, err := c.persistedLiveSource.RecentlySettledLiveStreamRooms(ctx, sinceByStreamID)
	if err != nil {
		*deliveryErrs = append(*deliveryErrs, fmt.Errorf("pg settled delivery evidence: %w", err))
	} else {
		evidence.sentRoomsByStreamID = settled.Sent
		evidence.terminalFailedRoomsByStreamID = settled.TerminalFailed
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
		len(e.sentRoomsByStreamID) > 0 ||
		len(e.terminalFailedRoomsByStreamID) > 0
}

func mergeStringSet(dst, src map[string]struct{}) {
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
	observedAt time.Time
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
	if !isLiveCatchupCandidate(stream) || stream.ID == "" {
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
		observedAt: session.ObservedAt,
		rooms:      UniqueStrings(rooms),
	}, true
}

func persistedLiveGuardrailObservedAt(session PersistedYouTubeLiveSession) time.Time {
	if !session.LiveFirstSeenAt.IsZero() {
		return session.LiveFirstSeenAt.UTC()
	}

	if !session.ObservedAt.IsZero() {
		return session.ObservedAt.UTC()
	}

	return time.Time{}
}

// missingLiveDeliveryRooms는 주어진 어느 방 집합에도 없는 구독 방을 돌려준다.
func missingLiveDeliveryRooms(rooms []string, accountedRooms ...map[string]struct{}) []string {
	uniqueRooms := UniqueStrings(rooms)
	if len(uniqueRooms) == 0 {
		return nil
	}

	missing := make([]string, 0, len(uniqueRooms))
	for _, room := range uniqueRooms {
		if slices.ContainsFunc(accountedRooms, func(set map[string]struct{}) bool {
			_, ok := set[room]

			return ok
		}) {
			continue
		}

		missing = append(missing, room)
	}

	return missing
}
