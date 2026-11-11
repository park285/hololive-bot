package alarmdispatch

import (
	"log/slog"
	"testing"

	"github.com/park285/shared-go/v2/pkg/kakaoformat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

func newCelebrationTestRenderer(t *testing.T) *template.Renderer {
	t.Helper()

	return template.NewRenderer(dbtest.NewPool(t), slog.Default())
}

func TestRenderCelebrationMessageBirthday(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "시라카미 후부키",
			ChannelID:  "UCdn5BQ06XqgXoAxIhbqw5Rg",
		},
	}

	variants, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)

	for _, msg := range messageTexts(variants) {
		assert.Contains(t, kakaoformat.Render(msg), envelope.Celebration.MemberName)
		assert.Contains(t, msg, "https://youtube.com/channel/"+envelope.Celebration.ChannelID)
	}
}

func TestRenderCelebrationMessageBirthdayOrdinal(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "리오나",
			ChannelID:  "UC9LSiN9hXI55svYEBrrK-tw",
			Ordinal:    2,
		},
	}

	variants, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)

	for _, msg := range messageTexts(variants) {
		assert.Contains(t, kakaoformat.Render(msg), envelope.Celebration.MemberName)
		assert.Contains(t, msg, "https://youtube.com/channel/"+envelope.Celebration.ChannelID)
		assert.Contains(t, msg, "2")
	}
}

func TestRenderCelebrationMessageAnniversary(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindAnniversary,
			MemberName: "토키노 소라",
			ChannelID:  "UCp6993wxpyDPHUpavwDFqgg",
			Years:      7,
		},
	}

	variants, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)

	for _, msg := range messageTexts(variants) {
		assert.Contains(t, kakaoformat.Render(msg), envelope.Celebration.MemberName)
		assert.Contains(t, msg, "https://youtube.com/channel/"+envelope.Celebration.ChannelID)
		assert.Contains(t, msg, "7")
	}
}

func TestRenderCelebrationMessageBirthdayStream(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:              domain.CelebrationKindBirthdayStream,
			MemberName:        "시라카미 후부키",
			ChannelID:         "UCdn5BQ06XqgXoAxIhbqw5Rg",
			VideoID:           "video-1",
			StreamTitle:       "【생일 방송】후부키 생일 기념 라이브!",
			StreamURL:         "https://www.youtube.com/watch?v=video-1",
			ScheduledStartKST: "21:00",
		},
	}

	variants, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)

	for _, msg := range messageTexts(variants) {
		for _, field := range []string{envelope.Celebration.MemberName, envelope.Celebration.ScheduledStartKST, envelope.Celebration.StreamTitle, envelope.Celebration.StreamURL} {
			assert.Contains(t, kakaoformat.Render(msg), field)
		}
	}
}

func TestRenderCelebrationMessageBirthdayStreamWithoutOptionalFields(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthdayStream,
			MemberName: "시라카미 후부키",
			ChannelID:  "UCdn5BQ06XqgXoAxIhbqw5Rg",
			VideoID:    "video-1",
		},
	}

	variants, err := renderCelebrationMessage(t.Context(), renderer, &envelope)
	require.NoError(t, err)

	for _, msg := range messageTexts(variants) {
		assert.Contains(t, kakaoformat.Render(msg), envelope.Celebration.MemberName)
		assert.NotContains(t, msg, "https://")
	}
}

func TestRenderCelebrationMessageNilPayload(t *testing.T) {
	t.Parallel()

	_, err := renderCelebrationMessage(t.Context(), newCelebrationTestRenderer(t), &dispatchoutbox.AlarmQueueEnvelope{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "payload is nil")
}

func TestRenderCelebrationMessageAnniversaryZeroYears(t *testing.T) {
	t.Parallel()

	envelope := dispatchoutbox.AlarmQueueEnvelope{
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

	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Celebration: &domain.CelebrationDispatchPayload{Kind: "unknown"},
	}

	_, err := renderCelebrationMessage(t.Context(), newCelebrationTestRenderer(t), &envelope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown kind")
}

func TestDispatchGroupCelebrationUsesMessagePath(t *testing.T) {
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeBirthday,
			RoomID:    testAlarmRoomID,
			Channel:   &domain.Channel{ID: testCelebrationChannelID, Name: "Test"},
		},
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindBirthday,
			MemberName: "Test Member",
			ChannelID:  testCelebrationChannelID,
			Date:       "2026-05-26",
		},
	}

	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: alarmGoldenMembers{}, consumer: consumer, sender: sender, renderer: newCelebrationTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())

	require.NoError(t, err)
	assert.True(t, processed)
	require.Len(t, sender.messages, 1)
	assert.Contains(t, sender.messages[0], envelope.Celebration.MemberName)
	assert.Contains(t, sender.messages[0], "https://youtube.com/channel/UC_test")
}

func TestRenderAlarmDispatchGroupCelebration(t *testing.T) {
	t.Parallel()

	renderer := newCelebrationTestRenderer(t)
	envelope := dispatchoutbox.AlarmQueueEnvelope{
		SourceKind: domain.AlarmDispatchSourceKindCelebration,
		Celebration: &domain.CelebrationDispatchPayload{
			Kind:       domain.CelebrationKindAnniversary,
			MemberName: "토키노 소라",
			ChannelID:  "UCp6993wxpyDPHUpavwDFqgg",
			Years:      7,
		},
	}
	group := alarmDispatchGroup{
		roomID:    testAlarmRoomID,
		envelopes: []dispatchoutbox.AlarmQueueEnvelope{envelope},
	}

	variants, err := renderAlarmDispatchGroup(t.Context(), renderer, nil, &alarmDispatchMembers{}, "", group)
	require.NoError(t, err)

	for _, msg := range messageTexts(variants) {
		assert.Contains(t, kakaoformat.Render(msg), envelope.Celebration.MemberName)
		assert.Contains(t, msg, "https://youtube.com/channel/"+envelope.Celebration.ChannelID)
	}
}
