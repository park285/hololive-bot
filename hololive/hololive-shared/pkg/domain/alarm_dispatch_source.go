package domain

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
)

type AlarmDispatchSourceKind string

const (
	AlarmDispatchSourceKindYouTubeOutbox  AlarmDispatchSourceKind = "youtube_outbox"
	AlarmDispatchSourceKindCelebration    AlarmDispatchSourceKind = "celebration"
	AlarmDispatchSourceKindDeliveryDigest AlarmDispatchSourceKind = "delivery_digest"

	maxYouTubeOutboxIdentityItems   = 1000
	maxYouTubeOutboxContentIDBytes  = 512
	maxDeliveryDigestPeriodKeyBytes = 256
	maxDeliveryDigestMessageBytes   = 64 * 1024
)

var canonicalDispatchValidators = map[AlarmDispatchSourceKind]func(*AlarmQueueEnvelope) error{
	AlarmDispatchSourceKindYouTubeOutbox:  (*AlarmQueueEnvelope).validateYouTubeOutboxDispatch,
	AlarmDispatchSourceKindCelebration:    (*AlarmQueueEnvelope).validateCelebrationDispatch,
	AlarmDispatchSourceKindDeliveryDigest: (*AlarmQueueEnvelope).validateDeliveryDigestDispatch,
}

type DeliveryDigestDispatchPayload struct {
	Kind               DeliveryOutboxKind `json:"kind"`
	PeriodKey          string             `json:"period_key"`
	PreRenderedMessage string             `json:"pre_rendered_message"`
}

func (p *DeliveryDigestDispatchPayload) Validate() error {
	if p == nil {
		return errors.New("delivery digest dispatch payload is nil")
	}

	if !p.Kind.IsValid() {
		return fmt.Errorf("delivery digest dispatch payload kind %q is invalid", p.Kind)
	}

	periodKey := strings.TrimSpace(p.PeriodKey)
	if periodKey == "" {
		return errors.New("delivery digest dispatch payload period key is empty")
	}

	if len(periodKey) > maxDeliveryDigestPeriodKeyBytes {
		return fmt.Errorf("delivery digest dispatch payload period key is too long: %d > %d bytes", len(periodKey), maxDeliveryDigestPeriodKeyBytes)
	}

	message := strings.TrimSpace(p.PreRenderedMessage)
	if message == "" {
		return errors.New("delivery digest dispatch payload message is empty")
	}

	if len(message) > maxDeliveryDigestMessageBytes {
		return fmt.Errorf("delivery digest dispatch payload message is too long: %d > %d bytes", len(message), maxDeliveryDigestMessageBytes)
	}

	return nil
}

func (p *DeliveryDigestDispatchPayload) Identity() string {
	if p == nil || !p.Kind.IsValid() || strings.TrimSpace(p.PeriodKey) == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(string(p.Kind) + "\x00" + strings.TrimSpace(p.PeriodKey)))

	return "sha256:" + hex.EncodeToString(sum[:])
}

func (p *DeliveryDigestDispatchPayload) ContentIdentity() string {
	if p == nil {
		return ""
	}

	identity := p.Identity()
	if identity == "" {
		return ""
	}

	message := strings.TrimSpace(p.PreRenderedMessage)
	if message == "" {
		return ""
	}

	messageHash := sha256.Sum256([]byte(message))

	return identity + ":message_sha256:" + hex.EncodeToString(messageHash[:])
}

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
		return errors.New("youtube outbox dispatch payload is nil")
	}

	if err := firstError(
		validateYouTubeOutboxHeader(p),
		validateYouTubeOutboxItems(p.Items),
	); err != nil {
		return fmt.Errorf("first error: %w", err)
	}

	return nil
}

func validateYouTubeOutboxHeader(p *YouTubeOutboxDispatchPayload) error {
	if !p.Kind.IsValid() {
		return fmt.Errorf("youtube outbox dispatch payload kind %q is invalid", p.Kind)
	}

	if err := validateYouTubeOutboxAlarmHeader(p.AlarmType, p.ChannelID); err != nil {
		return fmt.Errorf("validate youtube outbox alarm header: %w", err)
	}

	if want := p.Kind.ToAlarmType(); p.AlarmType != want {
		return fmt.Errorf("youtube outbox dispatch payload alarm type %q does not match kind %q (want %q)", p.AlarmType, p.Kind, want)
	}

	if hasTemplateAndPreRenderedMessage(p) {
		return errors.New("youtube outbox dispatch payload cannot set both render template key and pre-rendered message")
	}

	return nil
}

