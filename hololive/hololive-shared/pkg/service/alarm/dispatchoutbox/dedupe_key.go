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
	SourceOutboxKind            domain.OutboxKind
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

	// YouTube source identity는 domain.YouTubeOutboxDispatchPayload.CanonicalIdentity(sha256:<64hex>)다. 저장 경로는 envelope
	// 검증 뒤 payload에서 이 값을 다시 계산하고 validateSourceIdentity로 형식을 확인한다. 비정규(legacy raw) 식별자를 해시로
	// 감싸 받던 분기는 지웠다(stack-audit 2026-09-26 T11 holo-dispatch-raw-youtube-identity-hash). 정규 식별자의 key는
	// 바뀌지 않는다.
	if input.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return "youtube-outbox:" + string(input.SourceOutboxKind) + ":" + input.SourceIdentity
	}

	if input.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest {
		return "delivery-digest:" + input.SourceIdentity
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

// validateSourceIdentity는 저장할 key의 YouTube outbox source identity가 CanonicalIdentity 형식(sha256:<64 lowercase hex>)인지
// 확인한다. 예전에는 비정규 식별자를 해시로 감싸 받았지만, 이제 저장 전에 오류로 거절해 서로 다른 잘못된 입력이
// 같은 key로 겹치거나 raw 식별자가 key에 들어가지 않게 한다. 식별자 값은 오류에 넣지 않는다.
func validateSourceIdentity(input *DedupeInput) error {
	if input.SourceKind != domain.AlarmDispatchSourceKindYouTubeOutbox {
		return nil
	}

	if !isCanonicalSHA256Identity(input.SourceIdentity) {
		return fmt.Errorf("youtube outbox source identity is not canonical sha256:<64 lowercase hex> (%d bytes)", len(input.SourceIdentity))
	}

	return nil
}

func isCanonicalSHA256Identity(identity string) bool {
	const prefix = "sha256:"

	if len(identity) != len(prefix)+sha256.Size*2 || !strings.HasPrefix(identity, prefix) {
		return false
	}

	for _, char := range identity[len(prefix):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}

	return true
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

	applyYouTubeOutboxDedupeSource(&input, envelope)
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
