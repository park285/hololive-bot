package dispatchoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type eventPayloadEnvelope struct {
	Notification   eventPayloadNotification              `json:"notification"`
	SourceKind     domain.AlarmDispatchSourceKind        `json:"source_kind,omitempty"`
	YouTubeOutbox  *domain.YouTubeOutboxDispatchPayload  `json:"youtube_outbox,omitempty"`
	Celebration    *domain.CelebrationDispatchPayload    `json:"celebration,omitempty"`
	DeliveryDigest *domain.DeliveryDigestDispatchPayload `json:"delivery_digest,omitempty"`
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

func buildLedgerRows(envelope *domain.AlarmQueueEnvelope, status Status) (eventInsert, deliveryInsert, error) {
	if err := envelope.ValidateCanonicalDispatch(); err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("build dispatch ledger rows: validate envelope: %w", err)
	}

	preparedInput := prepareEnvelopeDedupeInput(envelope)
	input := &preparedInput.input
	alarmType := input.AlarmType

	if alarmType == "" {
		alarmType = domain.AlarmTypeLive
		input.AlarmType = alarmType
		envelope.Notification.AlarmType = alarmType
	}

	eventKey := preparedInput.eventKey()
	dedupeKey := buildDedupeKey(input.RoomID, eventKey)

	payload, err := marshalEventPayload(envelope)
	if err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("marshal event payload: %w", err)
	}

	if validateErr := validateEventPayloadRoomAgnostic(payload); validateErr != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("validate event payload room agnostic: %w", validateErr)
	}

	hash := sha256.Sum256(payload)

	deliveryContext, err := jsonv2.Marshal(deliveryContext{Users: envelope.Notification.Users})
	if err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("build dispatch delivery context: %w", err)
	}

	event, delivery := assembleLedgerRows(envelope, input, status, alarmType, eventKey, dedupeKey, payload, hash, deliveryContext)

	return event, delivery, nil
}

func assembleLedgerRows(
	envelope *domain.AlarmQueueEnvelope,
	input *DedupeInput,
	status Status,
	alarmType domain.AlarmType,
	eventKey, dedupeKey string,
	payload []byte,
	hash [sha256.Size]byte,
	deliveryContext []byte,
) (eventInsert, deliveryInsert) {
	dispatchGroupKey := ""

	if status == StatusPending {
		dispatchGroupKey = BuildDispatchGroupKeyFromEnvelope(envelope)
	}

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
		Status:           status,
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

func marshalEventPayload(envelope *domain.AlarmQueueEnvelope) ([]byte, error) {
	payload := eventPayloadEnvelope{
		Notification: eventPayloadNotification{
			AlarmType:                   envelope.Notification.AlarmType,
			Channel:                     envelope.Notification.Channel,
			Stream:                      envelope.Notification.Stream,
			MinutesUntil:                envelope.Notification.MinutesUntil,
			ScheduleChangeMessage:       envelope.Notification.ScheduleChangeMessage,
			ScheduleChangePreviousStart: envelope.Notification.ScheduleChangePreviousStart,
		},
		SourceKind:     envelope.SourceKind,
		YouTubeOutbox:  envelope.YouTubeOutbox,
		Celebration:    envelope.Celebration,
		DeliveryDigest: envelope.DeliveryDigest,
		Version:        envelope.Version,
	}

	raw, err := jsonv2.Marshal(payload, jsonv2.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("marshal dispatch event payload: %w", err)
	}

	return raw, nil
}

func validateEventPayloadRoomAgnostic(raw []byte) error {
	var payload struct {
		RoomID       jsontext.Value `json:"room_id"`
		RoomIDCamel  jsontext.Value `json:"roomId"`
		Room         jsontext.Value `json:"room"`
		Users        jsontext.Value `json:"users"`
		Notification struct {
			RoomID      jsontext.Value `json:"room_id"`
			RoomIDCamel jsontext.Value `json:"roomId"`
			Room        jsontext.Value `json:"room"`
			Users       jsontext.Value `json:"users"`
		} `json:"notification"`
	}

	if err := jsonv2.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("validate dispatch event payload: %w", err)
	}

	if hasDeliverySpecificFields(payload.RoomID, payload.RoomIDCamel, payload.Room, payload.Users) {
		return errors.New("validate dispatch event payload: delivery-specific top-level field")
	}

	if hasDeliverySpecificFields(payload.Notification.RoomID, payload.Notification.RoomIDCamel, payload.Notification.Room, payload.Notification.Users) {
		return errors.New("validate dispatch event payload: delivery-specific notification field")
	}

	return nil
}

func hasDeliverySpecificFields(fields ...jsontext.Value) bool {
	for _, field := range fields {
		if field != nil {
			return true
		}
	}

	return false
}
