package workerapp

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCelebrationTestRenderer(t *testing.T) *template.Renderer {
	t.Helper()
	return template.NewRenderer(dbtest.NewPool(t), slog.Default())
}

func TestRenderCelebrationMessageBirthday(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := domain.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "시라카미 후부키",
			ChannelID:  "UCdn5BQ06XqgXoAxIhbqw5Rg",
		},
	}

	msg, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)
	assert.Equal(t, "🎂 시라카미 후부키 생일 축하합니다!\n🔗 https://youtube.com/channel/UCdn5BQ06XqgXoAxIhbqw5Rg", msg)
}

func TestRenderCelebrationMessageBirthdayOrdinal(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := domain.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "리오나",
			ChannelID:  "UC9LSiN9hXI55svYEBrrK-tw",
			Ordinal:    2,
		},
	}

	msg, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)
	assert.Equal(t, "🎂 리오나 2번째 생일 축하합니다!\n🔗 https://youtube.com/channel/UC9LSiN9hXI55svYEBrrK-tw", msg)
}

func TestRenderCelebrationMessageAnniversary(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := domain.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindAnniversary,
			MemberName: "토키노 소라",
			ChannelID:  "UCp6993wxpyDPHUpavwDFqgg",
			Years:      7,
		},
	}

	msg, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)
	assert.Equal(t, "🎉 토키노 소라 데뷔 7주년 축하합니다!\n🔗 https://youtube.com/channel/UCp6993wxpyDPHUpavwDFqgg", msg)
}

func TestRenderCelebrationMessageNilPayload(t *testing.T) {
	t.Parallel()

	_, err := renderCelebrationMessage(t.Context(), newCelebrationTestRenderer(t), &domain.AlarmQueueEnvelope{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload is nil")
}

func TestRenderCelebrationMessageAnniversaryZeroYears(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:  domain.CelebrationKindAnniversary,
			Years: 0,
		},
	}

	_, err := renderCelebrationMessage(t.Context(), newCelebrationTestRenderer(t), &envelope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "years must be positive")
}

func TestRenderCelebrationMessageUnknownKind(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{Kind: "unknown"},
	}

	_, err := renderCelebrationMessage(t.Context(), newCelebrationTestRenderer(t), &envelope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown kind")
}

func TestAlarmDispatchGroupKeyCelebration(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:      domain.CelebrationKindBirthday,
			ChannelID: "UC_test",
		},
	}

	got := alarmDispatchGroupKey(&envelope)
	assert.Equal(t, "room-1|celebration|birthday|UC_test", got)
}

func TestAlarmDispatchGroupKeyCelebrationPerMember(t *testing.T) {
	t.Parallel()

	member1 := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{RoomID: "room-1", AlarmType: domain.AlarmTypeBirthday},
		SourceKind:   domain.AlarmDispatchSourceKindCelebration,
		Celebration:  &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, ChannelID: "UC_a"},
	}
	member2 := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{RoomID: "room-1", AlarmType: domain.AlarmTypeBirthday},
		SourceKind:   domain.AlarmDispatchSourceKindCelebration,
		Celebration:  &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, ChannelID: "UC_b"},
	}

	groups := groupAlarmDispatchEnvelopes([]domain.AlarmQueueEnvelope{member1, member2})
	assert.Len(t, groups, 2)
}

func TestAlarmDispatchKaringGroupKeyCelebrationDelegates(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{RoomID: "room-1", AlarmType: domain.AlarmTypeBirthday},
		SourceKind:   domain.AlarmDispatchSourceKindCelebration,
		Celebration:  &domain.CelebrationDispatchPayload{Kind: domain.CelebrationKindBirthday, ChannelID: "UC_test"},
	}

	assert.Equal(t, alarmDispatchGroupKey(&envelope), alarmDispatchKaringGroupKey(&envelope))
}

func TestDispatchGroupCelebrationUsesMessagePath(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    "room-1",
			Channel:   &domain.Channel{ID: "UC_test", Name: "Test"},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "Test Member",
			ChannelID:  "UC_test",
			Date:       "2026-05-26",
		},
	}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := alarmDispatchRunner{consumer: consumer, sender: sender, renderer: newCelebrationTestRenderer(t), karingEnabled: true, postSendQuarantine: true, maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.messages[0], "🎂 Test Member 생일 축하합니다!")
	assert.Contains(t, sender.messages[0], "https://youtube.com/channel/UC_test")
	assert.Empty(t, sender.karingRequests)
}

func TestRenderAlarmDispatchGroupCelebration(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := domain.AlarmQueueEnvelope{
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindAnniversary,
			MemberName: "토키노 소라",
			ChannelID:  "UCp6993wxpyDPHUpavwDFqgg",
			Years:      7,
		},
	}
	group := alarmDispatchGroup{
		roomID:    "room-1",
		envelopes: []domain.AlarmQueueEnvelope{envelope},
	}

	msg, err := renderAlarmDispatchGroup(t.Context(), renderer, nil, group)
	require.NoError(t, err)
	assert.Equal(t, "🎉 토키노 소라 데뷔 7주년 축하합니다!\n🔗 https://youtube.com/channel/UCp6993wxpyDPHUpavwDFqgg", msg)
}