func validateYouTubeOutboxAlarmHeader(alarmType AlarmType, channelID string) error {
	if alarmType == "" {
		return errors.New("youtube outbox dispatch payload alarm type is empty")
	}

	if !alarmType.IsValid() {
		return fmt.Errorf("youtube outbox dispatch payload alarm type %q is invalid", alarmType)
	}

	if strings.TrimSpace(channelID) == "" {
		return errors.New("youtube outbox dispatch payload channel id is empty")
	}

	return nil
}

func hasTemplateAndPreRenderedMessage(p *YouTubeOutboxDispatchPayload) bool {
	return strings.TrimSpace(string(p.RenderTemplateKey)) != "" && strings.TrimSpace(p.PreRenderedMessage) != ""
}

func validateYouTubeOutboxItems(items []YouTubeOutboxItem) error {
	if err := validateYouTubeOutboxIdentityItems(items); err != nil {
		return fmt.Errorf("validate youtube outbox identity items: %w", err)
	}

	for i := range items {
		if strings.TrimSpace(items[i].Payload) == "" {
			return fmt.Errorf("youtube outbox dispatch payload item %d payload is empty", i)
		}
	}

	return nil
}

func validateYouTubeOutboxIdentityItems(items []YouTubeOutboxItem) error {
	if len(items) == 0 {
		return errors.New("youtube outbox dispatch payload items are empty")
	}

	if len(items) > maxYouTubeOutboxIdentityItems {
		return fmt.Errorf("youtube outbox dispatch payload has too many items: %d > %d", len(items), maxYouTubeOutboxIdentityItems)
	}

	for i := range items {
		contentID := strings.TrimSpace(items[i].ContentID)
		if contentID == "" {
			return fmt.Errorf("youtube outbox dispatch payload item %d content id is empty", i)
		}

		if len(contentID) > maxYouTubeOutboxContentIDBytes {
			return fmt.Errorf("youtube outbox dispatch payload item %d content id is too long: %d > %d bytes", i, len(contentID), maxYouTubeOutboxContentIDBytes)
		}
	}

	return nil
}

func (p *YouTubeOutboxDispatchPayload) IdentityParts() []string {
	parts, err := p.canonicalIdentityParts()
	if err != nil {
		return nil
	}

	return parts
}

func (p *YouTubeOutboxDispatchPayload) Identity() string {
	identity, err := p.CanonicalIdentity()
	if err != nil {
		return ""
	}

	return identity
}

