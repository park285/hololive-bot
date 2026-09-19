package domain

import (
	"errors"
	"fmt"
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

// XSpaceDispatchPayload는 직접 개설한 스페이스의 최초 관측을 고정한 발송 자료다.
// ChannelID는 기존 LIVE 구독을 찾는 채널이며 SpaceID는 X의 식별자다.
// 제목 변경은 이미 만들어진 시작 이벤트의 내용이나 정체성을 바꾸지 않는다.
type XSpaceDispatchPayload struct {
	SpaceID    string    `json:"space_id"`
	CreatorID  string    `json:"creator_id"`
	ChannelID  string    `json:"channel_id"`
	MemberName string    `json:"member_name"`
	Title      string    `json:"title"`
	StartedAt  time.Time `json:"started_at"`
}

// Validate는 발송과 URL 구성에 필요한 식별자·표시 문자열·시각을 검사한다.
func (p *XSpaceDispatchPayload) Validate() error {
	if p == nil {
		return errors.New("x space payload is nil")
	}

	if !xSpaceIDPattern.MatchString(p.SpaceID) || !xUserIDPattern.MatchString(p.CreatorID) || !xSpaceChannelPattern.MatchString(p.ChannelID) {
		return errors.New("x space identity is invalid")
	}

	if strings.TrimSpace(p.MemberName) == "" || len(p.MemberName) > 256 || len(p.Title) > 2048 {
		return errors.New("x space display text is invalid")
	}

	if strings.ContainsFunc(p.MemberName+p.Title, unicode.IsControl) || p.StartedAt.IsZero() {
		return errors.New("x space text or start time is invalid")
	}

	return nil
}

// URL은 검증된 스페이스의 공개 참여 링크를 반환한다. 외부 제공 URL을 사용하지 않는다.
func (p *XSpaceDispatchPayload) URL() string {
	return "https://x.com/i/spaces/" + p.SpaceID
}

func (e *AlarmQueueEnvelope) validateXSpaceDispatch() error {
	if err := validateCanonicalNotification(&e.Notification); err != nil {
		return fmt.Errorf("x space notification: %w", err)
	}

	if e.Notification.AlarmType != AlarmTypeLive || e.Notification.Channel != nil || e.Notification.Stream != nil {
		return errors.New("x space dispatch requires LIVE subscription without a stream payload")
	}

	if e.YouTubeOutbox != nil || e.Celebration != nil || e.DeliveryDigest != nil {
		return errors.New("x space dispatch contains another source payload")
	}

	return e.XSpace.Validate()
}
