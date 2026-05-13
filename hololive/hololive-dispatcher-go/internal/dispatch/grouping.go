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
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type NotificationGroup struct {
	RoomID        string
	MinutesUntil  int
	Envelopes     []domain.AlarmQueueEnvelope
	Notifications []domain.AlarmNotification
	ClaimKeys     []string
}

func GroupEnvelopes(envelopes []domain.AlarmQueueEnvelope) []NotificationGroup {
	if len(envelopes) == 0 {
		return nil
	}

	groups := make([]NotificationGroup, 0, len(envelopes))
	index := make(map[string]int, len(envelopes))

	for i := range envelopes {
		envelope := envelopes[i]
		key := buildGroupKey(envelope)
		groupIndex, exists := index[key]
		if !exists {
			index[key] = len(groups)
			groups = append(groups, NotificationGroup{
				RoomID:        envelope.Notification.RoomID,
				MinutesUntil:  envelope.Notification.MinutesUntil,
				Envelopes:     []domain.AlarmQueueEnvelope{envelope},
				Notifications: []domain.AlarmNotification{envelope.Notification},
				ClaimKeys:     append([]string{}, envelope.ClaimKeys...),
			})
			continue
		}

		group := &groups[groupIndex]
		group.MinutesUntil = mergeMinutesUntil(group.MinutesUntil, envelope.Notification.MinutesUntil)
		group.Envelopes = append(group.Envelopes, envelope)
		group.Notifications = append(group.Notifications, envelope.Notification)
		group.ClaimKeys = append(group.ClaimKeys, envelope.ClaimKeys...)
	}

	return groups
}

func buildGroupKey(envelope domain.AlarmQueueEnvelope) string {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return fmt.Sprintf("%s|source|%s|%s|%s|%s",
			envelope.Notification.RoomID,
			envelope.SourceKind,
			envelope.YouTubeOutbox.ChannelID,
			envelope.YouTubeOutbox.Kind,
			envelope.YouTubeOutbox.Identity(),
		)
	}
	notification := envelope.Notification
	if notification.Stream != nil && notification.Stream.StartScheduled != nil {
		minuteBucket := notification.Stream.StartScheduled.UTC().Unix() / 60
		return fmt.Sprintf("%s|scheduled|%d", notification.RoomID, minuteBucket)
	}
	return fmt.Sprintf("%s|minutes|%d", notification.RoomID, notification.MinutesUntil)
}

func mergeMinutesUntil(current, next int) int {
	if next < 0 {
		return current
	}
	if current < 0 {
		return next
	}
	if next < current {
		return next
	}
	return current
}
