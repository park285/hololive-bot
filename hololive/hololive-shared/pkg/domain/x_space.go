package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var (
	xSpaceIDPattern      = regexp.MustCompile(`^[a-zA-Z0-9]{1,64}$`)
	xUserIDPattern       = regexp.MustCompile(`^\d{1,20}$`)
	xSpaceChannelPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
)

type XSpaceDispatchPayload struct {
	SpaceID   string    `json:"space_id"`
	CreatorID string    `json:"creator_id"`
	ChannelID string    `json:"channel_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
}

func (p *XSpaceDispatchPayload) Validate() error {
	if p == nil {
		return errors.New("x space payload is nil")
	}

	if !xSpaceIDPattern.MatchString(p.SpaceID) || !xUserIDPattern.MatchString(p.CreatorID) || !xSpaceChannelPattern.MatchString(p.ChannelID) {
		return errors.New("x space identity is invalid")
	}

	if len(p.Title) > 2048 {
		return errors.New("x space display text is invalid")
	}

	if strings.ContainsFunc(p.Title, unicode.IsControl) || p.StartedAt.IsZero() {
		return errors.New("x space text or start time is invalid")
	}

	return nil
}

func (p *XSpaceDispatchPayload) URL() string {
	return "https://x.com/i/spaces/" + p.SpaceID
}
