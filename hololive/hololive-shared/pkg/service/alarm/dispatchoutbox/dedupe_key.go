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

type preparedDedupeInput struct {
	input                    DedupeInput
	canonicalYouTubeIdentity string
}

func BuildDedupeKey(input *DedupeInput) string {
	return buildDedupeKey(input.RoomID, buildEventKey(input, ""))
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
	return buildEventKey(input, "")
}

func buildEventKey(input *DedupeInput, canonicalYouTubeIdentity string) string {
	raw := buildRawEventKey(input, canonicalYouTubeIdentity)
	if len(raw) <= eventKeyMaxLength {
		return raw
	}
	sum := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("event_sha:%s", hex.EncodeToString(sum[:]))
}

func buildRawEventKey(input *DedupeInput, canonicalYouTubeIdentity string) string {
	if input.SourceKind == domain.AlarmDispatchSourceKindCelebration {
		return "celebration:" + input.SourceIdentity
	}
	if input.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		identity := resolveYouTubeSourceIdentity(input.SourceIdentity, canonicalYouTubeIdentity)
		return "youtube-outbox:" + string(input.SourceOutboxKind) + ":" + identity
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

func resolveYouTubeSourceIdentity(identity, canonicalIdentity string) string {
	if canonicalIdentity != "" && identity == canonicalIdentity {
		return identity
	}
	return boundedYouTubeSourceIdentity(identity)
}

func boundedYouTubeSourceIdentity(identity string) string {
	identity = strings.TrimSpace(identity)
	if isCanonicalSHA256Identity(identity) {
		return identity
	}
	sum := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func isCanonicalSHA256Identity(identity string) bool {
	const prefix = "sha256:"
	if len(identity) != len(prefix)+sha256.Size*2 || identity[:len(prefix)] != prefix {
		return false
	}
	for i := len(prefix); i < len(identity); i++ {
		char := identity[i]
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func EnvelopeDedupeInput(envelope *domain.AlarmQueueEnvelope) DedupeInput {
	return prepareEnvelopeDedupeInput(envelope).input
}

func prepareEnvelopeDedupeInput(envelope *domain.AlarmQueueEnvelope) preparedDedupeInput {
	input := envelopeNotificationDedupeInput(&envelope.Notification)
	applyCelebrationDedupeSource(&input, envelope)
	canonicalYouTubeIdentity := applyYouTubeOutboxDedupeSource(&input, envelope)
	return preparedDedupeInput{input: input, canonicalYouTubeIdentity: canonicalYouTubeIdentity}
}

func (input *preparedDedupeInput) eventKey() string {
	return buildEventKey(&input.input, input.canonicalYouTubeIdentity)
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

func applyYouTubeOutboxDedupeSource(input *DedupeInput, envelope *domain.AlarmQueueEnvelope) string {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		identity := envelope.YouTubeOutbox.Identity()
		input.SourceKind = envelope.SourceKind
		input.SourceIdentity = identity
		input.SourceOutboxKind = envelope.YouTubeOutbox.Kind
		input.ChannelID = strings.TrimSpace(envelope.YouTubeOutbox.ChannelID)
		input.AlarmType = envelope.YouTubeOutbox.AlarmType
		input.Category = string(envelope.SourceKind)
		return identity
	}
	return ""
}

func BuildDedupeKeyFromEnvelope(envelope *domain.AlarmQueueEnvelope) string {
	input := prepareEnvelopeDedupeInput(envelope)
	return buildDedupeKey(input.input.RoomID, input.eventKey())
}
