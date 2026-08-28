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
	MemberID          int             `json:"member_id,omitempty"`
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

func (p *CelebrationDispatchPayload) Identity() string {
	memberIdentity := p.ChannelID
	if p.MemberID > 0 {
		memberIdentity = fmt.Sprintf("member-%d", p.MemberID)
	}

	identity := fmt.Sprintf("%s:%s:%s", p.Kind, memberIdentity, p.Date)
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
