package domain

import (
	"sort"
	"time"
)

// MajorEventStatus: 대형 행사 상태
type MajorEventStatus string

const (
	MajorEventStatusActive   MajorEventStatus = "active" // 활성 (예정/진행 중)
	MajorEventStatusEnded    MajorEventStatus = "ended"  // 종료됨
	MajorEventStatusCanceled MajorEventStatus = "canceled"
)

// MajorEventType: 행사/뉴스 유형
type MajorEventType string

const (
	MajorEventTypeEvent MajorEventType = "event" // 대형 행사 (콘서트, Fes, Expo 등)
	MajorEventTypeNews  MajorEventType = "news"  // 공식 뉴스
)

// MajorEventLinkStatus: 링크 검증 상태
type MajorEventLinkStatus string

const (
	MajorEventLinkStatusUnchecked MajorEventLinkStatus = "unchecked"
	MajorEventLinkStatusOK        MajorEventLinkStatus = "ok"
	MajorEventLinkStatusFailed    MajorEventLinkStatus = "failed"
	MajorEventLinkStatusBlocked   MajorEventLinkStatus = "blocked"
)

// MajorEvent: 홀로라이브 대형 행사/뉴스 정보 (콘서트, Fes, Expo, 공식 뉴스 등)
// RSS Feed에서 파싱되어 DB에 저장됨
type MajorEvent struct {
	// Primary
	ID         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ExternalID string `json:"external_id" gorm:"uniqueIndex;size:500;not null"` // RSS guid/link (중복 방지)

	// Type
	Type MajorEventType `json:"type" gorm:"size:20;default:'event';index"` // event 또는 news

	// Content
	Title       string   `json:"title" gorm:"size:500;not null"`
	Link        string   `json:"link" gorm:"size:1000;not null"`
	Description string   `json:"description" gorm:"type:text"`
	Members     []string `json:"members" gorm:"type:text[];serializer:json"` // PostgreSQL text[] 또는 JSON

	// Dates
	PubDate        *time.Time  `json:"pub_date" gorm:"type:timestamptz"`        // RSS 발행일
	EventStartDate *time.Time  `json:"event_start_date" gorm:"type:date;index"` // 행사 시작일
	EventEndDate   *time.Time  `json:"event_end_date" gorm:"type:date"`         // 행사 종료일 (멀티데이)
	EventDates     []time.Time `json:"-" gorm:"-"`                              // 파싱 시 임시 저장 (DB 미저장)

	// State
	Status        MajorEventStatus     `json:"status" gorm:"size:50;default:'active';index"`
	LinkStatus    MajorEventLinkStatus `json:"link_status" gorm:"size:20;default:'unchecked';index"`
	LinkCheckedAt *time.Time           `json:"link_checked_at" gorm:"type:timestamptz"`
	NotifiedAt    *time.Time           `json:"notified_at" gorm:"type:timestamptz"` // 알림 발송 시각
	NotifiedWeek  string               `json:"notified_week" gorm:"size:10;index"`  // 알림 발송 주차 (YYYY-WW)
	NotifiedMonth string               `json:"notified_month" gorm:"size:10;index"` // 월간 알림 발송 월 (YYYY-MM)

	// Audit
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// TableName: GORM 테이블 이름 지정
func (MajorEvent) TableName() string {
	return "major_events"
}

// NewMajorEvent: 새로운 대형 행사 객체를 생성합니다.
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

// NewMajorNews: 새로운 뉴스 객체를 생성합니다.
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

// HasEventDates: 행사 날짜가 설정되어 있는지 확인합니다.
func (e *MajorEvent) HasEventDates() bool {
	return e.EventStartDate != nil || len(e.EventDates) > 0
}

// GetEventStartDate: 행사 시작일을 반환합니다. DB 필드 우선, 없으면 EventDates[0] 사용.
func (e *MajorEvent) GetEventStartDate() *time.Time {
	if e.EventStartDate != nil {
		return e.EventStartDate
	}
	if len(e.EventDates) > 0 {
		return &e.EventDates[0]
	}
	return nil
}

// SetEventDatesFromParsed: 파싱된 EventDates를 EventStartDate/EventEndDate로 변환합니다.
func (e *MajorEvent) SetEventDatesFromParsed() {
	if len(e.EventDates) == 0 {
		return
	}

	sort.Slice(e.EventDates, func(i, j int) bool {
		return e.EventDates[i].Before(e.EventDates[j])
	})

	startDate := e.EventDates[0]
	endDate := e.EventDates[len(e.EventDates)-1]
	e.EventStartDate = &startDate
	e.EventEndDate = &endDate
}

// IsNotified: 특정 주차에 알림이 발송되었는지 확인합니다.
func (e *MajorEvent) IsNotified(weekKey string) bool {
	return e.NotifiedWeek == weekKey
}

// IsMonthlyNotified: 특정 월에 월간 알림이 발송되었는지 확인합니다.
func (e *MajorEvent) IsMonthlyNotified(monthKey string) bool {
	return e.NotifiedMonth == monthKey
}

// MarkAsNotified: 알림 발송 완료로 표시합니다.
func (e *MajorEvent) MarkAsNotified(weekKey string, at time.Time) {
	e.NotifiedWeek = weekKey
	e.NotifiedAt = &at
}

// EventRoomSubscription: 대형 행사 알림 방 구독 정보 (DB 저장)
type EventRoomSubscription struct {
	ID        int       `json:"id" gorm:"primaryKey;autoIncrement"`
	RoomID    string    `json:"room_id" gorm:"uniqueIndex;not null"` // 카카오톡 방 ID (고유)
	RoomName  string    `json:"room_name"`                           // 방 이름 (표시용)
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
}

// TableName: GORM 테이블 이름 지정
func (EventRoomSubscription) TableName() string {
	return "major_event_subscriptions"
}

// NewEventRoomSubscription: 새로운 행사 알림 구독 객체를 생성합니다.
func NewEventRoomSubscription(roomID, roomName string) *EventRoomSubscription {
	return &EventRoomSubscription{
		RoomID:    roomID,
		RoomName:  roomName,
		CreatedAt: time.Now(),
	}
}
