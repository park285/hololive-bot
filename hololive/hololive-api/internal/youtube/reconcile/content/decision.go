package content

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func refreshNotifications(session *reduceSession) {
	session.notifications = session.notifications[:0]

	candidates := session.novel
	if session.evidence.Kind == contract.KindShortsList {
		candidates = session.applied
		if !session.state.Initialized {
			candidates = nil
		}
	}

	for videoID, entity := range candidates {
		if _, ok := session.state.Videos[videoID]; !ok {
			continue
		}

		if entity.IsPremiere != nil && *entity.IsPremiere {
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
