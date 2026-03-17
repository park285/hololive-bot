package settlement

import "time"

// Member: 정산 대상 멤버 (방 + kakao user_id <-> 정산 이름 매핑).
type Member struct {
	ID          int
	RoomID      string
	KakaoUserID string
	MemberName  string
}

// Cycle: 월별 정산 주기 (방별).
type Cycle struct {
	ID          int
	RoomID      string
	Year        int
	Month       int
	TotalAmount int
	PerPerson   int
	DueDay      int
}

// PaymentStatus: 멤버별 납부 상태.
type PaymentStatus struct {
	MemberName string
	PaidAt     *time.Time
}
