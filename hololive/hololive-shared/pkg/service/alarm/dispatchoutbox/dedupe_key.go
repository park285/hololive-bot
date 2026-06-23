package dispatchoutbox

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
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
	SourceOutboxKind            domain.OutboxKind
}

func BuildDedupeKey(input *DedupeInput) string {
	eventKey := BuildEventKey(input)
	dedupeKey := fmt.Sprintf("v2:room:%s:event:%s", input.RoomID, eventKey)
	if len(dedupeKey) <= 768 {
		return dedupeKey
	}
	sum := sha256.Sum256([]byte(eventKey))
	return fmt.Sprintf("v2:room:%s:event_sha:%s", input.RoomID, hex.EncodeToString(sum[:]))
}

func buildLegacyDedupeKey(input *DedupeInput) string {
	alarmType := input.AlarmType
	if alarmType == "" {
		alarmType = domain.AlarmTypeLive
	}
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
		return fmt.Sprintf("legacy-schedule:%s:%s:%s:%s:%d:%s:%s",
			input.RoomID,
			input.ChannelID,
			input.StreamID,
			oldStart,
			scheduledUnix,
			category,
			alarmType,
		)
	}
	return fmt.Sprintf("legacy-live:%s:%s:%s:%d:%s:%s",
		input.RoomID,
		input.ChannelID,
		input.StreamID,
		scheduledUnix,
		category,
		alarmType,
	)
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
	if input.SourceKind == domain.AlarmDispatchSourceKindCelebration {
		return "celebration:" + input.SourceIdentity
	}
	if input.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return "youtube-outbox:" + string(input.SourceOutboxKind) + ":" + input.SourceIdentity
	}
	alarmType := input.AlarmType
	if alarmType == "" {
		alarmType = domain.AlarmTypeLive
	}
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
		return fmt.Sprintf("legacy-schedule:%s:%s:%s:%d:%s:%s",
			input.ChannelID,
			input.StreamID,
			oldStart,
			scheduledUnix,
			category,
			alarmType,
		)
	}
	return fmt.Sprintf("legacy-live:%s:%s:%d:%s:%s",
		input.ChannelID,
		input.StreamID,
		scheduledUnix,
		category,
		alarmType,
	)
}

func EnvelopeDedupeInput(envelope *domain.AlarmQueueEnvelope) DedupeInput {
	input := envelopeNotificationDedupeInput(&envelope.Notification)
	applyCelebrationDedupeSource(&input, envelope)
	applyYouTubeOutboxDedupeSource(&input, envelope)
	return input
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
	return DedupeInput{
		RoomID:                      notification.RoomID,
		ChannelID:                   channelID,
		AlarmType:                   notification.AlarmType,
		StreamID:                    streamID,
		Title:                       title,
		StartScheduled:              scheduled,
		MinutesUntil:                notification.MinutesUntil,
		ScheduleChangePreviousStart: notification.ScheduleChangePreviousStart,
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

func applyYouTubeOutboxDedupeSource(input *DedupeInput, envelope *domain.AlarmQueueEnvelope) {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		input.SourceKind = envelope.SourceKind
		input.SourceIdentity = envelope.YouTubeOutbox.Identity()
		input.SourceOutboxKind = envelope.YouTubeOutbox.Kind
		input.ChannelID = strings.TrimSpace(envelope.YouTubeOutbox.ChannelID)
		input.AlarmType = envelope.YouTubeOutbox.AlarmType
		input.Category = string(envelope.SourceKind)
	}
}

func BuildDedupeKeyFromEnvelope(envelope *domain.AlarmQueueEnvelope) string {
	input := EnvelopeDedupeInput(envelope)
	return BuildDedupeKey(&input)
}

func BuildLegacyDedupeKeyFromEnvelope(envelope *domain.AlarmQueueEnvelope) string {
	input := EnvelopeDedupeInput(envelope)
	if len(envelope.ClaimKeys) > 0 {
		input.Category = envelope.ClaimKeys[len(envelope.ClaimKeys)-1]
	}
	return buildLegacyDedupeKey(&input)
}
