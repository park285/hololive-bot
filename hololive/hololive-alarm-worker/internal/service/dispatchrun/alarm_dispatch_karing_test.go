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

package dispatchrun

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const karingPremiereLabel = "최초공개"

func TestAlarmDispatchClientRequestID(t *testing.T) {
	t.Parallel()

	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.DispatchOutboxID = 42
	envelope.ClaimKeys = []string{"claim-a", "claim-b"}

	group := alarmDispatchGroup{
		roomID:    testAlarmRoomID,
		envelopes: []domain.AlarmQueueEnvelope{envelope},
	}
	changed := alarmDispatchGroup{
		roomID:    group.roomID,
		envelopes: append([]domain.AlarmQueueEnvelope(nil), group.envelopes...),
	}

	changed.envelopes[0].DispatchOutboxID = 43

	first := alarmDispatchClientRequestID(group, 0, 1)
	second := alarmDispatchClientRequestID(group, 0, 1)
	differentRange := alarmDispatchClientRequestID(group, 1, 2)
	differentEnvelope := alarmDispatchClientRequestID(changed, 0, 1)

	assert.Equal(t, first, second)
	assert.NotEqual(t, first, differentRange)
	assert.NotEqual(t, first, differentEnvelope)
	assert.Equal(t, "hololive-alarm:", alarmDispatchClientRequestIDNamespace)
	assert.True(t, strings.HasPrefix(first, alarmDispatchClientRequestIDNamespace))
}

func TestApplyAlarmDispatchKaringReceiver(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		roomID             string
		req                *iris.KaringContentListRequest
		wantReceiverRoomID int64
		wantReceiverName   string
	}{
		{
			name:               "numeric room id sets ReceiverRoomID",
			roomID:             " 464252100463241 ",
			req:                &iris.KaringContentListRequest{},
			wantReceiverRoomID: 464252100463241,
		},
		{
			name:             "non-numeric room id sets ReceiverName",
			roomID:           " room-1 ",
			req:              &iris.KaringContentListRequest{},
			wantReceiverName: testAlarmRoomID,
		},
		{
			name:   "nil request is no-op",
			roomID: testAlarmRoomID,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.NotPanics(t, func() {
				applyAlarmDispatchKaringReceiver(tc.req, tc.roomID)
			})

			if tc.req == nil {
				return
			}

			assert.Equal(t, tc.wantReceiverRoomID, tc.req.ReceiverRoomID)
			assert.Equal(t, tc.wantReceiverName, tc.req.ReceiverName)
		})
	}
}

func TestAlarmDispatchKaringTemplateID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		itemCount int
		want      int64
	}{
		{name: "one item", itemCount: 1, want: 133266},
		{name: "two items", itemCount: 2, want: 133223},
		{name: "three items", itemCount: 3, want: 133222},
		{name: "four items", itemCount: 4, want: 133267},
		{name: "unknown item count", itemCount: 5, want: 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, alarmDispatchKaringTemplateID(tc.itemCount))
		})
	}
}

func TestBuildAlarmDispatchKaringContentItems(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	thumbnail := "https://i.ytimg.com/vi/stream-a/maxresdefault.jpg"
	first := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	first.Channel.Name = "Member A"
	first.Stream.ID = "stream-a"
	first.Stream.Title = "Stream A"
	first.Stream.ChannelName = "Channel A"
	first.Stream.StartScheduled = &start
	first.Stream.Thumbnail = &thumbnail

	second := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	second.Channel.Name = "Member B"
	second.Stream.ID = "stream-b"
	second.Stream.Title = "Stream B"
	second.Stream.ChannelName = "Channel B"
	second.Stream.StartScheduled = new(start.Add(time.Hour))

	entries, err := buildAlarmDispatchKaringItems(t.Context(), nil, alarmDispatchGroup{
		notifications: []domain.AlarmNotification{first, second},
	})

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, iris.KaringContentItem{
		Title:        "Stream A",
		URL:          "https://youtube.com/watch?v=stream-a",
		MemberName:   "Member A",
		ChannelName:  "Channel A",
		Status:       "",
		StartAt:      "05/16 21:00",
		ThumbnailURL: thumbnail,
		Platform:     "youtube",
	}, entries[0].item)
	assert.Equal(t, "Stream B", entries[1].item.Title)
	assert.Equal(t, "Member B", entries[1].item.MemberName)
	assert.Equal(t, "Channel B", entries[1].item.ChannelName)
	assert.Equal(t, "05/16 22:00", entries[1].item.StartAt)
}

