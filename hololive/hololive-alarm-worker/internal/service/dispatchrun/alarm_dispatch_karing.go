package dispatchrun

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	alarmDispatchKaringMaxItemsPerRequest = 4
	// Iris admission dedup의 canonical idempotency namespace다. 재시도 identity bytes를 유지한다.
	alarmDispatchClientRequestIDNamespace = "hololive-alarm:"
)

var alarmDispatchKaringDisplayLocation = util.KSTZone

var alarmDispatchKaringTemplateIDByItemCount = map[int]int64{
	1: 133266,
	2: 133223,
	3: 133222,
	4: 133267,
}

type alarmDispatchKaringItem struct {
	identity string
	item     iris.KaringContentItem
}

func buildAlarmDispatchKaringContentListRequests(ctx context.Context, messageStrings *messagestrings.Store, group alarmDispatchGroup) ([]iris.KaringContentListRequest, error) {
	entries, err := buildAlarmDispatchKaringItems(ctx, messageStrings, group)
	if err != nil {
		return nil, fmt.Errorf("build alarm dispatch karing items: %w", err)
	}

	if len(entries) == 0 {
		return nil, errors.New("build alarm dispatch karing content list request: items are empty")
	}

	// 재스케줄로 그룹 구성이 바뀌어도 동일 item 조합이면 동일 ClientRequestID가 재생산되어야
	// admission dedup이 기전송 chunk를 걸러낸다 — identity 정렬로 chunk 경계를 고정한다.
	slices.SortStableFunc(entries, func(left, right alarmDispatchKaringItem) int {
		return cmp.Compare(left.identity, right.identity)
	})

	requests := make([]iris.KaringContentListRequest, 0, (len(entries)+alarmDispatchKaringMaxItemsPerRequest-1)/alarmDispatchKaringMaxItemsPerRequest)
	for start := 0; start < len(entries); start += alarmDispatchKaringMaxItemsPerRequest {
		end := min(start+alarmDispatchKaringMaxItemsPerRequest, len(entries))
		chunk := entries[start:end]
		items := make([]iris.KaringContentItem, 0, len(chunk))
		identities := make([]string, 0, len(chunk))

		for i := range chunk {
			items = append(items, chunk[i].item)
			identities = append(identities, chunk[i].identity)
		}

		clientRequestID := alarmDispatchKaringChunkClientRequestID(group.roomID, identities)
		if persisted := persistedAlarmDispatchClientRequestID(group); persisted != "" {
			clientRequestID = alarmDispatchPersistedKaringChunkClientRequestID(persisted, identities)
		}

		req := iris.KaringContentListRequest{
			ClientRequestID: new(clientRequestID),
			Items:           items,
			ExtraArgs:       buildAlarmDispatchKaringExtraArgs(ctx, messageStrings, group, len(chunk)),
			TemplateID:      alarmDispatchKaringTemplateID(len(chunk)),
		}
		applyAlarmDispatchKaringReceiver(&req, group.roomID)

		requests = append(requests, req)
	}

	return requests, nil
}

func alarmDispatchPersistedKaringChunkClientRequestID(persisted string, identities []string) string {
	parts := make([]string, 0, 2+len(identities))

	parts = append(parts, "alarm-dispatch-karing-persisted-v1", strings.TrimSpace(persisted))
	parts = append(parts, identities...)

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return alarmDispatchClientRequestIDNamespace + hex.EncodeToString(sum[:16])
}

func alarmDispatchKaringChunkClientRequestID(roomID string, identities []string) string {
	parts := make([]string, 0, 2+len(identities))

	parts = append(parts, "alarm-dispatch-karing-v2", strings.TrimSpace(roomID))
	parts = append(parts, identities...)

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return alarmDispatchClientRequestIDNamespace + hex.EncodeToString(sum[:16])
}

