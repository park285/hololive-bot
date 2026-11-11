package dispatchoutbox

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// AlarmQueueEnvelope의 JSON 형식은 이미 저장된 dispatch event payload를 그대로 decode할 수 있어야 한다.
type AlarmQueueEnvelope struct {
	DispatchOutboxID int64                              `json:"dispatch_outbox_id"`
	DispatchGroupKey string                             `json:"dispatch_group_key,omitempty"`
	SendUnitID       int64                              `json:"send_unit_id"`
	ClientRequestID  string                             `json:"client_request_id,omitempty"`
	Notification     domain.AlarmNotification           `json:"notification"`
	SourceKind       domain.AlarmDispatchSourceKind     `json:"source_kind,omitempty"`
	Celebration      *domain.CelebrationDispatchPayload `json:"celebration,omitempty"`
	XSpace           *domain.XSpaceDispatchPayload      `json:"x_space,omitempty"`
	ClaimKeys        []string                           `json:"claim_keys"`
	EnqueuedAt       string                             `json:"enqueued_at"`
	Version          uint8                              `json:"version"`
	Retry            *AlarmQueueRetryMetadata           `json:"retry,omitempty"`
	SourcePayloadRaw string                             `json:"source_payload,omitempty"`
	// RequestUnpinned는 claim이 본 send unit 요청의 미고정 여부다. 큐 payload에는 싣지 않는다.
	RequestUnpinned bool `json:"-"`
}

type AlarmQueueRetryMetadata struct {
	Attempt       int    `json:"attempt"`
	RetryAfterMS  int64  `json:"retry_after_ms"`
	LastError     string `json:"last_error,omitempty"`
	LastErrorCode string `json:"last_error_code,omitempty"`
}

type alarmQueueEnvelopeWire struct {
	DispatchOutboxID int64                              `json:"dispatch_outbox_id"`
	Notification     domain.AlarmNotification           `json:"notification"`
	SourceKind       domain.AlarmDispatchSourceKind     `json:"source_kind,omitempty"`
	Celebration      *domain.CelebrationDispatchPayload `json:"celebration,omitempty"`
	XSpace           *domain.XSpaceDispatchPayload      `json:"x_space,omitempty"`
	ClaimKeys        []string                           `json:"claim_keys"`
	EnqueuedAt       string                             `json:"enqueued_at"`
	Version          uint8                              `json:"version"`
	Retry            *AlarmQueueRetryMetadata           `json:"retry,omitempty"`
	SourcePayload    string                             `json:"source_payload,omitempty"`
}

var canonicalDispatchValidators = map[domain.AlarmDispatchSourceKind]func(*AlarmQueueEnvelope) error{
	domain.AlarmDispatchSourceKindCelebration: (*AlarmQueueEnvelope).validateCelebrationDispatch,
	domain.AlarmDispatchSourceKindXSpace:      (*AlarmQueueEnvelope).validateXSpaceDispatch,
}

func (e *AlarmQueueEnvelope) SourcePayload() string {
	if e == nil {
		return ""
	}

	return e.SourcePayloadRaw
}

func (e *AlarmQueueEnvelope) UnmarshalJSON(data []byte) error {
	var wire alarmQueueEnvelopeWire

	if err := jsonv2.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("unmarshal alarm queue envelope: %w", err)
	}

	// 저장 payload는 발행 시점에 alarm_type을 채운다. 누락을 Live로 간주하지 않고, 다른 알림 종류로 오인해 보내지 않도록
	// decode 오류로 드러낸다.
	if wire.Notification.AlarmType == "" {
		return errors.New("unmarshal alarm queue envelope: notification.alarm_type is empty")
	}

	*e = AlarmQueueEnvelope{
		DispatchOutboxID: wire.DispatchOutboxID,
		Notification:     wire.Notification,
		SourceKind:       wire.SourceKind,
		Celebration:      wire.Celebration,
		XSpace:           wire.XSpace,
		ClaimKeys:        wire.ClaimKeys,
		EnqueuedAt:       wire.EnqueuedAt,
		Version:          wire.Version,
		Retry:            wire.Retry,
		SourcePayloadRaw: wire.SourcePayload,
	}

	return nil
}

func (e *AlarmQueueEnvelope) ValidateCanonicalDispatch() error {
	if e == nil {
		return errors.New("canonical alarm dispatch: envelope is nil")
	}

	if e.SourceKind == "" {
		if err := e.Notification.ValidateLiveDispatchRoute(); err != nil {
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

func (e *AlarmQueueEnvelope) validateCelebrationDispatch() error {
	if e.Notification.RoomID == "" {
		return errors.New("canonical alarm dispatch: celebration room id is empty")
	}

	at := e.Notification.AlarmType
	if at != domain.AlarmTypeBirthday && at != domain.AlarmTypeAnniversary {
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

	if e.Celebration.Kind == domain.CelebrationKindBirthdayStream && strings.TrimSpace(e.Celebration.VideoID) == "" {
		return errors.New("canonical alarm dispatch: celebration birthday stream video id is empty")
	}

	return nil
}

func (e *AlarmQueueEnvelope) validateXSpaceDispatch() error {
	if err := validateCanonicalNotification(&e.Notification); err != nil {
		return fmt.Errorf("x space notification: %w", err)
	}

	if e.Notification.AlarmType != domain.AlarmTypeLive || e.Notification.Channel != nil || e.Notification.Stream != nil {
		return errors.New("x space dispatch requires LIVE subscription without a stream payload")
	}

	if e.Celebration != nil {
		return errors.New("x space dispatch contains another source payload")
	}

	if err := e.XSpace.Validate(); err != nil {
		return fmt.Errorf("validate x space payload: %w", err)
	}

	return nil
}

func validateCanonicalNotification(notification *domain.AlarmNotification) error {
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
