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

package dispatch

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestGroupEnvelopes_GroupByRoomAndScheduledMinute(t *testing.T) {
	t.Parallel()

	startA := time.Date(2026, 3, 4, 10, 0, 10, 0, time.UTC)
	startB := time.Date(2026, 3, 4, 10, 0, 59, 0, time.UTC)

	envelopes := []domain.AlarmQueueEnvelope{
		newEnvelope("room-a", "stream-1", 5, &startA, "claim-1"),
		newEnvelope("room-a", "stream-2", 3, &startB, "claim-2"),
		newEnvelope("room-b", "stream-3", 5, &startA, "claim-3"),
	}

	groups := GroupEnvelopes(envelopes)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	if groups[0].RoomID != "room-a" {
		t.Fatalf("expected first group room-a, got %s", groups[0].RoomID)
	}
	if groups[0].MinutesUntil != 3 {
		t.Fatalf("expected merged minutes_until=3, got %d", groups[0].MinutesUntil)
	}
	if len(groups[0].Notifications) != 2 {
		t.Fatalf("expected room-a notifications=2, got %d", len(groups[0].Notifications))
	}
	if len(groups[0].ClaimKeys) != 2 {
		t.Fatalf("expected room-a claim keys=2, got %d", len(groups[0].ClaimKeys))
	}
}

func TestGroupEnvelopes_GroupByMinutesWhenScheduleMissing(t *testing.T) {
	t.Parallel()

	envelopes := []domain.AlarmQueueEnvelope{
		newEnvelope("room-a", "stream-1", 5, nil, "claim-1"),
		newEnvelope("room-a", "stream-2", 5, nil, "claim-2"),
		newEnvelope("room-a", "stream-3", 3, nil, "claim-3"),
	}

	groups := GroupEnvelopes(envelopes)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func newEnvelope(roomID, streamID string, minutesUntil int, startScheduled *time.Time, claimKey string) domain.AlarmQueueEnvelope {
	stream := &domain.Stream{
		ID:             streamID,
		Title:          "title-" + streamID,
		ChannelID:      "channel-id",
		ChannelName:    "channel-name",
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: startScheduled,
	}

	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:       roomID,
			Channel:      &domain.Channel{ID: "channel-id", Name: "channel-name"},
			Stream:       stream,
			MinutesUntil: minutesUntil,
		},
		ClaimKeys: []string{claimKey},
		Version:   1,
	}
}
