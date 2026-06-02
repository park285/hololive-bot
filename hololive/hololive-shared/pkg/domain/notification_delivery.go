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

import (
	"database/sql"
	"time"
)

type DeliveryOutboxKind string

const (
	DeliveryKindMajorEventWeekly  DeliveryOutboxKind = "MAJOR_EVENT_WEEKLY"
	DeliveryKindMajorEventMonthly DeliveryOutboxKind = "MAJOR_EVENT_MONTHLY"
	DeliveryKindMemberNewsWeekly  DeliveryOutboxKind = "MEMBER_NEWS_WEEKLY"
	DeliveryKindMemberNewsMonthly DeliveryOutboxKind = "MEMBER_NEWS_MONTHLY"
)

type DeliveryOutboxStatus string

const (
	DeliveryStatusPending DeliveryOutboxStatus = "PENDING"
	DeliveryStatusSent    DeliveryOutboxStatus = "SENT"
	DeliveryStatusFailed  DeliveryOutboxStatus = "FAILED"
)

type NotificationDeliveryOutbox struct {
	ID            int64                `db:"id"`
	Kind          DeliveryOutboxKind   `db:"kind"`
	PeriodKey     string               `db:"period_key"`
	RoomID        string               `db:"room_id"`
	ContentID     string               `db:"content_id"`
	Payload       string               `db:"payload"`
	Status        DeliveryOutboxStatus `db:"status"`
	AttemptCount  int                  `db:"attempt_count"`
	NextAttemptAt time.Time            `db:"next_attempt_at"`
	CreatedAt     time.Time            `db:"created_at"`
	LockedAt      sql.NullTime         `db:"locked_at"`
	SentAt        sql.NullTime         `db:"sent_at"`
	Error         sql.NullString       `db:"error"`
}

func (NotificationDeliveryOutbox) TableName() string {
	return "notification_delivery_outbox"
}