func TestBuildAlarmDispatchKaringContentListRequestsLiveCatchupUsesLiveLabels(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.May, 16, 12, 0, 0, 0, time.UTC)
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.Notification.MinutesUntil = 5
	envelope.Notification.Stream.StartActual = &start

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, alarmDispatchGroup{
		roomID:        testAlarmRoomID,
		minutesUntil:  5,
		envelopes:     []domain.AlarmQueueEnvelope{envelope},
		notifications: []domain.AlarmNotification{envelope.Notification},
	})

	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "라이브 시작", requests[0].ExtraArgs["alarm_title"])
	assert.Equal(t, "지금 시작", requests[0].ExtraArgs["time_left"])
}

func TestBuildAlarmDispatchKaringContentListRequestsPremiereUsesCountdown(t *testing.T) {
	t.Parallel()

	scheduled := time.Now().UTC().Add(30 * time.Minute)
	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:       domain.OutboxKindNewVideo,
		AlarmType:  domain.AlarmTypeLive,
		ChannelID:  testAlarmChannelID,
		MemberName: "아크로라",
		Items: []domain.YouTubeOutboxItem{{
			OutboxID:  1,
			ContentID: "premiere",
			Payload:   `{"video_id":"premiere","title":"최초공개 영상","scheduled_start_at":"` + scheduled.Format(time.RFC3339Nano) + `","is_premiere":true}`,
		}},
	}

	requests, err := buildAlarmDispatchKaringContentListRequests(t.Context(), nil, alarmDispatchGroup{
		roomID:    testAlarmRoomID,
		envelopes: []domain.AlarmQueueEnvelope{envelope},
	})

	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.Equal(t, "30분 후 공개 예정", requests[0].ExtraArgs["alarm_title"])
	assert.Equal(t, "30분 후 공개", requests[0].ExtraArgs["time_left"])
	require.Len(t, requests[0].Items, 1)
	assert.Equal(t, iris.KaringStreamStatus(karingPremiereLabel), requests[0].Items[0].Status)
}

func TestAlarmDispatchOutboxPremiereMinutesRequiresSingleFuturePremiere(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	scheduled := now.Add(30*time.Minute + time.Second)
	payload := &domain.YouTubeOutboxDispatchPayload{
		Kind: domain.OutboxKindNewVideo,
		Items: []domain.YouTubeOutboxItem{{
			Payload: `{"scheduled_start_at":"` + scheduled.Format(time.RFC3339) + `","is_premiere":true}`,
		}},
	}

	minutes, ok := alarmDispatchOutboxPremiereMinutes(payload, now)
	assert.True(t, ok)
	assert.Equal(t, 31, minutes)

	payload.Items = append(payload.Items, domain.YouTubeOutboxItem{Payload: payload.Items[0].Payload})
	_, ok = alarmDispatchOutboxPremiereMinutes(payload, now)
	assert.False(t, ok)
}

func TestBuildAlarmDispatchOutboxKaringExtraArgsPremiereBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		scheduledAt *time.Time
		wantTitle   string
		wantTime    string
	}{
		"future partial minute": {
			scheduledAt: new(now.Add(30*time.Minute + time.Second)),
			wantTitle:   "31분 후 공개 예정",
			wantTime:    "31분 후 공개",
		},
		"exact start": {
			scheduledAt: new(now),
			wantTitle:   karingPremiereLabel,
			wantTime:    karingPremiereLabel,
		},
		"past start": {
			scheduledAt: new(now.Add(-time.Second)),
			wantTitle:   karingPremiereLabel,
			wantTime:    karingPremiereLabel,
		},
		"missing start": {
			wantTitle: karingPremiereLabel,
			wantTime:  karingPremiereLabel,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			payload := `{"video_id":"premiere","is_premiere":true}`

			if test.scheduledAt != nil {
				payload = `{"video_id":"premiere","is_premiere":true,"scheduled_start_at":"` + test.scheduledAt.Format(time.RFC3339Nano) + `"}`
			}

			envelope := domain.AlarmQueueEnvelope{YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
				Kind:  domain.OutboxKindNewVideo,
				Items: []domain.YouTubeOutboxItem{{Payload: payload}},
			}}

			args := buildAlarmDispatchOutboxKaringExtraArgsAt(t.Context(), nil, &envelope, 1, now)
			assert.Equal(t, test.wantTitle, args["alarm_title"])
			assert.Equal(t, test.wantTime, args["time_left"])
		})
	}
}

