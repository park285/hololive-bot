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
	"fmt"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/assert"
)

func TestGroupAlarmDispatchEnvelopes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		envelopes []domain.AlarmQueueEnvelope
		want      []alarmDispatchGroupSummary
	}{
		{
			name: "empty input",
			want: []alarmDispatchGroupSummary{},
		},
		{
			name: "single envelope",
			envelopes: []domain.AlarmQueueEnvelope{
				alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeLive, 5),
			},
			want: []alarmDispatchGroupSummary{
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
			},
		},
		{
			name: "multiple envelopes grouped by key",
			envelopes: []domain.AlarmQueueEnvelope{
				alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeLive, 10),
				alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeLive, 10),
				alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeLive, 5),
				alarmDispatchGroupTestEnvelope("room-2", domain.AlarmTypeLive, 10),
			},
			want: []alarmDispatchGroupSummary{
				{roomID: "room-1", minutesUntil: 10, envelopeCount: 2, notificationCount: 2},
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
				{roomID: "room-2", minutesUntil: 10, envelopeCount: 1, notificationCount: 1},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			groups := groupAlarmDispatchEnvelopes(tc.envelopes)

			assert.Equal(t, tc.want, summarizeAlarmDispatchGroups(groups))
		})
	}
}

func TestGroupAlarmDispatchEnvelopesForKaring(t *testing.T) {
	t.Parallel()

	firstStart := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	secondStart := firstStart.Add(time.Minute)

	testCases := []struct {
		name          string
		karingEnabled bool
		envelopes     []domain.AlarmQueueEnvelope
		want          []alarmDispatchGroupSummary
	}{
		{
			name:          "karing disabled falls back to regular grouping",
			karingEnabled: false,
			envelopes: []domain.AlarmQueueEnvelope{
				alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeLive, 5, firstStart),
				alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeLive, 5, secondStart),
			},
			want: []alarmDispatchGroupSummary{
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
			},
		},
		{
			name:          "karing enabled groups by alarm type",
			karingEnabled: true,
			envelopes: []domain.AlarmQueueEnvelope{
				alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeLive, 5, firstStart),
				alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeCommunity, 5, secondStart),
				alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeLive, 5, secondStart),
			},
			want: []alarmDispatchGroupSummary{
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 2, notificationCount: 2},
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
			},
		},
		{
			name:          "karing enabled splits live catchup and prelive",
			karingEnabled: true,
			envelopes: []domain.AlarmQueueEnvelope{
				alarmDispatchGroupTestStartedEnvelope(domain.AlarmTypeLive, 5, firstStart),
				alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeLive, 5, secondStart),
			},
			want: []alarmDispatchGroupSummary{
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
				{roomID: "room-1", minutesUntil: 5, envelopeCount: 1, notificationCount: 1},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			groups := groupAlarmDispatchEnvelopesForKaring(tc.envelopes, tc.karingEnabled)

			assert.Equal(t, tc.want, summarizeAlarmDispatchGroups(groups))
		})
	}
}

func TestAlarmDispatchGroupKey(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 14, 10, 0, 30, 0, time.FixedZone("KST", 9*60*60))
	minuteBucket := start.UTC().Unix() / 60
	youtubeOutbox := alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeShorts, 0)
	youtubeOutbox.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	youtubeOutbox.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:      domain.OutboxKindNewShort,
		AlarmType: domain.AlarmTypeShorts,
		ChannelID: "UCtest",
		Items: []domain.YouTubeOutboxItem{
			{ContentID: "short-b", Payload: `{"video_id":"short-b"}`},
			{ContentID: "short-a", Payload: `{"video_id":"short-a"}`},
		},
	}

	testCases := []struct {
		name     string
		envelope domain.AlarmQueueEnvelope
		want     string
	}{
		{
			name:     "youtube outbox source kind",
			envelope: youtubeOutbox,
			want:     "room-1|source|youtube_outbox|UCtest|NEW_SHORT|" + youtubeOutbox.YouTubeOutbox.Identity(),
		},
		{
			name:     "scheduled stream with StartScheduled",
			envelope: alarmDispatchGroupTestScheduledEnvelope(domain.AlarmTypeLive, 10, start),
			want:     fmt.Sprintf("room-1|scheduled|%d", minuteBucket),
		},
		{
			name:     "fallback to minutes",
			envelope: alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeLive, 15),
			want:     "room-1|minutes|15",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, alarmDispatchGroupKey(&tc.envelope))
		})
	}
}

