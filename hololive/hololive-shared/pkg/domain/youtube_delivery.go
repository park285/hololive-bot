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

package domain

import "time"

type YouTubeNotificationDelivery struct {
	ID            int64        `db:"id" json:"id"`
	OutboxID      int64        `db:"outbox_id" json:"outbox_id"`
	RoomID        string       `db:"room_id" json:"room_id"`
	Status        OutboxStatus `db:"status" json:"status"`
	AttemptCount  int          `db:"attempt_count" json:"attempt_count"`
	NextAttemptAt time.Time    `db:"next_attempt_at" json:"next_attempt_at"`
	CreatedAt     time.Time    `db:"created_at" json:"created_at"`
	LockedAt      *time.Time   `json:"locked_at,omitempty"`
	SentAt        *time.Time   `json:"sent_at,omitempty"`
	Error         string       `db:"error" json:"error,omitempty"`
}

func (YouTubeNotificationDelivery) TableName() string {
	return "youtube_notification_delivery"
}
