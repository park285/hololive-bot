package dispatchrun

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type alarmDispatchGroup struct {
	roomID           string
	minutesUntil     int
	envelopes        []domain.AlarmQueueEnvelope
	notifications    []domain.AlarmNotification
	egressPath       alarmDispatchEgressPath
	egressErr        error
	egressRoomScoped bool
}

type regularChatResolver interface {
	RegularChat(ctx context.Context, roomID string) bool
}

type alarmDispatchRoomEligibilitySnapshot struct {
	rooms         regularChatResolver
	regularByRoom map[string]bool
}

func newAlarmDispatchRoomEligibilitySnapshot(rooms regularChatResolver) *alarmDispatchRoomEligibilitySnapshot {
	return &alarmDispatchRoomEligibilitySnapshot{
		rooms:         rooms,
		regularByRoom: make(map[string]bool),
	}
}

func (s *alarmDispatchRoomEligibilitySnapshot) RegularChat(ctx context.Context, roomID string) bool {
	if regular, ok := s.regularByRoom[roomID]; ok {
		return regular
	}

	regular := s.rooms != nil && s.rooms.RegularChat(ctx, roomID)

	s.regularByRoom[roomID] = regular

	return regular
}

func groupAlarmDispatchEnvelopesForDelivery(
	ctx context.Context,
	rooms regularChatResolver,
	envelopes []domain.AlarmQueueEnvelope,
) []alarmDispatchGroup {
	grouped := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}
	roomEligibility := newAlarmDispatchRoomEligibilitySnapshot(rooms)

	for i := range envelopes {
		envelope := &envelopes[i]
		path, roomScoped, pathErr := alarmDispatchEnvelopeEgressPath(ctx, roomEligibility, envelope)
		key := fmt.Sprintf("%d|%s", path, alarmDispatchRegroupKey(envelope, func(current *domain.AlarmQueueEnvelope) string {
			return alarmDispatchDeliveryGroupKey(current, path, pathErr)
		}))
		groupIndex, ok := index[key]

		if !ok {
			group := newAlarmDispatchGroup(envelope)

			group.egressPath = path
			group.egressErr = pathErr
			group.egressRoomScoped = roomScoped
			index[key] = len(grouped)
			grouped = append(grouped, group)

			continue
		}

		appendAlarmDispatchEnvelope(&grouped[groupIndex], envelope)

		grouped[groupIndex].egressRoomScoped = grouped[groupIndex].egressRoomScoped || roomScoped
	}

	split := make([]alarmDispatchGroup, 0, len(grouped))

	for i := range grouped {
		if grouped[i].egressErr != nil || grouped[i].egressPath != alarmDispatchEgressKaring {
			split = append(split, grouped[i])
			continue
		}

		split = append(split, splitAlarmDispatchKaringGroup(grouped[i])...)
	}

	return split
}

func alarmDispatchDeliveryGroupKey(
	envelope *domain.AlarmQueueEnvelope,
	path alarmDispatchEgressPath,
	pathErr error,
) string {
	if pathErr != nil || path == alarmDispatchEgressUnresolved {
		var outboxID int64

		if envelope != nil {
			outboxID = envelope.DispatchOutboxID
		}

		return fmt.Sprintf("%s|invalid|%d", alarmDispatchGroupKey(envelope), outboxID)
	}

	if path == alarmDispatchEgressText {
		return alarmDispatchGroupKey(envelope)
	}

	return alarmDispatchKaringGroupKey(envelope)
}

// 한 그룹이 여러 chunk로 나뉘면 앞 chunk만 전송된 뒤 뒤 chunk가 502로 실패하는 부분 성공이 가능하고,
// 그 실패는 not-admitted라 envelopeCount와 무관하게 재시도되어 전체를 retry-solo로 재그룹한다 —
// 이미 전송된 item이 다른 ClientRequestID로 다시 나간다. 그래서 chunk 경계에서 미리 잘라 둔다.
func splitAlarmDispatchKaringGroup(group alarmDispatchGroup) []alarmDispatchGroup {
	if len(group.envelopes) <= alarmDispatchKaringMaxItemsPerRequest ||
		len(group.envelopes) != len(group.notifications) {
		return []alarmDispatchGroup{group}
	}

	order := make([]int, len(group.envelopes))
	for i := range order {
		order[i] = i
	}

	// buildAlarmDispatchKaringContentListRequests와 같은 정렬이어야 분할 결과가 기존 chunk 경계와
	// 일치하고, 드레인 순서가 ClientRequestID에 새지 않는다.
	slices.SortStableFunc(order, func(left, right int) int {
		return cmp.Compare(
			alarmDispatchNotificationKaringItemIdentity(group, left),
			alarmDispatchNotificationKaringItemIdentity(group, right),
		)
	})

	groups := make([]alarmDispatchGroup, 0, (len(order)+alarmDispatchKaringMaxItemsPerRequest-1)/alarmDispatchKaringMaxItemsPerRequest)
	for start := 0; start < len(order); start += alarmDispatchKaringMaxItemsPerRequest {
		end := min(start+alarmDispatchKaringMaxItemsPerRequest, len(order))
		sub := alarmDispatchGroup{
			roomID:           group.roomID,
			minutesUntil:     group.minutesUntil,
			egressPath:       group.egressPath,
			egressErr:        group.egressErr,
			egressRoomScoped: group.egressRoomScoped,
		}

		for _, index := range order[start:end] {
			envelope := group.envelopes[index]

			sub.envelopes = append(sub.envelopes, envelope)
			sub.notifications = append(sub.notifications, group.notifications[index])
		}

		groups = append(groups, sub)
	}

	return groups
}