func TestBuildAlarmDispatchKaringExtraArgsPremiereLabels(t *testing.T) {
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
		INSERT INTO message_strings(namespace, key, value) VALUES
		('karing', 'alarm_title_live_premiere', '설정 선행공개 시작'),
		('karing', 'alarm_title_prelive_premiere', '설정 선행공개 %d분 전 알림')
		ON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value
	`)
	require.NoError(t, err)

	store := messagestrings.NewStore(pool, slog.Default())
	require.NoError(t, store.Load(t.Context()))

	premiere := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	premiere.Stream.IsPremiere = true

	regular := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	for _, tc := range []struct {
		name          string
		minutesUntil  int
		notifications []domain.AlarmNotification
		wantTitle     string
		wantTimeLeft  string
	}{
		{
			name:          "all premiere starting",
			notifications: []domain.AlarmNotification{premiere},
			wantTitle:     "설정 선행공개 시작",
			wantTimeLeft:  "지금 시작",
		},
		{
			name:         "all premiere prelive",
			minutesUntil: 5,
			notifications: func() []domain.AlarmNotification {
				n := premiere

				n.MinutesUntil = 5

				return []domain.AlarmNotification{n}
			}(),
			wantTitle:    "설정 선행공개 5분 전 알림",
			wantTimeLeft: "5분 후 시작",
		},
		{
			name:         "mixed prelive keeps broadcast",
			minutesUntil: 5,
			notifications: func() []domain.AlarmNotification {
				p := premiere

				p.MinutesUntil = 5

				r := regular

				r.MinutesUntil = 5

				return []domain.AlarmNotification{p, r}
			}(),
			wantTitle:    "방송 5분 전 알림",
			wantTimeLeft: "5분 후 시작",
		},
		{
			name:          "mixed starting keeps live",
			notifications: []domain.AlarmNotification{premiere, regular},
			wantTitle:     "라이브 시작",
			wantTimeLeft:  "지금 시작",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := buildAlarmDispatchKaringExtraArgs(t.Context(), store, alarmDispatchGroup{
				minutesUntil:  tc.minutesUntil,
				notifications: tc.notifications,
			}, len(tc.notifications))

			assert.Equal(t, tc.wantTitle, args["alarm_title"])
			assert.Equal(t, tc.wantTimeLeft, args["time_left"])
		})
	}
}

func TestBuildAlarmDispatchKaringExtraArgsPremiereFallbacks(t *testing.T) {
	pool := dbtest.NewPool(t)
	_, err := pool.Exec(t.Context(), `
		DELETE FROM message_strings
		WHERE namespace = 'karing'
		  AND key IN ('alarm_title_live_premiere', 'alarm_title_prelive_premiere')
	`)
	require.NoError(t, err)

	store := messagestrings.NewStore(pool, slog.Default())
	require.NoError(t, store.Load(t.Context()))

	premiere := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil).Notification

	premiere.Stream.IsPremiere = true

	prelive := premiere

	prelive.MinutesUntil = 5

	startingArgs := buildAlarmDispatchKaringExtraArgs(t.Context(), store, alarmDispatchGroup{
		notifications: []domain.AlarmNotification{premiere},
	}, 1)
	preliveArgs := buildAlarmDispatchKaringExtraArgs(t.Context(), store, alarmDispatchGroup{
		minutesUntil:  5,
		notifications: []domain.AlarmNotification{prelive},
	}, 1)

	assert.Equal(t, "선행공개 시작", startingArgs["alarm_title"])
	assert.Equal(t, "지금 시작", startingArgs["time_left"])
	assert.Equal(t, "선행공개 5분 전 알림", preliveArgs["alarm_title"])
	assert.Equal(t, "5분 후 시작", preliveArgs["time_left"])
}

func TestAlarmDispatchEnvelopeClientRequestIDParts(t *testing.T) {
	t.Parallel()

	envelope := alarmDispatchRunnerTestEnvelope(testAlarmRoomID, nil)

	envelope.DispatchOutboxID = 42
	envelope.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	envelope.Notification.AlarmType = domain.AlarmTypeCommunity
	envelope.Notification.MinutesUntil = 30
	envelope.ClaimKeys = []string{"claim-a", "claim-b"}

	parts := alarmDispatchEnvelopeClientRequestIDParts(&envelope)

	assert.Equal(t, []string{
		"42",
		"youtube_outbox",
		"COMMUNITY",
		"30",
		"claim-a",
		"claim-b",
	}, parts)
}
