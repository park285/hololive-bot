package dispatchoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type eventPayloadEnvelope struct {
	Notification   eventPayloadNotification              `json:"notification"`
	SourceKind     domain.AlarmDispatchSourceKind        `json:"source_kind,omitempty"`
	Celebration    *domain.CelebrationDispatchPayload    `json:"celebration,omitempty"`
	DeliveryDigest *domain.DeliveryDigestDispatchPayload `json:"delivery_digest,omitempty"`
	XSpace         *domain.XSpaceDispatchPayload         `json:"x_space,omitempty"`
	Version        uint8                                 `json:"version"`
}

type eventPayloadNotification struct {
	AlarmType                   domain.AlarmType `json:"alarm_type,omitempty"`
	Channel                     *domain.Channel  `json:"channel"`
	Stream                      *domain.Stream   `json:"stream"`
	MinutesUntil                int              `json:"minutes_until"`
	ScheduleChangeMessage       string           `json:"schedule_change_message,omitempty"`
	ScheduleChangePreviousStart string           `json:"schedule_change_previous_start,omitempty"`
}

func buildLedgerRows(envelope *AlarmQueueEnvelope) (eventInsert, deliveryInsert, error) {
	if err := envelope.ValidateCanonicalDispatch(); err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("build dispatch ledger rows: validate envelope: %w", err)
	}

	dedupeInput := EnvelopeDedupeInput(envelope)
	input := &dedupeInput
	alarmType := input.AlarmType
	eventKey := BuildEventKey(input)
	dedupeKey := buildDedupeKey(input.RoomID, eventKey)

	payload, err := marshalEventPayload(envelope)
	if err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("marshal event payload: %w", err)
	}

	hash := sha256.Sum256(payload)

	deliveryContext, err := jsonv2.Marshal(deliveryContext{Users: envelope.Notification.Users})
	if err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("build dispatch delivery context: %w", err)
	}

	event, delivery := assembleLedgerRows(envelope, input, alarmType, eventKey, dedupeKey, payload, hash, deliveryContext)

	return event, delivery, nil
}

func assembleLedgerRows(
	envelope *AlarmQueueEnvelope,
	input *DedupeInput,
	alarmType domain.AlarmType,
	eventKey, dedupeKey string,
	payload []byte,
	hash [sha256.Size]byte,
	deliveryContext []byte,
) (eventInsert, deliveryInsert) {
	dispatchGroupKey := BuildDispatchGroupKeyFromEnvelope(envelope)

	event := eventInsert{
		EventKey:    eventKey,
		PayloadHash: hex.EncodeToString(hash[:]),
		AlarmType:   alarmType,
		ChannelID:   input.ChannelID,
		StreamID:    input.StreamID,
		Category:    eventCategory(input),
		Payload:     payload,
	}

	delivery := deliveryInsert{
		EventKey:         eventKey,
		RoomID:           input.RoomID,
		DedupeKey:        dedupeKey,
		ClaimKeys:        envelope.ClaimKeys,
		DeliveryContext:  deliveryContext,
		DispatchGroupKey: dispatchGroupKey,
	}

	return event, delivery
}

func eventCategory(input *DedupeInput) string {
	if input.SourceKind != "" {
		return string(input.SourceKind)
	}

	category := strings.TrimSpace(input.Category)
	if category != "" {
		return category
	}

	return strconv.Itoa(input.MinutesUntil)
}

func marshalEventPayload(envelope *AlarmQueueEnvelope) ([]byte, error) {
	payload := eventPayloadEnvelope{
		Notification: eventPayloadNotification{
			AlarmType:                   envelope.Notification.AlarmType,
			Channel:                     envelope.Notification.Channel,
			Stream:                      canonicalEventStream(envelope.Notification.Stream),
			MinutesUntil:                envelope.Notification.MinutesUntil,
			ScheduleChangeMessage:       envelope.Notification.ScheduleChangeMessage,
			ScheduleChangePreviousStart: envelope.Notification.ScheduleChangePreviousStart,
		},
		SourceKind:     envelope.SourceKind,
		Celebration:    envelope.Celebration,
		DeliveryDigest: envelope.DeliveryDigest,
		XSpace:         envelope.XSpace,
		Version:        envelope.Version,
	}

	raw, err := jsonv2.Marshal(payload, jsonv2.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("marshal dispatch event payload: %w", err)
	}

	return raw, nil
}

// event payload는 방과 무관하게 공유하는 내용 식별자이고 hash가 다르면 나중에 합류한 방의 delivery를 거절한다.
// 시청자 수는 관측할 때마다 바뀌고 알림 본문에 쓰지 않으므로 identity에서 뺀다.
func canonicalEventStream(stream *domain.Stream) *domain.Stream {
	if stream == nil || stream.ViewerCount == nil {
		return stream
	}

	canonical := *stream

	canonical.ViewerCount = nil

	return &canonical
}
