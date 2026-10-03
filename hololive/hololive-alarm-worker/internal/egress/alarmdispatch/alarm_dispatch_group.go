package alarmdispatch

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type alarmDispatchGroup struct {
	request       *dispatchoutbox.SendRequest
	roomID        string
	minutesUntil  int
	envelopes     []domain.AlarmQueueEnvelope
	notifications []domain.AlarmNotification
	// envelopeErr는 발송할 수 없는 봉투(퇴역 제공자, 대상 없음, 미지원 source)의 사유다. 이런 봉투는 발송 전 실패로 라우팅한다.
	envelopeErr error
}

// alarm dispatch는 모든 봉투를 Text 경로(오픈채팅은 sender가 Markdown으로 보냄)로 보낸다. 방 유형에 따라 Karing template을
// 고르던 분기와 Karing chunk 분할은 DEC-20260926-hololive-karing-egress-disposition에 따라 삭제했다.
func groupAlarmDispatchEnvelopesForDelivery(envelopes []domain.AlarmQueueEnvelope) []alarmDispatchGroup {
	grouped := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}

	for i := range envelopes {
		envelope := &envelopes[i]
		envelopeErr := alarmDispatchEnvelopeError(envelope)
		key := alarmDispatchRegroupKey(envelope, func(current *domain.AlarmQueueEnvelope) string {
			return alarmDispatchDeliveryGroupKey(current, envelopeErr)
		})

		// 발송할 수 없는 봉투는 같은 send unit의 발송 가능한 봉투와 섞이지 않게 별도 그룹으로 둔다.
		if envelopeErr != nil {
			key = "invalid|" + key
		}

		groupIndex, ok := index[key]
		if !ok {
			group := newAlarmDispatchGroup(envelope)

			group.envelopeErr = envelopeErr
			index[key] = len(grouped)
			grouped = append(grouped, group)

			continue
		}

		appendAlarmDispatchEnvelope(&grouped[groupIndex], envelope)
	}

	return grouped
}

func alarmDispatchDeliveryGroupKey(envelope *domain.AlarmQueueEnvelope, envelopeErr error) string {
	if envelopeErr != nil {
		var outboxID int64

		if envelope != nil {
			outboxID = envelope.DispatchOutboxID
		}

		return fmt.Sprintf("%s|invalid|%d", alarmDispatchGroupKey(envelope), outboxID)
	}

	return alarmDispatchGroupKey(envelope)
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

// 저장된 send unit이 발송 경계다. 저장된 send unit이 없는 봉투는 keyFunc로 묶이지만 Text 경로는 저장된 client_request_id가
// 없으면 발송하지 않는다(errAlarmDispatchSendUnitIdentityMissing). 파생 ID를 고정하려고 재시도 봉투를 solo로 떼던
// 분기는 파생 ID와 함께 지웠다(stack-audit 2026-09-26 T17).
func alarmDispatchRegroupKey(envelope *domain.AlarmQueueEnvelope, keyFunc func(*domain.AlarmQueueEnvelope) string) string {
	if envelope != nil && envelope.SendUnitID > 0 {
		return fmt.Sprintf("send-unit|%d", envelope.SendUnitID)
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
	if envelope.SourceKind == domain.AlarmDispatchSourceKindXSpace && envelope.XSpace != nil {
		return fmt.Sprintf("%s|source|x_space|%s", envelope.Notification.RoomID, envelope.XSpace.SpaceID), true
	}

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
	key := fmt.Sprintf("%s|celebration|%s|member-%d",
		envelope.Notification.RoomID,
		envelope.Celebration.Kind,
		envelope.Celebration.MemberID,
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

func alarmDispatchEnvelopeError(envelope *domain.AlarmQueueEnvelope) error {
	if envelope == nil {
		return errors.New("alarm dispatch envelope is nil")
	}

	switch envelope.SourceKind {
	case domain.AlarmDispatchSourceKindCelebration, domain.AlarmDispatchSourceKindDeliveryDigest, domain.AlarmDispatchSourceKindXSpace:
		return nil
	case domain.AlarmDispatchSourceKindYouTubeOutbox:
		return alarmDispatchYouTubeOutboxEnvelopeError(envelope)
	case "":
		return alarmDispatchStreamEnvelopeError(envelope)
	default:
		return fmt.Errorf("alarm dispatch source kind %q has no egress path", envelope.SourceKind)
	}
}

func alarmDispatchYouTubeOutboxEnvelopeError(envelope *domain.AlarmQueueEnvelope) error {
	if envelope.YouTubeOutbox == nil {
		return errors.New("youtube outbox dispatch payload is nil")
	}

	switch envelope.YouTubeOutbox.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream, domain.OutboxKindCommunityPost:
		return nil
	default:
		return fmt.Errorf("youtube outbox kind %q has no egress path", envelope.YouTubeOutbox.Kind)
	}
}

// errAlarmDispatchRetiredStreamProvider는 Twitch/Chzzk 단독 방송을 담은 보관 v3 envelope의 드레인 종단이다.
// DEC-20260926-youtube-only-stream-providers(e65119bc1)로 비유튜브 제공자를 퇴역했고, 이 봉투는 발송하지 않고 기존
// 발송 전 실패 경로(재시도 한도 뒤 DLQ)로 보낸다. 렌더러의 같은 분기는 이 가드 뒤라 도달하지 않아 지웠다(stack-audit
// 2026-09-26 T17). 드레인 표시는 runner의 error 로그와 DLQ last_error다.
// 제거 조건: authoritative DB에서 alarm_dispatch_events.payload의 notification.stream.is_twitch_only 또는
// is_chzzk_only가 true이면서 pending·retry·leased·sending delivery가 있는 event가 0건이고, 해당 event가 dispatch
// retention(기본 90일)으로 소멸한 뒤 이 가드, domain.Stream의 Twitch/Chzzk 필드(외부 Stream API 계약 확인 포함)를 지운다.
// 재검토 기한: remove_after = "2026-12-31".
var errAlarmDispatchRetiredStreamProvider = errors.New("non-YouTube stream provider is retired")

func alarmDispatchStreamEnvelopeError(envelope *domain.AlarmQueueEnvelope) error {
	stream := envelope.Notification.Stream
	if stream == nil {
		return errors.New("alarm notification stream is nil")
	}

	// 보관된 구형 envelope를 YouTube 알림으로 오인해 발송하지 않는다(드레인 종단).
	if stream.IsTwitchOnly || stream.IsChzzkOnly {
		return errAlarmDispatchRetiredStreamProvider
	}

	if !stream.HasYouTubeInfo() {
		return errors.New("alarm notification has no YouTube target")
	}

	return nil
}

func alarmDispatchGroupError(group alarmDispatchGroup) error {
	if group.envelopeErr != nil {
		return group.envelopeErr
	}

	if len(group.envelopes) == 0 {
		return errors.New("alarm dispatch group is empty")
	}

	return nil
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
