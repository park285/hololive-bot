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

func BuildDedupeKey(input DedupeInput) string {
	eventKey := BuildEventKey(input)
	dedupeKey := fmt.Sprintf("v2:room:%s:event:%s", input.RoomID, eventKey)
	if len(dedupeKey) <= 768 {
		return dedupeKey
	}
	sum := sha256.Sum256([]byte(eventKey))
	return fmt.Sprintf("v2:room:%s:event_sha:%s", input.RoomID, hex.EncodeToString(sum[:]))
}

func buildLegacyDedupeKey(input DedupeInput) string {
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

func BuildEventKey(input DedupeInput) string {
	if input.SourceKind == domain.AlarmDispatchSourceKindCelebration {
		return fmt.Sprintf("celebration:%s", input.SourceIdentity)
	}
	if input.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return fmt.Sprintf("youtube-outbox:%s:%s", input.SourceOutboxKind, input.SourceIdentity)
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

func EnvelopeDedupeInput(envelope domain.AlarmQueueEnvelope) DedupeInput {
	notification := envelope.Notification
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
	input := DedupeInput{
		RoomID:                      notification.RoomID,
		ChannelID:                   channelID,
		AlarmType:                   notification.AlarmType,
		StreamID:                    streamID,
		Title:                       title,
		StartScheduled:              scheduled,
		MinutesUntil:                notification.MinutesUntil,
		ScheduleChangePreviousStart: notification.ScheduleChangePreviousStart,
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		input.SourceKind = envelope.SourceKind
		input.SourceIdentity = envelope.Celebration.Identity()
		input.ChannelID = envelope.Celebration.ChannelID
		input.AlarmType = notification.AlarmType
		input.Category = string(envelope.SourceKind)
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		input.SourceKind = envelope.SourceKind
		input.SourceIdentity = envelope.YouTubeOutbox.Identity()
		input.SourceOutboxKind = envelope.YouTubeOutbox.Kind
		input.ChannelID = strings.TrimSpace(envelope.YouTubeOutbox.ChannelID)
		input.AlarmType = envelope.YouTubeOutbox.AlarmType
		input.Category = string(envelope.SourceKind)
	}
	return input
}

func BuildDedupeKeyFromEnvelope(envelope domain.AlarmQueueEnvelope) string {
	return BuildDedupeKey(EnvelopeDedupeInput(envelope))
}

func BuildLegacyDedupeKeyFromEnvelope(envelope domain.AlarmQueueEnvelope) string {
	input := EnvelopeDedupeInput(envelope)
	if len(envelope.ClaimKeys) > 0 {
		input.Category = envelope.ClaimKeys[len(envelope.ClaimKeys)-1]
	}
	return buildLegacyDedupeKey(input)
}
