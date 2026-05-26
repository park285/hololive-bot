package model

import "fmt"

type CelebrationKind string

const (
	CelebrationKindBirthday    CelebrationKind = "birthday"
	CelebrationKindAnniversary CelebrationKind = "anniversary"
)

type CelebrationDispatchPayload struct {
	Kind       CelebrationKind `json:"kind"`
	MemberName string          `json:"member_name"`
	ChannelID  string          `json:"channel_id"`
	Photo      string          `json:"photo,omitempty"`
	Years      int             `json:"years,omitempty"`
	Date       string          `json:"date"`
}

func (p *CelebrationDispatchPayload) Identity() string {
	return fmt.Sprintf("%s:%s:%s", p.Kind, p.ChannelID, p.Date)
}
