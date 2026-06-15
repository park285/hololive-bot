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
	ID         int    `json:"id" db:"id"`
	ExternalID string `json:"external_id" db:"external_id"` // RSS guid/link (중복 방지)

	Type MajorEventType `json:"type" db:"type"` // event 또는 news

	Title       string   `json:"title" db:"title"`
	Link        string   `json:"link" db:"link"`
	Description string   `json:"description" db:"description"`
	Members     []string `json:"members" db:"members"` // PostgreSQL text[] 또는 JSON

	PubDate        *time.Time  `json:"pub_date" db:"pub_date"`                 // RSS 발행일
	EventStartDate *time.Time  `json:"event_start_date" db:"event_start_date"` // 행사 시작일
	EventEndDate   *time.Time  `json:"event_end_date" db:"event_end_date"`     // 행사 종료일 (멀티데이)
	EventDates     []time.Time `json:"-" db:"-"`                               // 파싱 시 임시 저장 (DB 미저장)

	Status        MajorEventStatus     `json:"status" db:"status"`
	LinkStatus    MajorEventLinkStatus `json:"link_status" db:"link_status"`
	LinkCheckedAt *time.Time           `json:"link_checked_at" db:"link_checked_at"`
	NotifiedAt    *time.Time           `json:"notified_at" db:"notified_at"`       // 알림 발송 시각
	NotifiedWeek  string               `json:"notified_week" db:"notified_week"`   // 알림 발송 주차 (YYYY-WW)
	NotifiedMonth string               `json:"notified_month" db:"notified_month"` // 월간 알림 발송 월 (YYYY-MM)

	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
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

func (e *MajorEvent) MarkAsNotified(weekKey string, at time.Time) {
	e.NotifiedWeek = weekKey
	e.NotifiedAt = &at
}

type EventRoomSubscription struct {
	ID        int       `json:"id" db:"id"`
	RoomID    string    `json:"room_id" db:"room_id"` // 카카오톡 방 ID (고유)
	RoomName  string    `json:"room_name"`            // 방 이름 (표시용)
	CreatedAt time.Time `json:"created_at" db:"created_at"`
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