func TestAlarmDispatchKaringGroupKey(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
	youtubeOutbox := alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeCommunity, 0)
	youtubeOutbox.SourceKind = domain.AlarmDispatchSourceKindYouTubeOutbox
	youtubeOutbox.YouTubeOutbox = &domain.YouTubeOutboxDispatchPayload{
		Kind:      domain.OutboxKindCommunityPost,
		AlarmType: domain.AlarmTypeCommunity,
		ChannelID: "UCtest",
		Items: []domain.YouTubeOutboxItem{{
			ContentID: "post-1",
			Payload:   `{"post_id":"post-1","content_text":"hello"}`,
		}},
	}

	testCases := []struct {
		name     string
		envelope domain.AlarmQueueEnvelope
		want     string
	}{
		{
			name:     "youtube outbox source delegates to regular key",
			envelope: youtubeOutbox,
			want:     alarmDispatchGroupKey(&youtubeOutbox),
		},
		{
			name:     "non-outbox uses karing format",
			envelope: alarmDispatchGroupTestEnvelope("room-1", domain.AlarmTypeCommunity, 3),
			want:     "room-1|karing|COMMUNITY|prelive|minutes|3",
		},
		{
			name:     "live catchup includes starting phase",
			envelope: alarmDispatchGroupTestStartedEnvelope(domain.AlarmTypeLive, 5, start),
			want:     "room-1|karing|LIVE|starting|minutes|5",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, alarmDispatchKaringGroupKey(&tc.envelope))
		})
	}
}

func TestMinAlarmDispatchMinutes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		current int
		next    int
		want    int
	}{
		{name: "negative next is ignored", current: 10, next: -1, want: 10},
		{name: "negative current is replaced", current: -1, next: 5, want: 5},
		{name: "current smaller is kept", current: 3, next: 10, want: 3},
		{name: "next smaller is used", current: 10, next: 3, want: 3},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, minAlarmDispatchMinutes(tc.current, tc.next))
		})
	}
}

type alarmDispatchGroupSummary struct {
	roomID            string
	minutesUntil      int
	envelopeCount     int
	notificationCount int
}

func summarizeAlarmDispatchGroups(groups []alarmDispatchGroup) []alarmDispatchGroupSummary {
	summaries := make([]alarmDispatchGroupSummary, 0, len(groups))
	for _, group := range groups {
		summaries = append(summaries, alarmDispatchGroupSummary{
			roomID:            group.roomID,
			minutesUntil:      group.minutesUntil,
			envelopeCount:     len(group.envelopes),
			notificationCount: len(group.notifications),
		})
	}
	return summaries
}

func alarmDispatchGroupTestEnvelope(roomID string, alarmType domain.AlarmType, minutesUntil int) domain.AlarmQueueEnvelope {
	envelope := alarmDispatchRunnerTestEnvelope(roomID, nil)
	envelope.Notification.AlarmType = alarmType
	envelope.Notification.MinutesUntil = minutesUntil
	return envelope
}

func alarmDispatchGroupTestScheduledEnvelope(
	alarmType domain.AlarmType,
	minutesUntil int,
	start time.Time,
) domain.AlarmQueueEnvelope {
	envelope := alarmDispatchGroupTestEnvelope("room-1", alarmType, minutesUntil)
	envelope.Notification.Stream.StartScheduled = &start
	return envelope
}

func alarmDispatchGroupTestStartedEnvelope(
	alarmType domain.AlarmType,
	minutesUntil int,
	start time.Time,
) domain.AlarmQueueEnvelope {
	envelope := alarmDispatchGroupTestEnvelope("room-1", alarmType, minutesUntil)
	envelope.Notification.Stream.StartActual = &start
	return envelope
}
