package dispatchoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

type eventPayloadEnvelope struct {
	Notification eventPayloadNotification `json:"notification"`
	Version      uint8                    `json:"version"`
}

type eventPayloadNotification struct {
	AlarmType                   domain.AlarmType `json:"alarm_type,omitempty"`
	Channel                     *domain.Channel  `json:"channel"`
	Stream                      *domain.Stream   `json:"stream"`
	MinutesUntil                int              `json:"minutes_until"`
	ScheduleChangeMessage       string           `json:"schedule_change_message,omitempty"`
	ScheduleChangePreviousStart string           `json:"schedule_change_previous_start,omitempty"`
}

func buildLedgerRows(envelope domain.AlarmQueueEnvelope, status Status) (eventInsert, deliveryInsert, error) {
	input := EnvelopeDedupeInput(envelope)
	alarmType := input.AlarmType
	if alarmType == "" {
		alarmType = domain.AlarmTypeLive
		input.AlarmType = alarmType
		envelope.Notification.AlarmType = alarmType
	}
	eventKey := BuildEventKey(input)
	dedupeKey := BuildDedupeKey(input)
	payload, err := marshalEventPayload(envelope)
	if err != nil {
		return eventInsert{}, deliveryInsert{}, err
	}
	if err := validateEventPayloadRoomAgnostic(payload); err != nil {
		return eventInsert{}, deliveryInsert{}, err
	}
	hash := sha256.Sum256(payload)
	deliveryContext, err := json.Marshal(deliveryContext{Users: envelope.Notification.Users})
	if err != nil {
		return eventInsert{}, deliveryInsert{}, fmt.Errorf("build dispatch delivery context: %w", err)
	}
	return eventInsert{
			EventKey:    eventKey,
			PayloadHash: hex.EncodeToString(hash[:]),
			AlarmType:   alarmType,
			ChannelID:   input.ChannelID,
			StreamID:    input.StreamID,
			Category:    eventCategory(input),
			Payload:     payload,
		}, deliveryInsert{
			EventKey:        eventKey,
			RoomID:          input.RoomID,
			DedupeKey:       dedupeKey,
			LegacyDedupeKey: BuildLegacyDedupeKeyFromEnvelope(envelope),
			ClaimKeys:       envelope.ClaimKeys,
			DeliveryContext: deliveryContext,
			Status:          status,
		}, nil
}

func eventCategory(input DedupeInput) string {
	category := strings.TrimSpace(input.Category)
	if category != "" {
		return category
	}
	return strconv.Itoa(input.MinutesUntil)
}

func marshalEventPayload(envelope domain.AlarmQueueEnvelope) ([]byte, error) {
	payload := eventPayloadEnvelope{
		Notification: eventPayloadNotification{
			AlarmType:                   envelope.Notification.AlarmType,
			Channel:                     envelope.Notification.Channel,
			Stream:                      envelope.Notification.Stream,
			MinutesUntil:                envelope.Notification.MinutesUntil,
			ScheduleChangeMessage:       envelope.Notification.ScheduleChangeMessage,
			ScheduleChangePreviousStart: envelope.Notification.ScheduleChangePreviousStart,
		},
		Version: envelope.Version,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal dispatch event payload: %w", err)
	}
	return raw, nil
}

func validateEventPayloadRoomAgnostic(raw []byte) error {
	var payload struct {
		RoomID       json.RawMessage `json:"room_id"`
		RoomIDCamel  json.RawMessage `json:"roomId"`
		Room         json.RawMessage `json:"room"`
		Users        json.RawMessage `json:"users"`
		Notification struct {
			RoomID      json.RawMessage `json:"room_id"`
			RoomIDCamel json.RawMessage `json:"roomId"`
			Room        json.RawMessage `json:"room"`
			Users       json.RawMessage `json:"users"`
		} `json:"notification"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("validate dispatch event payload: %w", err)
	}
	if hasDeliverySpecificFields(payload.RoomID, payload.RoomIDCamel, payload.Room, payload.Users) {
		return fmt.Errorf("validate dispatch event payload: delivery-specific top-level field")
	}
	if hasDeliverySpecificFields(payload.Notification.RoomID, payload.Notification.RoomIDCamel, payload.Notification.Room, payload.Notification.Users) {
		return fmt.Errorf("validate dispatch event payload: delivery-specific notification field")
	}
	return nil
}

func hasDeliverySpecificFields(fields ...json.RawMessage) bool {
	for _, field := range fields {
		if field != nil {
			return true
		}
	}
	return false
}
