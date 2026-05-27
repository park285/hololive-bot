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
	"slices"
	"time"
)

type MajorEventStatus string

const (
	MajorEventStatusActive   MajorEventStatus = "active" // 활성 (예정/진행 중)
	MajorEventStatusEnded    MajorEventStatus = "ended"  // 종료됨
	MajorEventStatusCanceled MajorEventStatus = "canceled"
)

type MajorEventType string

const (
	MajorEventTypeEvent MajorEventType = "event" // 대형 행사 (콘서트, Fes, Expo 등)
	MajorEventTypeNews  MajorEventType = "news"  // 공식 뉴스
)

type MajorEventLinkStatus string

const (
	MajorEventLinkStatusUnchecked MajorEventLinkStatus = "unchecked"
	MajorEventLinkStatusOK        MajorEventLinkStatus = "ok"
	MajorEventLinkStatusFailed    MajorEventLinkStatus = "failed"
	MajorEventLinkStatusBlocked   MajorEventLinkStatus = "blocked"
)

type MajorEvent struct {
	ID         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ExternalID string `json:"external_id" gorm:"uniqueIndex;size:500;not null"` // RSS guid/link (중복 방지)

	Type MajorEventType `json:"type" gorm:"size:20;default:'event';index"` // event 또는 news

	Title       string   `json:"title" gorm:"size:500;not null"`
	Link        string   `json:"link" gorm:"size:1000;not null"`
	Description string   `json:"description" gorm:"type:text"`
	Members     []string `json:"members" gorm:"type:text[];serializer:json"` // PostgreSQL text[] 또는 JSON

	PubDate        *time.Time  `json:"pub_date" gorm:"type:timestamptz"`        // RSS 발행일
	EventStartDate *time.Time  `json:"event_start_date" gorm:"type:date;index"` // 행사 시작일
	EventEndDate   *time.Time  `json:"event_end_date" gorm:"type:date"`         // 행사 종료일 (멀티데이)
	EventDates     []time.Time `json:"-" gorm:"-"`                              // 파싱 시 임시 저장 (DB 미저장)

	Status        MajorEventStatus     `json:"status" gorm:"size:50;default:'active';index"`
	LinkStatus    MajorEventLinkStatus `json:"link_status" gorm:"size:20;default:'unchecked';index"`
	LinkCheckedAt *time.Time           `json:"link_checked_at" gorm:"type:timestamptz"`
	NotifiedAt    *time.Time           `json:"notified_at" gorm:"type:timestamptz"` // 알림 발송 시각
	NotifiedWeek  string               `json:"notified_week" gorm:"size:10;index"`  // 알림 발송 주차 (YYYY-WW)
	NotifiedMonth string               `json:"notified_month" gorm:"size:10;index"` // 월간 알림 발송 월 (YYYY-MM)

	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

func (MajorEvent) TableName() string {
	return "major_events"
}

func NewMajorEvent(title, link string, pubDate time.Time) *MajorEvent {
	return &MajorEvent{
		Title:      title,
		Link:       link,
		ExternalID: link, // 기본적으로 link를 external_id로 사용
		PubDate:    &pubDate,
		Type:       MajorEventTypeEvent,
		Status:     MajorEventStatusActive,
		LinkStatus: MajorEventLinkStatusUnchecked,
	}
}

func NewMajorNews(title, link string, pubDate time.Time) *MajorEvent {
	return &MajorEvent{
		Title:      title,
		Link:       link,
		ExternalID: link,
		PubDate:    &pubDate,
		Type:       MajorEventTypeNews,
		Status:     MajorEventStatusActive,
		LinkStatus: MajorEventLinkStatusUnchecked,
	}
}

func (e *MajorEvent) HasEventDates() bool {
	return e.EventStartDate != nil || len(e.EventDates) > 0
}

func (e *MajorEvent) GetEventStartDate() *time.Time {
	if e.EventStartDate != nil {
		return e.EventStartDate
	}
	if len(e.EventDates) > 0 {
		return &e.EventDates[0]
	}
	return nil
}

func (e *MajorEvent) SetEventDatesFromParsed() {
	if len(e.EventDates) == 0 {
		return
	}

	slices.SortFunc(e.EventDates, func(a, b time.Time) int {
		return a.Compare(b)
	})

	startDate := e.EventDates[0]
	endDate := e.EventDates[len(e.EventDates)-1]
	e.EventStartDate = &startDate
	e.EventEndDate = &endDate
}

func (e *MajorEvent) IsNotified(weekKey string) bool {
	return e.NotifiedWeek == weekKey
}

func (e *MajorEvent) IsMonthlyNotified(monthKey string) bool {
	return e.NotifiedMonth == monthKey
}

func (e *MajorEvent) MarkAsNotified(weekKey string, at time.Time) {
	e.NotifiedWeek = weekKey
	e.NotifiedAt = &at
}

type EventRoomSubscription struct {
	ID        int       `json:"id" gorm:"primaryKey;autoIncrement"`
	RoomID    string    `json:"room_id" gorm:"uniqueIndex;not null"` // 카카오톡 방 ID (고유)
	RoomName  string    `json:"room_name"`                           // 방 이름 (표시용)
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

func (EventRoomSubscription) TableName() string {
	return "major_event_subscriptions"
}

func NewEventRoomSubscription(roomID, roomName string) *EventRoomSubscription {
	return &EventRoomSubscription{
		RoomID:    roomID,
		RoomName:  roomName,
		CreatedAt: time.Now(),
	}
}
