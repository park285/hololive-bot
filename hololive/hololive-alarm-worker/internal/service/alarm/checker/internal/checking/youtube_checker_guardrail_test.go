// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package checking

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type guardrailEvidenceSource struct {
	dispatched   map[string]struct{}
	dispatchErr  error
	sentRooms    map[string]map[string]struct{}
	sentRoomsErr error
}

func (s *guardrailEvidenceSource) LoadRecentSessions(
	context.Context,
	[]string,
	time.Time,
) ([]PersistedYouTubeLiveSession, error) {
	return nil, nil
}

func (s *guardrailEvidenceSource) LoadRecentLiveChannelIDs(
	context.Context,
	[]string,
	time.Time,
) ([]string, error) {
	return nil, nil
}

func (s *guardrailEvidenceSource) RecentlyDispatchedStreamIDs(
	context.Context,
	[]string,
	time.Time,
) (map[string]struct{}, error) {
	return s.dispatched, s.dispatchErr
}

func (s *guardrailEvidenceSource) RecentlySentLiveStreamRooms(
	context.Context,
	[]string,
	time.Time,
) (map[string]map[string]struct{}, error) {
	return s.sentRooms, s.sentRoomsErr
}

func TestPersistedLiveGuardrailMetaFromSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 24, 10, 0, 0, 0, time.UTC)
	graceBoundary := now.Add(-persistedLiveGuardrailGraceWindow)
	lastSeenAt := now.Add(-time.Second)

	tests := map[string]struct {
		session       PersistedYouTubeLiveSession
		subscriberMap map[string][]string
		seen          map[string]struct{}
		wantOK        bool
		wantStreamID  string
		wantChannelID string
		wantRooms     []string
	}{
		"accepts live session at grace boundary with channel fallback": {
			session: PersistedYouTubeLiveSession{
				Stream: &domain.Stream{
					ID:      "stream-live",
					Status:  domain.StreamStatusLive,
					Channel: &domain.Channel{ID: "channel-from-object"},
				},
				LastSeenAt:      lastSeenAt,
				LiveFirstSeenAt: graceBoundary,
			},
			subscriberMap: map[string][]string{
				"channel-from-object": {"room-1", "room-1", "", "room-2"},
			},
			wantOK:        true,
			wantStreamID:  "stream-live",
			wantChannelID: "channel-from-object",
			wantRooms:     []string{"room-1", "room-2"},
		},
		"rejects nil stream": {
			session:       PersistedYouTubeLiveSession{},
			subscriberMap: map[string][]string{"channel-1": {"room-1"}},
		},
		"rejects non live stream": {
			session: PersistedYouTubeLiveSession{
				Stream: &domain.Stream{ID: "stream-upcoming", ChannelID: "channel-1", Status: domain.StreamStatusUpcoming},
			},
			subscriberMap: map[string][]string{"channel-1": {"room-1"}},
		},
		"rejects empty stream id": {
			session: PersistedYouTubeLiveSession{
				Stream:     &domain.Stream{ChannelID: "channel-1", Status: domain.StreamStatusLive},
				LastSeenAt: graceBoundary,
			},
			subscriberMap: map[string][]string{"channel-1": {"room-1"}},
		},
		"rejects fresh observation inside grace window": {
			session: PersistedYouTubeLiveSession{
				Stream:     &domain.Stream{ID: "stream-fresh", ChannelID: "channel-1", Status: domain.StreamStatusLive},
				LastSeenAt: now.Add(-persistedLiveGuardrailGraceWindow + time.Second),
			},
			subscriberMap: map[string][]string{"channel-1": {"room-1"}},
		},
		"rejects zero observed time": {
			session: PersistedYouTubeLiveSession{
				Stream: &domain.Stream{ID: "stream-zero", ChannelID: "channel-1", Status: domain.StreamStatusLive},
			},
			subscriberMap: map[string][]string{"channel-1": {"room-1"}},
		},
		"rejects duplicate stream id": {
			session: PersistedYouTubeLiveSession{
				Stream:     &domain.Stream{ID: "stream-live", ChannelID: "channel-1", Status: domain.StreamStatusLive},
				LastSeenAt: graceBoundary,
			},
			subscriberMap: map[string][]string{"channel-1": {"room-1"}},
			seen:          map[string]struct{}{"stream-live": {}},
		},
		"rejects live stream without subscriber rooms": {
			session: PersistedYouTubeLiveSession{
				Stream:     &domain.Stream{ID: "stream-no-room", ChannelID: "channel-1", Status: domain.StreamStatusLive},
				LastSeenAt: graceBoundary,
			},
			subscriberMap: map[string][]string{"channel-1": nil},
		},
		"rejects live stream without channel id": {
			session: PersistedYouTubeLiveSession{
				Stream:     &domain.Stream{ID: "stream-no-channel", Status: domain.StreamStatusLive},
				LastSeenAt: graceBoundary,
			},
			subscriberMap: map[string][]string{"": {"room-1"}},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			seen := tc.seen
			if seen == nil {
				seen = make(map[string]struct{})
			}

			meta, ok := persistedLiveGuardrailMetaFromSession(tc.session, tc.subscriberMap, seen, now)
			assert.Equal(t, tc.wantOK, ok)
			if !tc.wantOK {
				return
			}

			assert.Equal(t, tc.wantStreamID, meta.streamID)
			assert.Equal(t, tc.wantChannelID, meta.channelID)
			assert.Equal(t, lastSeenAt, meta.lastSeenAt)
			assert.Equal(t, tc.wantRooms, meta.rooms)
			assert.Contains(t, seen, tc.wantStreamID)
		})
	}
}