func (p *YouTubeOutboxDispatchPayload) CanonicalIdentity() (string, error) {
	parts, err := p.canonicalIdentityParts()
	if err != nil {
		return "", fmt.Errorf("canonical identity parts: %w", err)
	}

	hash := sha256.New()

	_, _ = hash.Write([]byte("youtube-outbox-content-identity-v1\x00"))

	var size [4]byte

	for _, part := range parts {
		binary.BigEndian.PutUint32(size[:], boundedContentIDByteLength(part))

		_, _ = hash.Write(size[:])
		_, _ = hash.Write([]byte(part))
	}

	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func boundedContentIDByteLength(contentID string) uint32 {
	var length uint32

	for range len(contentID) {
		length++
	}

	return length
}

func (p *YouTubeOutboxDispatchPayload) canonicalIdentityParts() ([]string, error) {
	if p == nil {
		return nil, errors.New("youtube outbox dispatch payload is nil")
	}

	if err := validateYouTubeOutboxIdentityItems(p.Items); err != nil {
		return nil, fmt.Errorf("validate youtube outbox identity items: %w", err)
	}

	seen := make(map[string]struct{}, len(p.Items))
	parts := make([]string, 0, len(p.Items))

	for i := range p.Items {
		contentID := strings.TrimSpace(p.Items[i].ContentID)
		if _, ok := seen[contentID]; ok {
			continue
		}

		seen[contentID] = struct{}{}
		parts = append(parts, contentID)
	}

	slices.Sort(parts)

	return parts, nil
}

func (e *AlarmQueueEnvelope) HasYouTubeOutboxSource() bool {
	return e != nil && e.SourceKind == AlarmDispatchSourceKindYouTubeOutbox
}

func (e *AlarmQueueEnvelope) ValidateCanonicalDispatch() error {
	if e == nil {
		return errors.New("canonical alarm dispatch: envelope is nil")
	}

	if e.SourceKind == "" {
		if err := e.validateLiveDispatch(); err != nil {
			return fmt.Errorf("validate live dispatch: %w", err)
		}

		return nil
	}

	validate, ok := canonicalDispatchValidators[e.SourceKind]
	if !ok {
		return fmt.Errorf("canonical alarm dispatch: unsupported source kind %q", e.SourceKind)
	}

	if err := validate(e); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateLiveDispatch() error {
	alarmType := e.Notification.AlarmType
	if alarmType == "" {
		alarmType = AlarmTypeLive
	}

	if err := validateLiveDispatchAlarmType(alarmType); err != nil {
		return fmt.Errorf("validate live dispatch alarm type: %w", err)
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateCelebrationDispatch() error {
	if e.Notification.RoomID == "" {
		return errors.New("canonical alarm dispatch: celebration room id is empty")
	}

	at := e.Notification.AlarmType
	if at != AlarmTypeBirthday && at != AlarmTypeAnniversary {
		return fmt.Errorf("canonical alarm dispatch: celebration alarm type %q is not birthday or anniversary", at)
	}

	if e.Celebration == nil {
		return errors.New("canonical alarm dispatch: celebration payload is nil")
	}

	if e.Celebration.Date == "" {
		return errors.New("canonical alarm dispatch: celebration date is empty")
	}

	if e.Celebration.Kind == CelebrationKindBirthdayStream && strings.TrimSpace(e.Celebration.VideoID) == "" {
		return errors.New("canonical alarm dispatch: celebration birthday stream video id is empty")
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateYouTubeOutboxDispatch() error {
	if err := validateCanonicalNotification(&e.Notification); err != nil {
		return fmt.Errorf("validate canonical notification: %w", err)
	}

	if e.YouTubeOutbox == nil {
		return errors.New("canonical alarm dispatch: youtube outbox payload is nil")
	}

	if err := e.YouTubeOutbox.Validate(); err != nil {
		return fmt.Errorf("canonical alarm dispatch: %w", err)
	}

	if err := validateCanonicalYouTubeOutboxMatch(&e.Notification, e.YouTubeOutbox); err != nil {
		return fmt.Errorf("validate canonical youtube outbox match: %w", err)
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateDeliveryDigestDispatch() error {
	if err := validateCanonicalNotification(&e.Notification); err != nil {
		return fmt.Errorf("validate canonical notification: %w", err)
	}

	if e.Notification.AlarmType != AlarmTypeCommunity {
		return fmt.Errorf("canonical alarm dispatch: delivery digest storage alarm type %q is not community", e.Notification.AlarmType)
	}

	if e.DeliveryDigest == nil {
		return errors.New("canonical alarm dispatch: delivery digest payload is nil")
	}

	if err := e.DeliveryDigest.Validate(); err != nil {
		return fmt.Errorf("canonical alarm dispatch: %w", err)
	}

	return nil
}

func validateCanonicalNotification(notification *AlarmNotification) error {
	if notification == nil {
		return errors.New("canonical alarm dispatch: notification is nil")
	}

	switch {
	case notification.RoomID == "":
		return errors.New("canonical alarm dispatch: room id is empty")
	case notification.AlarmType == "":
		return errors.New("canonical alarm dispatch: alarm type is empty")
	case !notification.AlarmType.IsValid():
		return fmt.Errorf("canonical alarm dispatch: alarm type %q is invalid", notification.AlarmType)
	default:
		return nil
	}
}

func validateCanonicalYouTubeOutboxMatch(notification *AlarmNotification, payload *YouTubeOutboxDispatchPayload) error {
	if notification.AlarmType != payload.AlarmType {
		return fmt.Errorf("canonical alarm dispatch: notification alarm type %q does not match source alarm type %q", notification.AlarmType, payload.AlarmType)
	}

	return nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}
