package dispatchoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type DedupeInput struct {
	RoomID                      string
	ChannelID                   string
	AlarmType                   domain.AlarmType
	StreamID                    string
	Title                       string
	StartScheduled              time.Time
	MinutesUntil                int
	ScheduleChangePreviousStart string
	Category                    string
	SourceKind                  domain.AlarmDispatchSourceKind
	SourceIdentity              string
}

func BuildDedupeKey(input *DedupeInput) string {
	return buildDedupeKey(input.RoomID, BuildEventKey(input))
}

func buildDedupeKey(roomID, eventKey string) string {
	dedupeKey := fmt.Sprintf("v2:room:%s:event:%s", roomID, eventKey)
	if len(dedupeKey) <= 768 {
		return dedupeKey
	}

	sum := sha256.Sum256([]byte(eventKey))

	return fmt.Sprintf("v2:room:%s:event_sha:%s", roomID, hex.EncodeToString(sum[:]))
}

const eventKeyMaxLength = 512

func BuildEventKey(input *DedupeInput) string {
	raw := buildRawEventKey(input)
	if len(raw) <= eventKeyMaxLength {
		return raw
	}

	sum := sha256.Sum256([]byte(raw))

	return fmt.Sprintf("event_sha:%s", hex.EncodeToString(sum[:]))
}

func buildRawEventKey(input *DedupeInput) string {
	if input.SourceKind == domain.AlarmDispatchSourceKindXSpace {
		return "x-space:start:" + input.SourceIdentity
	}

	if input.SourceKind == domain.AlarmDispatchSourceKindCelebration {
		return "celebration:" + input.SourceIdentity
	}

	if input.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest {
		return "delivery-digest:" + input.SourceIdentity
	}

	alarmType := input.AlarmType

	category := strings.TrimSpace(input.Category)
	if category == "" {
		category = strconv.Itoa(input.MinutesUntil)
	}

	scheduledUnix := int64(0)

	if !input.StartScheduled.IsZero() {
		scheduledUnix = input.StartScheduled.UTC().Truncate(time.Minute).Unix()
	}

	oldStart := strings.TrimSpace(input.ScheduleChangePreviousStart)
	if oldStart != "" {
		return fmt.Sprintf("schedule:%s:%s:%s:%d:%s:%s",
			input.ChannelID,
			input.StreamID,
			oldStart,
			scheduledUnix,
			category,
			alarmType,
		)
	}

	return fmt.Sprintf("live:%s:%s:%d:%s:%s",
		input.ChannelID,
		input.StreamID,
		scheduledUnix,
		category,
		alarmType,
	)
}

func EnvelopeDedupeInput(envelope *domain.AlarmQueueEnvelope) DedupeInput {
	input := envelopeNotificationDedupeInput(&envelope.Notification)
	if envelope.SourceKind == domain.AlarmDispatchSourceKindXSpace && envelope.XSpace != nil {
		input.SourceKind = envelope.SourceKind
		input.SourceIdentity = envelope.XSpace.SpaceID
		input.ChannelID = envelope.XSpace.ChannelID
		input.StreamID = envelope.XSpace.SpaceID
		input.Category = string(envelope.SourceKind)
	}

	applyCelebrationDedupeSource(&input, envelope)
	applyDeliveryDigestDedupeSource(&input, envelope)

	return input
}

func applyDeliveryDigestDedupeSource(input *DedupeInput, envelope *domain.AlarmQueueEnvelope) {
	if envelope.SourceKind != domain.AlarmDispatchSourceKindDeliveryDigest || envelope.DeliveryDigest == nil {
		return
	}

	input.SourceKind = envelope.SourceKind
	input.SourceIdentity = envelope.DeliveryDigest.ContentIdentity()
	input.AlarmType = envelope.Notification.AlarmType
	input.Category = string(envelope.SourceKind)
}

func envelopeNotificationDedupeInput(notification *domain.AlarmNotification) DedupeInput {
	channelID := ""
	streamID := ""
	title := ""

	var scheduled time.Time

	if notification.Channel != nil {
		channelID = notification.Channel.ID
	}

	if notification.Stream != nil {
		streamID = notification.Stream.ID
		title = notification.Stream.Title

		if channelID == "" {
			channelID = notification.Stream.ChannelID
		}

		if notification.Stream.StartScheduled != nil {
			scheduled = *notification.Stream.StartScheduled
		}
	}

	category := ""

	if notification.IsLiveCatchup() {
		category = sharedalarmkeys.NotificationCategoryLiveCatchup
		scheduled = time.Time{}
	}

	return DedupeInput{
		RoomID:                      notification.RoomID,
		ChannelID:                   channelID,
		AlarmType:                   notification.AlarmType,
		StreamID:                    streamID,
		Title:                       title,
		StartScheduled:              scheduled,
		MinutesUntil:                notification.MinutesUntil,
		ScheduleChangePreviousStart: notification.ScheduleChangePreviousStart,
		Category:                    category,
	}
}

func applyCelebrationDedupeSource(input *DedupeInput, envelope *domain.AlarmQueueEnvelope) {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		input.SourceKind = envelope.SourceKind
		input.SourceIdentity = envelope.Celebration.Identity()
		input.ChannelID = envelope.Celebration.ChannelID
		input.AlarmType = envelope.Notification.AlarmType
		input.Category = string(envelope.SourceKind)
	}
}

func BuildDedupeKeyFromEnvelope(envelope *domain.AlarmQueueEnvelope) string {
	input := EnvelopeDedupeInput(envelope)
	return BuildDedupeKey(&input)
}
