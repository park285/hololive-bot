package model

import (
	"fmt"
	"sort"
	"strings"
)

type AlarmDispatchSourceKind string

const (
	AlarmDispatchSourceKindYouTubeOutbox AlarmDispatchSourceKind = "youtube_outbox"
	AlarmDispatchSourceKindCelebration   AlarmDispatchSourceKind = "celebration"
)

type YouTubeOutboxDispatchPayload struct {
	OutboxIDs          []int64             `json:"outbox_ids"`
	Kind               OutboxKind          `json:"kind"`
	AlarmType          AlarmType           `json:"alarm_type"`
	ChannelID          string              `json:"channel_id"`
	MemberName         string              `json:"member_name,omitempty"`
	Items              []YouTubeOutboxItem `json:"items"`
	RenderTemplateKey  TemplateKey         `json:"render_template_key,omitempty"`
	PreRenderedMessage string              `json:"pre_rendered_message,omitempty"`
}

type YouTubeOutboxItem struct {
	OutboxID  int64  `json:"outbox_id"`
	ContentID string `json:"content_id"`
	Payload   string `json:"payload"`
}

func (p *YouTubeOutboxDispatchPayload) Validate() error {
	if p == nil {
		return fmt.Errorf("youtube outbox dispatch payload is nil")
	}
	return errorsJoin(
		validateYouTubeOutboxHeader(p),
		validateYouTubeOutboxItems(p.Items),
	)
}

func validateYouTubeOutboxHeader(p *YouTubeOutboxDispatchPayload) error {
	if p.Kind == "" {
		return fmt.Errorf("youtube outbox dispatch payload kind is empty")
	}
	if err := validateYouTubeOutboxAlarmHeader(p.AlarmType, p.ChannelID); err != nil {
		return err
	}
	if hasTemplateAndPreRenderedMessage(p) {
		return fmt.Errorf("youtube outbox dispatch payload cannot set both render template key and pre-rendered message")
	}
	return nil
}

func validateYouTubeOutboxAlarmHeader(alarmType AlarmType, channelID string) error {
	if alarmType == "" {
		return fmt.Errorf("youtube outbox dispatch payload alarm type is empty")
	}
	if !alarmType.IsValid() {
		return fmt.Errorf("youtube outbox dispatch payload alarm type %q is invalid", alarmType)
	}
	if strings.TrimSpace(channelID) == "" {
		return fmt.Errorf("youtube outbox dispatch payload channel id is empty")
	}
	return nil
}

func hasTemplateAndPreRenderedMessage(p *YouTubeOutboxDispatchPayload) bool {
	return strings.TrimSpace(string(p.RenderTemplateKey)) != "" && strings.TrimSpace(p.PreRenderedMessage) != ""
}

func validateYouTubeOutboxItems(items []YouTubeOutboxItem) error {
	if len(items) == 0 {
		return fmt.Errorf("youtube outbox dispatch payload items are empty")
	}
	for i := range items {
		if err := validateYouTubeOutboxItem(items[i], i); err != nil {
			return err
		}
	}
	return nil
}

func validateYouTubeOutboxItem(item YouTubeOutboxItem, index int) error {
	switch {
	case strings.TrimSpace(item.ContentID) == "":
		return fmt.Errorf("youtube outbox dispatch payload item %d content id is empty", index)
	case strings.TrimSpace(item.Payload) == "":
		return fmt.Errorf("youtube outbox dispatch payload item %d payload is empty", index)
	default:
		return nil
	}
}

func (p *YouTubeOutboxDispatchPayload) IdentityParts() []string {
	if p == nil {
		return nil
	}
	parts := make([]string, 0, len(p.Items))
	for i := range p.Items {
		contentID := strings.TrimSpace(p.Items[i].ContentID)
		if contentID != "" {
			parts = append(parts, contentID)
		}
	}
	sort.Strings(parts)
	return parts
}

func (p *YouTubeOutboxDispatchPayload) Identity() string {
	return strings.Join(p.IdentityParts(), ",")
}

func (e AlarmQueueEnvelope) HasYouTubeOutboxSource() bool {
	return e.SourceKind == AlarmDispatchSourceKindYouTubeOutbox
}

func (e AlarmQueueEnvelope) ValidateCanonicalDispatch() error {
	switch e.SourceKind {
	case AlarmDispatchSourceKindYouTubeOutbox:
		return e.validateYouTubeOutboxDispatch()
	case AlarmDispatchSourceKindCelebration:
		return e.validateCelebrationDispatch()
	case "":
		alarmType := e.Notification.AlarmType
		if alarmType == "" {
			alarmType = AlarmTypeLive
		}
		return validateLegacyRouteAlarmType(alarmType)
	default:
		return fmt.Errorf("canonical alarm dispatch: unsupported source kind %q", e.SourceKind)
	}
}

func (e AlarmQueueEnvelope) validateCelebrationDispatch() error {
	if e.Notification.RoomID == "" {
		return fmt.Errorf("canonical alarm dispatch: celebration room id is empty")
	}
	at := e.Notification.AlarmType
	if at != AlarmTypeBirthday && at != AlarmTypeAnniversary {
		return fmt.Errorf("canonical alarm dispatch: celebration alarm type %q is not birthday or anniversary", at)
	}
	if e.Celebration == nil {
		return fmt.Errorf("canonical alarm dispatch: celebration payload is nil")
	}
	if e.Celebration.Date == "" {
		return fmt.Errorf("canonical alarm dispatch: celebration date is empty")
	}
	return nil
}

func (e AlarmQueueEnvelope) validateYouTubeOutboxDispatch() error {
	if err := validateCanonicalNotification(e.Notification); err != nil {
		return err
	}
	if e.YouTubeOutbox == nil {
		return fmt.Errorf("canonical alarm dispatch: youtube outbox payload is nil")
	}
	if err := e.YouTubeOutbox.Validate(); err != nil {
		return fmt.Errorf("canonical alarm dispatch: %w", err)
	}
	return validateCanonicalYouTubeOutboxMatch(e.Notification, e.YouTubeOutbox)
}

func validateCanonicalNotification(notification AlarmNotification) error {
	switch {
	case notification.RoomID == "":
		return fmt.Errorf("canonical alarm dispatch: room id is empty")
	case notification.AlarmType == "":
		return fmt.Errorf("canonical alarm dispatch: alarm type is empty")
	case !notification.AlarmType.IsValid():
		return fmt.Errorf("canonical alarm dispatch: alarm type %q is invalid", notification.AlarmType)
	default:
		return nil
	}
}

func validateCanonicalYouTubeOutboxMatch(notification AlarmNotification, payload *YouTubeOutboxDispatchPayload) error {
	switch {
	case notification.AlarmType != payload.AlarmType:
		return fmt.Errorf("canonical alarm dispatch: notification alarm type %q does not match source alarm type %q", notification.AlarmType, payload.AlarmType)
	case payload.Identity() == "":
		return fmt.Errorf("canonical alarm dispatch: youtube outbox identity is empty")
	default:
		return nil
	}
}

func errorsJoin(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
