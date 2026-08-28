package celebration

import (
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	celebrationMemberIdentityLegacyReadFrom    = "2026-08-28"
	celebrationMemberIdentityLegacyReadThrough = "2026-08-29"
)

// alarm-worker owner는 전환 당일의 구 identity 행만 읽고 2026-08-30부터 이 경로를 제거한다.
func useLegacyCelebrationIdentity(dateStr string) bool {
	return dateStr >= celebrationMemberIdentityLegacyReadFrom && dateStr <= celebrationMemberIdentityLegacyReadThrough
}

func celebrationEventKey(payload *domain.CelebrationDispatchPayload) string {
	return dispatchoutbox.BuildEventKey(&dispatchoutbox.DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: payload.Identity(),
	})
}

func legacyCelebrationEventKey(payload *domain.CelebrationDispatchPayload) string {
	if payload == nil {
		return ""
	}

	legacy := *payload

	legacy.MemberID = 0

	return celebrationEventKey(&legacy)
}

func sameCelebrationMember(left, right *domain.CelebrationDispatchPayload) bool {
	return left != nil && right != nil &&
		left.MemberID == 0 &&
		left.Kind == right.Kind &&
		left.ChannelID == right.ChannelID &&
		left.MemberName == right.MemberName &&
		left.Date == right.Date
}

func legacyCelebrationEventKeys(envelopes []domain.AlarmQueueEnvelope) []string {
	keys := make([]string, 0, len(envelopes))
	seen := make(map[string]struct{}, len(envelopes))

	for i := range envelopes {
		if envelopes[i].Celebration == nil || envelopes[i].Celebration.MemberID <= 0 {
			continue
		}

		key := legacyCelebrationEventKey(envelopes[i].Celebration)
		if key == "" {
			continue
		}

		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

func birthdayGreetingEventKey(channelID, dateStr string, memberIDs ...int) string {
	payload := domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthday,
		ChannelID: channelID,
		Date:      dateStr,
	}

	if len(memberIDs) > 0 {
		payload.MemberID = memberIDs[0]
	}

	return celebrationEventKey(&payload)
}

func birthdayStreamEventKey(channelID, dateStr, videoID string, memberIDs ...int) string {
	payload := domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		ChannelID: channelID,
		Date:      dateStr,
		VideoID:   videoID,
	}

	if len(memberIDs) > 0 {
		payload.MemberID = memberIDs[0]
	}

	return celebrationEventKey(&payload)
}

func birthdayStreamEventKeyPrefix(channelID, dateStr string, memberIDs ...int) string {
	return birthdayStreamEventKey(channelID, dateStr, "", memberIDs...) + ":"
}

