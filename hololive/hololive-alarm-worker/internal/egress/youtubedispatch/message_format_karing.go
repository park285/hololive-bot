package youtubedispatch

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// KaringMaxItemsPerRequest는 현행 Karing card template의 한 요청당 item 상한이다.
const KaringMaxItemsPerRequest = 4

// 한 plan은 정확히 한 provider 호출과 그 호출이 소유하는 outbox subset이다.
// Ordinal은 첫 persisted outbox ID이므로 앞 chunk가 완료된 뒤 재계획해도 변하지 않는다.
type karingDeliveryChunk struct {
	outboxes        []domain.YouTubeNotificationOutbox
	payload         domain.YouTubeOutboxDispatchPayload
	clientRequestID string
}

func (mf *MessageFormatter) planKaringChunks(ctx context.Context, roomID, channelID string, kind domain.OutboxKind, outboxes []domain.YouTubeNotificationOutbox) ([]karingDeliveryChunk, error) {
	ordered := slices.Clone(outboxes)
	slices.SortFunc(ordered, func(a, b domain.YouTubeNotificationOutbox) int { return cmp.Compare(a.ID, b.ID) })

	fullPayload, err := mf.buildYouTubeOutboxKaringPayload(ctx, channelID, kind, ordered)
	if err != nil {
		return nil, err
	}

	chunks := make([]karingDeliveryChunk, 0, (len(ordered)+KaringMaxItemsPerRequest-1)/KaringMaxItemsPerRequest)
	for start := 0; start < len(ordered); start += KaringMaxItemsPerRequest {
		end := min(start+KaringMaxItemsPerRequest, len(ordered))
		part := ordered[start:end]
		payload := fullPayload

		payload.OutboxIDs = fullPayload.OutboxIDs[start:end]
		payload.Items = fullPayload.Items[start:end]

		// 표시용 member cache 변화는 같은 durable chunk의 멱등성 ID를 바꾸지 않는다.
		canonical, err := json.Marshal(struct {
			Room      string
			Ordinal   int64
			Kind      domain.OutboxKind
			ChannelID string
			Items     []domain.YouTubeOutboxItem
		}{roomID, part[0].ID, kind, channelID, payload.Items})
		if err != nil {
			return nil, fmt.Errorf("encode karing chunk identity: %w", err)
		}

		sum := sha256.Sum256(append([]byte("youtube-outbox-karing-chunk-v1\x00"), canonical...))

		chunks = append(chunks, karingDeliveryChunk{
			outboxes: part, payload: payload,
			clientRequestID: "hololive-alarm:" + hex.EncodeToString(sum[:16]),
		})
	}

	return chunks, nil
}

func (mf *MessageFormatter) buildYouTubeOutboxKaringPayload(
	ctx context.Context,
	channelID string,
	kind domain.OutboxKind,
	outboxes []domain.YouTubeNotificationOutbox,
) (domain.YouTubeOutboxDispatchPayload, error) {
	memberName, err := mf.getMemberName(ctx, channelID)
	if err != nil || strings.TrimSpace(memberName) == "" {
		memberName = mf.vtuberFallback(ctx)
	}

	payload := domain.YouTubeOutboxDispatchPayload{
		OutboxIDs:  make([]int64, 0, len(outboxes)),
		Kind:       kind,
		AlarmType:  kind.ToAlarmType(),
		ChannelID:  channelID,
		MemberName: strings.TrimSpace(memberName),
		Items:      make([]domain.YouTubeOutboxItem, 0, len(outboxes)),
	}
	for i := range outboxes {
		payload.OutboxIDs = append(payload.OutboxIDs, outboxes[i].ID)
		payload.Items = append(payload.Items, domain.YouTubeOutboxItem{
			OutboxID:  outboxes[i].ID,
			ContentID: outboxes[i].ContentID,
			Payload:   outboxes[i].Payload,
		})
	}

	if err := payload.Validate(); err != nil {
		return domain.YouTubeOutboxDispatchPayload{}, fmt.Errorf("build youtube outbox karing payload: %w", err)
	}

	return payload, nil
}