func alarmDispatchClientRequestID(group alarmDispatchGroup, start, end int) string {
	if persisted := persistedAlarmDispatchClientRequestID(group); persisted != "" {
		return persisted
	}

	parts := make([]string, 0, 4+len(group.envelopes)*8)

	parts = append(parts,
		"alarm-dispatch-v1",
		strings.TrimSpace(group.roomID),
		strconv.Itoa(start),
		strconv.Itoa(end),
	)

	for i := range group.envelopes {
		parts = append(parts, alarmDispatchEnvelopeClientRequestIDParts(&group.envelopes[i])...)
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return alarmDispatchClientRequestIDNamespace + hex.EncodeToString(sum[:16])
}

func persistedAlarmDispatchClientRequestID(group alarmDispatchGroup) string {
	if len(group.envelopes) > alarmDispatchKaringMaxItemsPerRequest {
		return ""
	}

	return persistedAlarmDispatchClientRequestIDFromEnvelopes(group.envelopes)
}

func persistedAlarmDispatchClientRequestIDFromEnvelopes(envelopes []domain.AlarmQueueEnvelope) string {
	persisted := ""

	for i := range envelopes {
		candidate := strings.TrimSpace(envelopes[i].ClientRequestID)
		if candidate == "" || (persisted != "" && candidate != persisted) {
			return ""
		}

		persisted = candidate
	}

	return persisted
}

func alarmDispatchEnvelopeClientRequestIDParts(envelope *domain.AlarmQueueEnvelope) []string {
	if envelope == nil {
		return nil
	}

	parts := make([]string, 0, 6+len(envelope.ClaimKeys))

	parts = append(parts,
		strconv.FormatInt(envelope.DispatchOutboxID, 10),
		string(envelope.SourceKind),
		string(envelope.Notification.AlarmType),
		strconv.Itoa(envelope.Notification.MinutesUntil),
	)
	parts = append(parts, envelope.ClaimKeys...)

	if envelope.YouTubeOutbox != nil {
		parts = append(parts, envelope.YouTubeOutbox.Identity())
		for _, item := range envelope.YouTubeOutbox.Items {
			parts = append(parts, strconv.FormatInt(item.OutboxID, 10), item.ContentID)
		}
	}

	return parts
}

func applyAlarmDispatchKaringReceiver(req *iris.KaringContentListRequest, roomID string) {
	if req == nil {
		return
	}

	trimmed := strings.TrimSpace(roomID)
	if receiverRoomID, err := strconv.ParseInt(trimmed, 10, 64); err == nil && receiverRoomID > 0 {
		req.ReceiverRoomID = receiverRoomID
		return
	}

	req.ReceiverName = trimmed
}

func alarmDispatchKaringTemplateID(itemCount int) int64 {
	return alarmDispatchKaringTemplateIDByItemCount[itemCount]
}

func buildAlarmDispatchKaringItems(ctx context.Context, messageStrings *messagestrings.Store, group alarmDispatchGroup) ([]alarmDispatchKaringItem, error) {
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		out, err := buildAlarmDispatchYouTubeOutboxKaringItems(ctx, messageStrings, &group.envelopes[0])
		if err != nil {
			return out, fmt.Errorf("build alarm dispatch youtube outbox karing items: %w", err)
		}

		return out, nil
	}

	entries := make([]alarmDispatchKaringItem, 0, len(group.notifications))
	for i := range group.notifications {
		entries = append(entries, alarmDispatchKaringItem{
			identity: alarmDispatchNotificationKaringItemIdentity(group, i),
			item:     buildAlarmDispatchNotificationKaringContentItem(ctx, messageStrings, &group.notifications[i]),
		})
	}

	return entries, nil
}

func alarmDispatchNotificationKaringItemIdentity(group alarmDispatchGroup, index int) string {
	var dispatchOutboxID int64

	if index < len(group.envelopes) {
		dispatchOutboxID = group.envelopes[index].DispatchOutboxID
	}

	streamID := ""

	if group.notifications[index].Stream != nil {
		streamID = group.notifications[index].Stream.ID
	}

	return fmt.Sprintf("%020d|%s", dispatchOutboxID, streamID)
}

func alarmDispatchOutboxKaringItemIdentity(item *domain.YouTubeOutboxItem) string {
	return fmt.Sprintf("%020d|%s", item.OutboxID, item.ContentID)
}

