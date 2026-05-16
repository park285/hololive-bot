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

package model

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
	ID            int64                `gorm:"primaryKey;autoIncrement"`
	Kind          DeliveryOutboxKind   `gorm:"column:kind;type:varchar(30);not null"`
	PeriodKey     string               `gorm:"column:period_key;type:varchar(20);not null"`
	RoomID        string               `gorm:"column:room_id;type:varchar(100);not null"`
	ContentID     string               `gorm:"column:content_id;type:varchar(200);not null"`
	Payload       string               `gorm:"column:payload;type:jsonb;not null;default:'{}'"`
	Status        DeliveryOutboxStatus `gorm:"column:status;type:varchar(20);not null;default:'PENDING'"`
	AttemptCount  int                  `gorm:"column:attempt_count;not null;default:0"`
	NextAttemptAt time.Time            `gorm:"column:next_attempt_at;not null;default:now()"`
	CreatedAt     time.Time            `gorm:"column:created_at;not null;default:now()"`
	LockedAt      sql.NullTime         `gorm:"column:locked_at"`
	SentAt        sql.NullTime         `gorm:"column:sent_at"`
	Error         sql.NullString       `gorm:"column:error"`
}

func (NotificationDeliveryOutbox) TableName() string {
	return "notification_delivery_outbox"
}
