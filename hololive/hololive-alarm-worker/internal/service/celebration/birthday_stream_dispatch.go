package celebration

import (
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
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
	sentRoomsByStreamEventKey map[string][]string,
	dateStr string,
) []domain.AlarmQueueEnvelope {
	var envelopes []domain.AlarmQueueEnvelope

	for _, candidate := range candidates {
		displayName := resolveCelebrationMemberName(candidate.member)
		rooms := roomsByBirthdayEventKey[birthdayGreetingEventKey(candidate.member.ID, dateStr)]
		streamEventKey := birthdayStreamEventKey(candidate.member.ID, dateStr, candidate.session.VideoID)
		published, wasPublished := publishedEvents[streamEventKey]
		sentRooms := sentRoomsByStreamEventKey[streamEventKey]

		for _, roomID := range rooms {
			if wasPublished {
				if slices.Contains(sentRooms, roomID) {
					continue
				}

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
			ScheduledStartKST: birthdayStreamScheduledStartKST(&candidate.session, dateStr),
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

func countBirthdayStreamAudienceRooms(roomsByEventKey map[string][]string) int {
	seen := make(map[string]struct{})

	for _, rooms := range roomsByEventKey {
		for _, roomID := range rooms {
			seen[roomID] = struct{}{}
		}
	}

	return len(seen)
}

// 판정 창이 KST 생일 당일 앞뒤로 넓으므로 다른 KST 날짜에 시작하는 방송은 날짜를 붙여
// 생일 당일 시각으로 오해하지 않게 한다.
func birthdayStreamScheduledStartKST(session *BirthdayStreamSession, dateStr string) string {
	start := timeutil.FirstNonNilTime(session.ScheduledStart, session.StartedAt)
	if start == nil {
		return ""
	}

	if timeutil.FormatKST(*start, time.DateOnly) != dateStr {
		return timeutil.FormatKST(*start, "01/02 15:04")
	}

	return timeutil.FormatKST(*start, "15:04")
}
