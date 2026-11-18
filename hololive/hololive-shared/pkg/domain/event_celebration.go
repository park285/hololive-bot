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

func (p *CelebrationDispatchPayload) Identity() string {
	return fmt.Sprintf("%s:member-%d:%s", p.Kind, p.MemberID, p.Date)
}

type CalendarEntry struct {
	Kind    CelebrationKind `json:"kind"`
	Member  *Member         `json:"member"`
	Day     int             `json:"day"`
	Ordinal int             `json:"ordinal"`
}
