package domain

import (
	"fmt"
)

type CelebrationKind string

const (
	CelebrationKindBirthday    CelebrationKind = "birthday"
	CelebrationKindAnniversary CelebrationKind = "anniversary"
)

func (k CelebrationKind) IsValid() bool {
	return k == CelebrationKindBirthday || k == CelebrationKindAnniversary
}

type CelebrationDispatchPayload struct {
	Kind       CelebrationKind `json:"kind"`
	MemberID   int             `json:"member_id,omitzero"`
	MemberName string          `json:"member_name"`
	ChannelID  string          `json:"channel_id"`
	Photo      string          `json:"photo,omitempty"`
	Ordinal    int             `json:"ordinal"`
	Years      int             `json:"years"`
	Date       string          `json:"date"`
}

// Identity는 안정적인 MemberID만으로 멤버를 식별한다. 채널을 공유하는 멤버가 충돌하지 않도록 ChannelID는 쓰지 않으며,
// MemberID가 없는 payload는 alarm-worker dispatch 원장의 ValidateCanonicalDispatch가 발행 전에 거절한다.
func (p *CelebrationDispatchPayload) Identity() string {
	return fmt.Sprintf("%s:member-%d:%s", p.Kind, p.MemberID, p.Date)
}

type CalendarEntry struct {
	Kind    CelebrationKind `json:"kind"`
	Member  *Member         `json:"member"`
	Day     int             `json:"day"`
	Ordinal int             `json:"ordinal"`
}
