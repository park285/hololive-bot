package dispatchoutbox

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const maxDeliveriesPerSendUnit = 10

func BuildDispatchGroupKeyFromEnvelope(envelope *domain.AlarmQueueEnvelope) string {
	if envelope == nil {
		return ""
	}

	parts := dispatchGroupKeyParts(envelope)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return "alarm-group-v1:" + hex.EncodeToString(sum[:])
}

func dispatchGroupKeyParts(envelope *domain.AlarmQueueEnvelope) []string {
	sourceParts := dispatchGroupSourceParts(envelope)
	parts := make([]string, 0, len(sourceParts)+2)

	parts = append(parts,
		"alarm-dispatch-group-v1",
		strings.TrimSpace(envelope.Notification.RoomID),
	)

	return append(parts, sourceParts...)
}

func dispatchGroupSourceParts(envelope *domain.AlarmQueueEnvelope) []string {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		return []string{"celebration", string(envelope.Celebration.Kind), envelope.Celebration.ChannelID, envelope.Celebration.VideoID}
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return []string{"youtube-outbox", string(envelope.YouTubeOutbox.Kind), envelope.YouTubeOutbox.ChannelID, envelope.YouTubeOutbox.Identity()}
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest && envelope.DeliveryDigest != nil {
		return []string{"delivery-digest", string(envelope.DeliveryDigest.Kind), envelope.DeliveryDigest.ContentIdentity()}
	}

	return notificationDispatchGroupParts(&envelope.Notification)
}

func notificationDispatchGroupParts(notification *domain.AlarmNotification) []string {
	phase := "prelive"

	if notification.IsStarting() {
		phase = "starting"
	}

	return []string{"notification", string(notification.AlarmType), phase, strconv.Itoa(notification.MinutesUntil)}
}

func assignSendUnits(deliveries []deliveryInsert) {
	grouped := make(map[string][]int, len(deliveries))
	for i := range deliveries {
		grouped[deliveries[i].DispatchGroupKey] = append(grouped[deliveries[i].DispatchGroupKey], i)
	}

	groupKeys := make([]string, 0, len(grouped))
	for key := range grouped {
		groupKeys = append(groupKeys, key)
	}

	slices.Sort(groupKeys)

	for _, groupKey := range groupKeys {
		indices := grouped[groupKey]
		slices.SortFunc(indices, func(left, right int) int {
			return cmp.Compare(deliveries[left].DedupeKey, deliveries[right].DedupeKey)
		})

		for start := 0; start < len(indices); start += maxDeliveriesPerSendUnit {
			end := min(start+maxDeliveriesPerSendUnit, len(indices))
			assignSendUnitChunk(deliveries, groupKey, indices[start:end])
		}
	}
}

func assignSendUnitChunk(deliveries []deliveryInsert, groupKey string, indices []int) {
	parts := make([]string, 0, len(indices)+2)

	parts = append(parts, "alarm-send-unit-v1", groupKey)

	for _, index := range indices {
		parts = append(parts, deliveries[index].DedupeKey)
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	unitKey := hex.EncodeToString(sum[:])
	clientRequestID := fmt.Sprintf("hololive-alarm:%s", hex.EncodeToString(sum[:16]))

	for _, index := range indices {
		deliveries[index].SendUnitKey = unitKey
		deliveries[index].ClientRequestID = clientRequestID
	}
}