func groupAlarmDispatchEnvelopesByKey(
	envelopes []domain.AlarmQueueEnvelope,
	keyFunc func(*domain.AlarmQueueEnvelope) string,
) []alarmDispatchGroup {
	groups := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}

	for i := range envelopes {
		envelope := &envelopes[i]
		key := alarmDispatchRegroupKey(envelope, keyFunc)
		groupIndex, ok := index[key]

		if !ok {
			index[key] = len(groups)
			groups = append(groups, newAlarmDispatchGroup(envelope))

			continue
		}

		appendAlarmDispatchEnvelope(&groups[groupIndex], envelope)
	}

	return groups
}

// 재드레인 봉투가 신규 봉투와 병합되면 그룹 구성이 바뀌어 ClientRequestID가 다르게 재파생되고,
// 이미 admission된 첫 발송이 dedup에 접히지 않아 중복 발화한다 — 재시도 봉투는 항상 solo 그룹.
func alarmDispatchRegroupKey(envelope *domain.AlarmQueueEnvelope, keyFunc func(*domain.AlarmQueueEnvelope) string) string {
	if envelope != nil && envelope.SendUnitID > 0 {
		return fmt.Sprintf("send-unit|%d", envelope.SendUnitID)
	}

	if envelope != nil && envelope.Retry != nil && envelope.Retry.Attempt > 0 {
		return fmt.Sprintf("retry-solo|%d", envelope.DispatchOutboxID)
	}

	return keyFunc(envelope)
}

func newAlarmDispatchGroup(envelope *domain.AlarmQueueEnvelope) alarmDispatchGroup {
	if envelope == nil {
		return alarmDispatchGroup{}
	}

	return alarmDispatchGroup{
		roomID:        envelope.Notification.RoomID,
		minutesUntil:  envelope.Notification.MinutesUntil,
		envelopes:     []domain.AlarmQueueEnvelope{*envelope},
		notifications: []domain.AlarmNotification{envelope.Notification},
	}
}

func appendAlarmDispatchEnvelope(group *alarmDispatchGroup, envelope *domain.AlarmQueueEnvelope) {
	if group == nil || envelope == nil {
		return
	}

	group.minutesUntil = minAlarmDispatchMinutes(group.minutesUntil, envelope.Notification.MinutesUntil)
	group.envelopes = append(group.envelopes, *envelope)
	group.notifications = append(group.notifications, envelope.Notification)
}

func alarmDispatchGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	if envelope == nil {
		return ""
	}

	if key, ok := alarmDispatchSourceGroupKey(envelope); ok {
		return key
	}

	return alarmDispatchTimeGroupKey(envelope)
}

func alarmDispatchSourceGroupKey(envelope *domain.AlarmQueueEnvelope) (string, bool) {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		return alarmDispatchCelebrationGroupKey(envelope), true
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return fmt.Sprintf("%s|source|%s|%s|%s|%s",
			envelope.Notification.RoomID,
			envelope.SourceKind,
			envelope.YouTubeOutbox.ChannelID,
			envelope.YouTubeOutbox.Kind,
			envelope.YouTubeOutbox.Identity(),
		), true
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest && envelope.DeliveryDigest != nil {
		return fmt.Sprintf("%s|source|%s|%s|%s",
			envelope.Notification.RoomID,
			envelope.SourceKind,
			envelope.DeliveryDigest.Kind,
			envelope.DeliveryDigest.ContentIdentity(),
		), true
	}

	return "", false
}

func alarmDispatchCelebrationGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	memberIdentity := envelope.Celebration.ChannelID
	if envelope.Celebration.MemberID > 0 {
		memberIdentity = fmt.Sprintf("member-%d", envelope.Celebration.MemberID)
	}

	key := fmt.Sprintf("%s|celebration|%s|%s",
		envelope.Notification.RoomID,
		envelope.Celebration.Kind,
		memberIdentity,
	)
	if envelope.Celebration.VideoID != "" {
		key += "|" + envelope.Celebration.VideoID
	}

	return key
}

func alarmDispatchTimeGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	if envelope.Notification.Stream != nil && envelope.Notification.Stream.StartScheduled != nil {
		minuteBucket := envelope.Notification.Stream.StartScheduled.UTC().Unix() / 60
		return fmt.Sprintf("%s|scheduled|%d", envelope.Notification.RoomID, minuteBucket)
	}

	return fmt.Sprintf("%s|minutes|%d", envelope.Notification.RoomID, envelope.Notification.MinutesUntil)
}

func alarmDispatchKaringGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	if envelope == nil {
		return ""
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		return alarmDispatchGroupKey(envelope)
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return alarmDispatchGroupKey(envelope)
	}

	if envelope.SourceKind == domain.AlarmDispatchSourceKindDeliveryDigest && envelope.DeliveryDigest != nil {
		return alarmDispatchGroupKey(envelope)
	}

	phase := "prelive"

	if envelope.Notification.IsStarting() {
		phase = "starting"
	}

	return fmt.Sprintf(
		"%s|karing|%s|%s|minutes|%d",
		envelope.Notification.RoomID,
		envelope.Notification.AlarmType,
		phase,
		envelope.Notification.MinutesUntil,
	)
}

type alarmDispatchEgressPath uint8

const (
	alarmDispatchEgressUnresolved alarmDispatchEgressPath = iota
	alarmDispatchEgressKaring
	alarmDispatchEgressText
)

func alarmDispatchEnvelopeEgressPath(
	ctx context.Context,
	rooms regularChatResolver,
	envelope *domain.AlarmQueueEnvelope,
) (alarmDispatchEgressPath, bool, error) {
	if envelope == nil {
		return alarmDispatchEgressUnresolved, false, errors.New("alarm dispatch envelope is nil")
	}

	switch envelope.SourceKind {
	case domain.AlarmDispatchSourceKindCelebration, domain.AlarmDispatchSourceKindDeliveryDigest:
		return alarmDispatchEgressText, false, nil
	case domain.AlarmDispatchSourceKindYouTubeOutbox:
		return alarmDispatchYouTubeOutboxEgressPath(ctx, rooms, envelope)
	case "":
		return alarmDispatchStreamEgressPath(ctx, rooms, envelope)
	default:
		return alarmDispatchEgressUnresolved, false, fmt.Errorf("alarm dispatch source kind %q has no egress path", envelope.SourceKind)
	}
}

func alarmDispatchYouTubeOutboxEgressPath(
	ctx context.Context,
	rooms regularChatResolver,
	envelope *domain.AlarmQueueEnvelope,
) (alarmDispatchEgressPath, bool, error) {
	if envelope.YouTubeOutbox == nil {
		return alarmDispatchEgressUnresolved, false, errors.New("youtube outbox dispatch payload is nil")
	}

	switch envelope.YouTubeOutbox.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream, domain.OutboxKindCommunityPost:
		return alarmDispatchRoomEgressPath(ctx, rooms, envelope.Notification.RoomID), true, nil
	case domain.OutboxKindMilestone:
		return alarmDispatchEgressText, false, nil
	default:
		return alarmDispatchEgressUnresolved, false, fmt.Errorf("youtube outbox kind %q has no egress path", envelope.YouTubeOutbox.Kind)
	}
}

func alarmDispatchStreamEgressPath(
	ctx context.Context,
	rooms regularChatResolver,
	envelope *domain.AlarmQueueEnvelope,
) (alarmDispatchEgressPath, bool, error) {
	stream := envelope.Notification.Stream
	if stream == nil {
		return alarmDispatchEgressUnresolved, false, errors.New("alarm notification stream is nil")
	}

	if stream.IsTwitchOnly || stream.IsChzzkOnly {
		return alarmDispatchEgressText, false, nil
	}

	if !stream.HasYouTubeInfo() {
		return alarmDispatchEgressUnresolved, false, errors.New("alarm notification has no YouTube target")
	}

	return alarmDispatchRoomEgressPath(ctx, rooms, envelope.Notification.RoomID), true, nil
}

func alarmDispatchRoomEgressPath(ctx context.Context, rooms regularChatResolver, roomID string) alarmDispatchEgressPath {
	if rooms != nil && rooms.RegularChat(ctx, roomID) {
		return alarmDispatchEgressKaring
	}

	return alarmDispatchEgressText
}

func alarmDispatchGroupEgressPath(group alarmDispatchGroup) (alarmDispatchEgressPath, error) {
	if group.egressErr != nil {
		return group.egressPath, group.egressErr
	}

	if len(group.envelopes) == 0 {
		return alarmDispatchEgressUnresolved, errors.New("alarm dispatch group is empty")
	}

	switch group.egressPath {
	case alarmDispatchEgressKaring, alarmDispatchEgressText:
		return group.egressPath, nil
	case alarmDispatchEgressUnresolved:
		return alarmDispatchEgressUnresolved, errors.New("alarm dispatch group egress path is unresolved")
	default:
		return alarmDispatchEgressUnresolved, errors.New("alarm dispatch group egress path is unresolved")
	}
}

func minAlarmDispatchMinutes(current, next int) int {
	if next < 0 {
		return current
	}

	if current < 0 || next < current {
		return next
	}

	return current
}
