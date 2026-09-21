package content

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func refreshNotifications(session *reduceSession) {
	session.notifications = session.notifications[:0]

	for videoID, entity := range session.applied {
		state, ok := session.state.Videos[videoID]
		if !ok || !canNotifyNewContent(session, state.FirstPositiveEffectiveAt) {
			continue
		}

		session.notifications = append(session.notifications, NotificationIntent{
			Kind:      outboxKind(session.evidence.Kind),
			ChannelID: entity.ChannelID,
			ContentID: notificationContentID(session.evidence.Kind, entity.VideoID),
			Video:     entity,
		})
	}
}

func canNotifyNewContent(session *reduceSession, firstPositiveAt time.Time) bool {
	if session.evidence.Kind == contract.KindShortsList {
		// 알림 기준 목록은 전체 이력 수집이 아니라 앞선 유효 목록의 저장으로 확정됩니다.
		return session.state.Initialized
	}

	earliest := session.state.EarliestCompleteAt
	return earliest != nil && firstPositiveAt.After(*earliest)
}

func watermarkOf(state *State, evidence *Evidence) *domain.YouTubeContentWatermark {
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     state.ChannelID,
		WatermarkType: watermarkType(evidence.Kind),
		Initialized:   contentWindowInitialized(state, evidence),
		LastContentID: state.LastContentID,
	}
	if id := newestContentID(evidence); id != "" {
		watermark.LastContentID = id
	}

	return watermark
}

func contentWindowInitialized(state *State, evidence *Evidence) bool {
	if evidence.Kind != contract.KindShortsList {
		return true
	}

	// 부분 목록은 존재를 증명하지만 빈 부분 목록은 최초 기준 목록이 될 수 없습니다.
	return state.Initialized || len(evidence.Videos) > 0 || completeEligible(evidence)
}

func newestContentID(evidence *Evidence) string {
	var (
		chosen    string
		published *time.Time
	)

	for i := range evidence.Videos {
		video := evidence.Videos[i]

		if chosen == "" {
			chosen = video.VideoID
			published = video.PublishedAt

			continue
		}

		if newerPublished(video.PublishedAt, published, video.VideoID, chosen) {
			chosen = video.VideoID
			published = video.PublishedAt
		}
	}

	return chosen
}

func newerPublished(candidate, current *time.Time, candidateID, currentID string) bool {
	if candidate == nil {
		return current == nil && candidateID < currentID
	}

	if current == nil || candidate.After(*current) {
		return true
	}

	if candidate.Equal(*current) {
		return candidateID < currentID
	}

	return false
}

func absenceSlotOf(state *State, evidence *Evidence) *AbsenceSlot {
	if !scopedNegative(evidence) {
		return nil
	}

	for i := range state.AbsenceSlots {
		if state.AbsenceSlots[i].ScheduledFor.Equal(evidence.ScheduledFor) {
			slot := state.AbsenceSlots[i]
			return &slot
		}
	}

	return nil
}

func shortsTrackingOf(notifications []NotificationIntent) []NotificationIntent {
	tracking := make([]NotificationIntent, 0, len(notifications))
	for i := range notifications {
		if notifications[i].Kind == domain.OutboxKindNewShort {
			tracking = append(tracking, notifications[i])
		}
	}

	return tracking
}

func headApplication(state *State) []Application {
	if state.ChannelID == "" {
		return nil
	}

	return []Application{{
		EntityKind: "content_channel_head",
		EntityKey:  state.ChannelID,
		Decision:   "APPLIED",
	}}
}

func boundApplications(decision *Decision) Decision {
	if len(decision.Applications) <= 1000 {
		return *decision
	}

	decision.Applications = decision.Applications[:1000]

	return *decision
}
