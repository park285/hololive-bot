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
	MemberName        string          `json:"member_name"`
	ChannelID         string          `json:"channel_id"`
	Photo             string          `json:"photo,omitempty"`
	Ordinal           int             `json:"ordinal,omitempty"`
	Years             int             `json:"years,omitempty"`
	Date              string          `json:"date"`
	VideoID           string          `json:"video_id,omitempty"`
	StreamTitle       string          `json:"stream_title,omitempty"`
	StreamURL         string          `json:"stream_url,omitempty"`
	ScheduledStartKST string          `json:"scheduled_start_kst,omitempty"`
}

func (p *CelebrationDispatchPayload) Identity() string {
	identity := fmt.Sprintf("%s:%s:%s", p.Kind, p.ChannelID, p.Date)
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
	Ordinal int             `json:"ordinal,omitempty"`
}
