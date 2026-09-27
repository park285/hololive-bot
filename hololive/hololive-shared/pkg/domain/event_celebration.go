package domain

import (
	"fmt"
	"strings"
)

type CelebrationKind string

const (
	CelebrationKindBirthday       CelebrationKind = "birthday"
	CelebrationKindAnniversary    CelebrationKind = "anniversary"
	CelebrationKindBirthdayStream CelebrationKind = "birthday_stream"
)

type CelebrationDispatchPayload struct {
	Kind              CelebrationKind `json:"kind"`
	MemberID          int             `json:"member_id,omitzero"`
	MemberName        string          `json:"member_name"`
	ChannelID         string          `json:"channel_id"`
	Photo             string          `json:"photo,omitempty"`
	Ordinal           int             `json:"ordinal"`
	Years             int             `json:"years"`
	Date              string          `json:"date"`
	VideoID           string          `json:"video_id,omitempty"`
	StreamTitle       string          `json:"stream_title,omitempty"`
	StreamURL         string          `json:"stream_url,omitempty"`
	ScheduledStartKST string          `json:"scheduled_start_kst,omitempty"`
}

// Identity는 안정적인 MemberID만으로 멤버를 식별한다. 채널을 공유하는 멤버가 충돌하지 않도록 ChannelID는 쓰지 않으며,
// MemberID가 없는 payload는 ValidateCanonicalDispatch가 발행 전에 거절한다.
func (p *CelebrationDispatchPayload) Identity() string {
	identity := fmt.Sprintf("%s:member-%d:%s", p.Kind, p.MemberID, p.Date)
	if p.Kind == CelebrationKindBirthdayStream {
		if videoID := strings.TrimSpace(p.VideoID); videoID != "" {
			identity += ":" + videoID
		}
	}

	return identity
}

type CalendarEntry struct {
	Kind    CelebrationKind `json:"kind"`
	Member  *Member         `json:"member"`
	Day     int             `json:"day"`
	Ordinal int             `json:"ordinal"`
}
