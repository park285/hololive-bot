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

package workerapp

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlarmDispatchClientRequestID(t *testing.T) {
	t.Parallel()

	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
	envelope.DispatchOutboxID = 42
	envelope.ClaimKeys = []string{"claim-a", "claim-b"}
	group := alarmDispatchGroup{
		roomID:    "room-1",
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
	assert.Contains(t, first, "hololive-alarm:")
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
			wantReceiverName: "room-1",
		},
		{
			name:   "nil request is no-op",
			roomID: "room-1",
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

	start := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	thumbnail := "https://i.ytimg.com/vi/stream-a/maxresdefault.jpg"
	first := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	first.Channel.Name = "Member A"
	first.Stream.ID = "stream-a"
	first.Stream.Title = "Stream A"
	first.Stream.ChannelName = "Channel A"
	first.Stream.StartScheduled = &start
	first.Stream.Thumbnail = &thumbnail

	second := alarmDispatchRunnerTestEnvelope("room-1", nil).Notification
	second.Channel.Name = "Member B"
	second.Stream.ID = "stream-b"
	second.Stream.Title = "Stream B"
	second.Stream.ChannelName = "Channel B"
	second.Stream.StartScheduled = new(start.Add(time.Hour))

	items, err := buildAlarmDispatchKaringContentItems(t.Context(), nil, alarmDispatchGroup{
		notifications: []domain.AlarmNotification{first, second},
	})

	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, iris.KaringContentItem{
		Title:        "Stream A",
		URL:          "https://youtube.com/watch?v=stream-a",
		MemberName:   "Member A",
		ChannelName:  "Channel A",
		Status:       "",
		StartAt:      "05/16 21:00",
		ThumbnailURL: thumbnail,
		Platform:     "youtube",
	}, items[0])
	assert.Equal(t, "Stream B", items[1].Title)
	assert.Equal(t, "Member B", items[1].MemberName)
	assert.Equal(t, "Channel B", items[1].ChannelName)
	assert.Equal(t, "05/16 22:00", items[1].StartAt)
}

func TestAlarmDispatchEnvelopeClientRequestIDParts(t *testing.T) {
	t.Parallel()

	envelope := alarmDispatchRunnerTestEnvelope("room-1", nil)
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
