package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type AlarmDispatchSourceKind string

const (
	AlarmDispatchSourceKindCelebration    AlarmDispatchSourceKind = "celebration"
	AlarmDispatchSourceKindDeliveryDigest AlarmDispatchSourceKind = "delivery_digest"
	// AlarmDispatchSourceKindXSpace는 X 스페이스 시작 알림을 기존 LIVE 구독으로 전달한다.
	AlarmDispatchSourceKindXSpace AlarmDispatchSourceKind = "x_space"

	maxDeliveryDigestPeriodKeyBytes = 256
	maxDeliveryDigestMessageBytes   = 64 * 1024
)

var canonicalDispatchValidators = map[AlarmDispatchSourceKind]func(*AlarmQueueEnvelope) error{
	AlarmDispatchSourceKindCelebration:    (*AlarmQueueEnvelope).validateCelebrationDispatch,
	AlarmDispatchSourceKindDeliveryDigest: (*AlarmQueueEnvelope).validateDeliveryDigestDispatch,
	AlarmDispatchSourceKindXSpace:         (*AlarmQueueEnvelope).validateXSpaceDispatch,
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
	if err := validateLiveDispatchAlarmType(e.Notification.AlarmType); err != nil {
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

	if e.Celebration.MemberID <= 0 {
		return fmt.Errorf("canonical alarm dispatch: celebration member id must be positive, got %d", e.Celebration.MemberID)
	}

	if e.Celebration.Date == "" {
		return errors.New("canonical alarm dispatch: celebration date is empty")
	}

	if e.Celebration.Kind == CelebrationKindBirthdayStream && strings.TrimSpace(e.Celebration.VideoID) == "" {
		return errors.New("canonical alarm dispatch: celebration birthday stream video id is empty")
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