func buildBirthdayStreamEnvelopes(
	candidates []birthdayStreamCandidate,
	roomsByBirthdayEventKey map[string][]string,
	publishedBirthdayEvents map[string]domain.AlarmQueueEnvelope,
	publishedEvents map[string]domain.AlarmQueueEnvelope,
	dateStr string,
) []domain.AlarmQueueEnvelope {
	var envelopes []domain.AlarmQueueEnvelope

	for _, candidate := range candidates {
		displayName := resolveCelebrationMemberName(candidate.member)
		rooms := birthdayStreamAudienceRooms(candidate, roomsByBirthdayEventKey, publishedBirthdayEvents, dateStr)
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
	eventKeys := make([]string, 0, len(candidates)*2)
	for _, candidate := range candidates {
		eventKeys = append(eventKeys, birthdayStreamEventKey(candidate.member.ChannelID, dateStr, candidate.session.VideoID, candidate.member.ID))
		if useLegacyCelebrationIdentity(dateStr) && candidate.member.ID > 0 {
			eventKeys = append(eventKeys, birthdayStreamEventKey(candidate.member.ChannelID, dateStr, candidate.session.VideoID))
		}
	}

	return eventKeys
}

func birthdayGreetingEventKeys(candidates []birthdayStreamCandidate, dateStr string) []string {
	eventKeys := make([]string, 0, len(candidates)*2)
	seen := make(map[string]struct{}, len(candidates)*2)

	for _, candidate := range candidates {
		keys := []string{birthdayGreetingEventKey(candidate.member.ChannelID, dateStr, candidate.member.ID)}
		if useLegacyCelebrationIdentity(dateStr) && candidate.member.ID > 0 {
			keys = append(keys, birthdayGreetingEventKey(candidate.member.ChannelID, dateStr))
		}

		for _, eventKey := range keys {
			if _, ok := seen[eventKey]; ok {
				continue
			}

			seen[eventKey] = struct{}{}
			eventKeys = append(eventKeys, eventKey)
		}
	}

	return eventKeys
}

func birthdayStreamAudienceRooms(
	candidate birthdayStreamCandidate,
	roomsByEventKey map[string][]string,
	publishedBirthdayEvents map[string]domain.AlarmQueueEnvelope,
	dateStr string,
) []string {
	keys := []string{birthdayGreetingEventKey(candidate.member.ChannelID, dateStr, candidate.member.ID)}
	if useLegacyCelebrationIdentity(dateStr) && candidate.member.ID > 0 {
		legacyKey := birthdayGreetingEventKey(candidate.member.ChannelID, dateStr)
		if published, ok := publishedBirthdayEvents[legacyKey]; ok &&
			sameCelebrationMember(published.Celebration, birthdayGreetingIdentity(candidate, dateStr)) {
			keys = append(keys, legacyKey)
		}
	}

	rooms := make([]string, 0)
	seen := make(map[string]struct{})

	for _, key := range keys {
		for _, roomID := range roomsByEventKey[key] {
			if _, ok := seen[roomID]; ok {
				continue
			}

			seen[roomID] = struct{}{}
			rooms = append(rooms, roomID)
		}
	}

	return rooms
}

func publishedBirthdayStreamEvent(
	candidate birthdayStreamCandidate,
	publishedEvents map[string]domain.AlarmQueueEnvelope,
	dateStr string,
) (domain.AlarmQueueEnvelope, bool) {
	currentKey := birthdayStreamEventKey(candidate.member.ChannelID, dateStr, candidate.session.VideoID, candidate.member.ID)
	if published, ok := publishedEvents[currentKey]; ok {
		return published, true
	}

	if !useLegacyCelebrationIdentity(dateStr) || candidate.member.ID <= 0 {
		return domain.AlarmQueueEnvelope{}, false
	}

	legacyKey := birthdayStreamEventKey(candidate.member.ChannelID, dateStr, candidate.session.VideoID)
	published, ok := publishedEvents[legacyKey]

	if !ok || !legacyBirthdayStreamBelongsToCandidate(published, candidate, dateStr) {
		return domain.AlarmQueueEnvelope{}, false
	}

	return published, true
}

func birthdayGreetingIdentity(candidate birthdayStreamCandidate, dateStr string) *domain.CelebrationDispatchPayload {
	return &domain.CelebrationDispatchPayload{
		Kind:       domain.CelebrationKindBirthday,
		MemberID:   candidate.member.ID,
		MemberName: resolveCelebrationMemberName(candidate.member),
		ChannelID:  candidate.member.ChannelID,
		Date:       dateStr,
	}
}

func legacyBirthdayStreamBelongsToCandidate(
	published domain.AlarmQueueEnvelope,
	candidate birthdayStreamCandidate,
	dateStr string,
) bool {
	expected := &domain.CelebrationDispatchPayload{
		Kind:       domain.CelebrationKindBirthdayStream,
		MemberID:   candidate.member.ID,
		MemberName: resolveCelebrationMemberName(candidate.member),
		ChannelID:  candidate.member.ChannelID,
		Date:       dateStr,
		VideoID:    candidate.session.VideoID,
	}

	return sameCelebrationMember(published.Celebration, expected) &&
		published.Celebration.VideoID == expected.VideoID
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
