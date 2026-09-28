package celebration

import (
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/util"
)

func celebrationEventKey(payload *domain.CelebrationDispatchPayload) string {
	return dispatchoutbox.BuildEventKey(&dispatchoutbox.DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: payload.Identity(),
	})
}

func birthdayGreetingEventKey(memberID int, dateStr string) string {
	return celebrationEventKey(&domain.CelebrationDispatchPayload{
		Kind:     domain.CelebrationKindBirthday,
		MemberID: memberID,
		Date:     dateStr,
	})
}

func birthdayStreamEventKey(memberID int, dateStr, videoID string) string {
	return celebrationEventKey(&domain.CelebrationDispatchPayload{
		Kind:     domain.CelebrationKindBirthdayStream,
		MemberID: memberID,
		Date:     dateStr,
		VideoID:  videoID,
	})
}

func birthdayStreamEventKeyPrefix(memberID int, dateStr string) string {
	return birthdayStreamEventKey(memberID, dateStr, "") + ":"
}

func buildBirthdayStreamEnvelopes(
	candidates []birthdayStreamCandidate,
	roomsByBirthdayEventKey map[string][]string,
	publishedEvents map[string]domain.AlarmQueueEnvelope,
	dateStr string,
) []domain.AlarmQueueEnvelope {
	var envelopes []domain.AlarmQueueEnvelope

	for _, candidate := range candidates {
		displayName := resolveCelebrationMemberName(candidate.member)
		rooms := roomsByBirthdayEventKey[birthdayGreetingEventKey(candidate.member.ID, dateStr)]
		published, wasPublished := publishedBirthdayStreamEvent(candidate, publishedEvents, dateStr)

		for _, roomID := range rooms {
			if wasPublished {
				envelopes = append(envelopes, birthdayStreamEnvelopeFromPublished(published, roomID))

				continue
			}

			envelopes = append(envelopes, birthdayStreamEnvelope(&candidate, displayName, roomID, dateStr))
		}
	}

	return envelopes
}

func birthdayStreamEnvelope(
	candidate *birthdayStreamCandidate,
	displayName string,
	roomID string,
	dateStr string,
) domain.AlarmQueueEnvelope {
	return domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    roomID,
			Channel:   &domain.Channel{ID: candidate.member.ChannelID, Name: displayName},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:              domain.CelebrationKindBirthdayStream,
			MemberID:          candidate.member.ID,
			MemberName:        displayName,
			ChannelID:         candidate.member.ChannelID,
			Photo:             candidate.member.Photo,
			Date:              dateStr,
			VideoID:           candidate.session.VideoID,
			StreamTitle:       candidate.session.Title,
			StreamURL:         domain.YouTubeWatchURL(candidate.session.VideoID),
			ScheduledStartKST: birthdayStreamScheduledStartKST(&candidate.session),
		},
		Version: contractsalarm.QueueEnvelopeVersionV1,
	}
}

func birthdayStreamEnvelopeFromPublished(
	published domain.AlarmQueueEnvelope,
	roomID string,
) domain.AlarmQueueEnvelope {
	published.Notification.RoomID = roomID
	published.Notification.Users = nil

	return published
}

func birthdayStreamCandidateEventKeys(candidates []birthdayStreamCandidate, dateStr string) []string {
	eventKeys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		eventKeys = append(eventKeys, birthdayStreamEventKey(candidate.member.ID, dateStr, candidate.session.VideoID))
	}

	return eventKeys
}

func birthdayGreetingEventKeys(candidates []birthdayStreamCandidate, dateStr string) []string {
	eventKeys := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))

	for _, candidate := range candidates {
		eventKey := birthdayGreetingEventKey(candidate.member.ID, dateStr)
		if _, ok := seen[eventKey]; ok {
			continue
		}

		seen[eventKey] = struct{}{}
		eventKeys = append(eventKeys, eventKey)
	}

	return eventKeys
}

func publishedBirthdayStreamEvent(
	candidate birthdayStreamCandidate,
	publishedEvents map[string]domain.AlarmQueueEnvelope,
	dateStr string,
) (domain.AlarmQueueEnvelope, bool) {
	published, ok := publishedEvents[birthdayStreamEventKey(candidate.member.ID, dateStr, candidate.session.VideoID)]

	return published, ok
}

func countBirthdayStreamAudienceRooms(roomsByEventKey map[string][]string) int {
	seen := make(map[string]struct{})

	for _, rooms := range roomsByEventKey {
		for _, roomID := range rooms {
			seen[roomID] = struct{}{}
		}
	}

	return len(seen)
}

func birthdayStreamScheduledStartKST(session *BirthdayStreamSession) string {
	start := util.FirstNonNilTime(session.ScheduledStart, session.StartedAt)
	if start == nil {
		return ""
	}

	return util.FormatKST(*start, "15:04")
}
