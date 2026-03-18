package settlement

import "time"

// RoomConfig: 방별 정산 설정.
type RoomConfig struct {
	RoomID                     string
	BillingAnchorDay           int
	BillingTZ                  string
	TotalAmount                int
	PerPerson                  int
	RequireExplicitForMultiple bool
}

// Member: 정산 대상 멤버.
type Member struct {
	ID          int
	RoomID      string
	KakaoUserID string
	MemberName  string
}

// MemberSnapshot: 회차 생성 시점의 참여 멤버 스냅샷 입력용.
type MemberSnapshot struct {
	MemberID   int
	MemberName string
}

// Cycle: 18일 앵커 기반 정산 회차.
type Cycle struct {
	ID                  int
	RoomID              string
	CycleKey            string
	PeriodStartAt       time.Time
	PeriodEndAt         time.Time
	TotalAmount         int
	PerPerson           int
	BillingAnchorDay    int
	MemberCountSnapshot int
	CreatedAt           time.Time
}

// PaymentStatus: 회차별 멤버 납부 상태.
type PaymentStatus struct {
	MemberID           int
	MemberNameSnapshot string
	PaidAt             *time.Time
}

// CycleWindow: 특정 시각이 속한 회차 범위.
type CycleWindow struct {
	CycleKey string
	StartAt  time.Time
	EndAt    time.Time
}

// PaymentTarget: !정산완료 대상 후보.
type PaymentTarget struct {
	CycleID       int
	CycleKey      string
	PeriodStartAt time.Time
	PeriodEndAt   time.Time
	MemberID      int
	PaidAt        *time.Time
}

// PaymentEventRef: 외부 이벤트 dedup 조회 결과.
type PaymentEventRef struct {
	CycleID int
}

// MarkPaidInput: 납부 완료 처리 입력.
type MarkPaidInput struct {
	RoomID           string
	KakaoUserID      string
	ExplicitCycleKey string
	SourceType       string
	SourceEventID    string
	PaidAt           time.Time
}