func buildAlarmDispatchNotificationKaringContentItem(ctx context.Context, store *messagestrings.Store, notification *domain.AlarmNotification) iris.KaringContentItem {
	if notification == nil {
		return iris.KaringContentItem{}
	}

	memberName := resolveAlarmDispatchMemberName(ctx, store, notification)

	return iris.KaringContentItem{
		Title:        resolveAlarmDispatchTitle(ctx, store, notification),
		URL:          resolveAlarmDispatchKaringURL(notification),
		MemberName:   memberName,
		ChannelName:  resolveAlarmDispatchKaringChannelName(notification, memberName),
		Status:       "",
		StartAt:      resolveAlarmDispatchKaringStartAt(notification.Stream),
		ThumbnailURL: resolveAlarmDispatchKaringThumbnailURL(notification),
		Platform:     resolveAlarmDispatchKaringPlatform(notification.Stream),
	}
}

func buildAlarmDispatchKaringExtraArgs(ctx context.Context, store *messagestrings.Store, group alarmDispatchGroup, itemCount int) iris.KaringTemplateArgs {
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return buildAlarmDispatchOutboxKaringExtraArgs(ctx, store, &group.envelopes[0], itemCount)
	}

	args := iris.KaringTemplateArgs{}
	allPremiere := alarmDispatchGroupAllPremiere(group)

	if alarmDispatchGroupAllStarting(group) {
		if allPremiere {
			args["alarm_title"] = store.GetOrContext(ctx, messagestrings.NamespaceKaring, "alarm_title_live_premiere", "선행공개 시작")
		} else {
			args["alarm_title"] = store.GetOrContext(ctx, messagestrings.NamespaceKaring, "alarm_title_live", "라이브 시작")
		}

		args["time_left"] = store.GetOrContext(ctx, messagestrings.NamespaceKaring, "time_left_live", "지금 시작")

		return args
	}

	if group.minutesUntil > 0 {
		if allPremiere {
			args["alarm_title"] = fmt.Sprintf(store.GetOrContext(ctx, messagestrings.NamespaceKaring, "alarm_title_prelive_premiere", "선행공개 %d분 전 알림"), group.minutesUntil)
		} else {
			args["alarm_title"] = fmt.Sprintf(store.GetOrContext(ctx, messagestrings.NamespaceKaring, "alarm_title_prelive", "방송 %d분 전 알림"), group.minutesUntil)
		}

		args["time_left"] = fmt.Sprintf(store.GetOrContext(ctx, messagestrings.NamespaceKaring, "time_left_prelive", "%d분 후 시작"), group.minutesUntil)

		return args
	}

	args["alarm_title"] = store.GetOrContext(ctx, messagestrings.NamespaceKaring, "alarm_title_live", "라이브 시작")
	args["time_left"] = store.GetOrContext(ctx, messagestrings.NamespaceKaring, "time_left_live", "지금 시작")

	return args
}

func resolveAlarmDispatchKaringChannelName(notification *domain.AlarmNotification, fallback string) string {
	if notification != nil && notification.Stream != nil && strings.TrimSpace(notification.Stream.ChannelName) != "" {
		return strings.TrimSpace(notification.Stream.ChannelName)
	}

	return fallback
}

func resolveAlarmDispatchKaringStartAt(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	if stream.StartActual != nil {
		return alarmDispatchKaringTimeString(stream.StartActual)
	}

	if stream.StartScheduled != nil {
		return alarmDispatchKaringTimeString(stream.StartScheduled)
	}

	return ""
}

func resolveAlarmDispatchKaringThumbnailURL(notification *domain.AlarmNotification) string {
	if notification == nil {
		return ""
	}

	if notification.Stream != nil && notification.Stream.Thumbnail != nil {
		if url := normalizeKaringImageURL(*notification.Stream.Thumbnail); url != "" {
			return url
		}
	}

	if notification.Channel != nil {
		return normalizeKaringImageURL(notification.Channel.GetPhotoURL())
	}

	return ""
}

func resolveAlarmDispatchKaringPlatform(stream *domain.Stream) string {
	if stream == nil {
		return "youtube"
	}

	if stream.IsTwitchOnly {
		return "twitch"
	}

	if stream.IsChzzkOnly {
		return "chzzk"
	}

	return "youtube"
}