func TestMissingLiveDeliveryRooms(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		rooms     []string
		sentRooms map[string]struct{}
		want      []string
	}{
		"nil rooms": {
			want: nil,
		},
		"all rooms sent": {
			rooms:     []string{"room-1", "room-2"},
			sentRooms: map[string]struct{}{"room-1": {}, "room-2": {}},
			want:      []string{},
		},
		"deduplicates rooms before detecting missing": {
			rooms:     []string{"room-1", "room-1", "", "room-2", "room-3"},
			sentRooms: map[string]struct{}{"room-2": {}},
			want:      []string{"room-1", "room-3"},
		},
		"nil sent map marks unique rooms missing": {
			rooms: []string{"room-1", "room-1", "room-2"},
			want:  []string{"room-1", "room-2"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, missingLiveDeliveryRooms(tc.rooms, tc.sentRooms))
		})
	}
}

func TestRecentLiveDispatchEvidenceErrorWrapping(t *testing.T) {
	t.Parallel()

	checker := &YouTubeChecker{
		persistedLiveSource: &guardrailEvidenceSource{
			dispatchErr:  errors.New("dispatch lookup failed"),
			sentRoomsErr: errors.New("sent room lookup failed"),
		},
		logger: newCheckerTestLogger(),
	}

	evidence, err := checker.recentLiveDispatchEvidence(
		t.Context(),
		[]string{"stream-1"},
		time.Date(2026, time.May, 24, 9, 0, 0, 0, time.UTC),
	)
	require.Error(t, err)
	assert.True(t, evidence.deliveryCheckFailed)
	assert.ErrorContains(t, err, "pg dispatch evidence")
	assert.ErrorContains(t, err, "dispatch lookup failed")
	assert.ErrorContains(t, err, "pg sent delivery evidence")
	assert.ErrorContains(t, err, "sent room lookup failed")
}

func TestRecentLiveDispatchEvidenceTrimsPersistedDispatchIDs(t *testing.T) {
	t.Parallel()

	checker := &YouTubeChecker{
		persistedLiveSource: &guardrailEvidenceSource{
			dispatched: map[string]struct{}{
				" " + "stream-pg" + " ": {},
				"":                      {},
			},
		},
		logger: newCheckerTestLogger(),
	}

	evidence, err := checker.recentLiveDispatchEvidence(
		t.Context(),
		[]string{"stream-pg"},
		time.Date(2026, time.May, 24, 9, 0, 0, 0, time.UTC),
	)
	require.NoError(t, err)
	assert.Contains(t, evidence.pgDispatchedStreamIDs, "stream-pg")
	assert.NotContains(t, evidence.pgDispatchedStreamIDs, "")
}

func TestObservePersistedLiveGuardrailMetaLogsOnlyRejectedDeliveryStates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 24, 10, 0, 0, 0, time.UTC)
	meta := persistedLiveGuardrailMeta{
		streamID:   "stream-1",
		channelID:  "channel-1",
		lastSeenAt: now.Add(-3 * time.Minute),
		rooms:      []string{"room-1", "room-2"},
	}

	tests := map[string]struct {
		evidence recentLiveDispatchEvidence
		wantLog  string
	}{
		"accepts complete room delivery": {
			evidence: recentLiveDispatchEvidence{
				sentRoomsByStreamID: map[string]map[string]struct{}{
					"stream-1": {"room-1": {}, "room-2": {}},
				},
			},
		},
		"logs partial delivery when some rooms are missing": {
			evidence: recentLiveDispatchEvidence{
				sentRoomsByStreamID: map[string]map[string]struct{}{
					"stream-1": {"room-1": {}},
				},
			},
			wantLog: "alarm.youtube.live_guardrail.partial_delivery",
		},
		"logs missing delivery when dispatch exists but no room delivery exists": {
			evidence: recentLiveDispatchEvidence{
				pgDispatchedStreamIDs: map[string]struct{}{"stream-1": {}},
			},
			wantLog: "alarm.youtube.live_guardrail.missing_delivery",
		},
		"accepts stream dispatch when delivery lookup failed": {
			evidence: recentLiveDispatchEvidence{
				pgDispatchedStreamIDs: map[string]struct{}{"stream-1": {}},
				deliveryCheckFailed:   true,
			},
		},
		"accepts valkey notified evidence": {
			evidence: recentLiveDispatchEvidence{
				valkeyNotifiedStreamIDs: map[string]struct{}{"stream-1": {}},
			},
		},
		"logs missing dispatch with no evidence": {
			evidence: recentLiveDispatchEvidence{},
			wantLog:  "alarm.youtube.live_guardrail.missing_dispatch",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			checker := &YouTubeChecker{
				logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
			}

			checker.observePersistedLiveGuardrailMeta(&meta, tc.evidence, now.Add(-persistedLiveDispatchRecentWindow))
			if tc.wantLog == "" {
				assert.Empty(t, buf.String())
				return
			}

			require.Contains(t, buf.String(), tc.wantLog)
			assert.Contains(t, buf.String(), "stream-1")
		})
	}
}
