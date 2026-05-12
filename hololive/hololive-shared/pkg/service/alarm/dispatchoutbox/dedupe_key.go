package dispatchoutbox

import (
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
}

func BuildDedupeKey(input DedupeInput) string {
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

func BuildDedupeKeyFromEnvelope(envelope domain.AlarmQueueEnvelope) string {
	input := EnvelopeDedupeInput(envelope)
	if len(envelope.ClaimKeys) > 0 {
		input.Category = envelope.ClaimKeys[len(envelope.ClaimKeys)-1]
	}
	return BuildDedupeKey(input)
}
