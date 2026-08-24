package celebration

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/util"
)

func birthdayGreetingEventKey(channelID, dateStr string) string {
	payload := domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		ChannelID: channelID,
		Date:      dateStr,
	}

	return dispatchoutbox.BuildEventKey(&dispatchoutbox.DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: payload.Identity(),
	})
}

func birthdayStreamEventKey(channelID, dateStr, videoID string) string {
	payload := domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		ChannelID: channelID,
		Date:      dateStr,
		VideoID:   videoID,
	}

	return dispatchoutbox.BuildEventKey(&dispatchoutbox.DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: payload.Identity(),
	})
}

func birthdayStreamEventKeyPrefix(channelID, dateStr string) string {
	return birthdayStreamEventKey(channelID, dateStr, "") + ":"
}

func buildBirthdayStreamEnvelopes(
	candidates []birthdayStreamCandidate,
	roomsByBirthdayEventKey map[string][]string,
	dateStr string,
) []domain.AlarmQueueEnvelope {
	var envelopes []domain.AlarmQueueEnvelope

	for _, candidate := range candidates {
		displayName := resolveCelebrationMemberName(candidate.member)
		rooms := roomsByBirthdayEventKey[birthdayGreetingEventKey(candidate.member.ChannelID, dateStr)]

		for _, roomID := range rooms {
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
			MemberName:        displayName,
			ChannelID:         candidate.member.ChannelID,
			Photo:             candidate.member.Photo,
			Date:              dateStr,
			VideoID:           candidate.session.VideoID,
			StreamTitle:       candidate.session.Title,
			StreamURL:         domain.YouTubeWatchURL(candidate.session.VideoID),
			ScheduledStartKST: birthdayStreamScheduledStartKST(&candidate.session),
		},
	}
}

func birthdayGreetingEventKeys(candidates []birthdayStreamCandidate, dateStr string) []string {
	eventKeys := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))

	for _, candidate := range candidates {
		eventKey := birthdayGreetingEventKey(candidate.member.ChannelID, dateStr)
		if _, ok := seen[eventKey]; ok {
			continue
		}

		seen[eventKey] = struct{}{}
		eventKeys = append(eventKeys, eventKey)
	}

	return eventKeys
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
