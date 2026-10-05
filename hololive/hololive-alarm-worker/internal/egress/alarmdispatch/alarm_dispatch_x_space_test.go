package alarmdispatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/util"
)

func TestXSpaceRenderingAndIsolation(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		SourceKind:   domain.AlarmDispatchSourceKindXSpace,
		Notification: domain.AlarmNotification{RoomID: "room-a", AlarmType: domain.AlarmTypeLive},
		XSpace:       &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: testAlarmChannelID, Title: "이야기", StartedAt: time.Now().UTC()},
	}
	message, handled, err := renderAlarmDispatchGroupSource(t.Context(), newAlarmDispatchTestRenderer(t), nil, xSpaceTestMembers{testAlarmChannelID: "소라"}, alarmDispatchGroup{envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "🔴 소라 스페이스 시작\n\u200b이야기\nhttps://x.com/i/spaces/1abc", message)

	require.NoError(t, alarmDispatchEnvelopeError(&envelope))

	other := envelope

	other.XSpace = new(*envelope.XSpace)
	other.XSpace.SpaceID = "1def"
	require.NotEqual(t, alarmDispatchGroupKey(&envelope), alarmDispatchGroupKey(&other))
	require.NotEqual(t, alarmDispatchGroupKey(&envelope), alarmDispatchGroupKey(&domain.AlarmQueueEnvelope{Notification: envelope.Notification}))

	envelope.XSpace.SpaceID = "bad/path"
	_, _, err = renderAlarmDispatchGroupSource(t.Context(), nil, nil, xSpaceTestMembers{testAlarmChannelID: "소라"}, alarmDispatchGroup{envelopes: []domain.AlarmQueueEnvelope{envelope}})
	require.Error(t, err)
}

func TestXSpaceDispatchUsesTextAndRecordsCompletion(t *testing.T) {
	envelope := domain.AlarmQueueEnvelope{
		SourceKind:   domain.AlarmDispatchSourceKindXSpace,
		Notification: domain.AlarmNotification{RoomID: testAlarmRoomID, AlarmType: domain.AlarmTypeLive},
		XSpace:       &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: testAlarmChannelID, Title: "이야기", StartedAt: time.Now().UTC()},
	}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]domain.AlarmQueueEnvelope{{envelope}}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: xSpaceTestMembers{testAlarmChannelID: "소라"}, consumer: consumer, sender: sender, renderer: newAlarmDispatchTestRenderer(t), messageStrings: newAlarmDispatchTestMessageStrings(t), maxBatch: 10}

	processed, err := runner.runOnce(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, testAlarmRoomID, sender.roomID)
	require.Equal(t, []string{"🔴 소라 스페이스 시작\n\u200b이야기\nhttps://x.com/i/spaces/1abc"}, sender.messages)
	require.Len(t, consumer.markSending, 1)
	require.Len(t, consumer.markDispatched, 1)
	require.Empty(t, consumer.scheduledRetry)
	require.Empty(t, consumer.movedDLQ)
}

func TestXSpaceTemplateTitleAndMarkdown(t *testing.T) {
	renderer := newAlarmDispatchTestRenderer(t)

	const markdownMemberName = "**소라** [링크]"

	for _, title := range []string{"", "[제목](https://example.com) **강조** _밑줄_"} {
		t.Run(title, func(t *testing.T) {
			payload := &domain.XSpaceDispatchPayload{
				SpaceID: "1abc", CreatorID: "123", ChannelID: testAlarmChannelID,
				Title: title, StartedAt: time.Now().UTC(),
			}
			envelope := domain.AlarmQueueEnvelope{
				SourceKind:   domain.AlarmDispatchSourceKindXSpace,
				Notification: domain.AlarmNotification{RoomID: testAlarmRoomID, AlarmType: domain.AlarmTypeLive},
				XSpace:       payload,
			}
			message, handled, err := renderAlarmDispatchGroupSource(t.Context(), renderer, nil, xSpaceTestMembers{testAlarmChannelID: markdownMemberName}, alarmDispatchGroup{envelopes: []domain.AlarmQueueEnvelope{envelope}})
			require.NoError(t, err)
			require.True(t, handled)

			link := payload.URL()

			if title != "" {
				link = util.KakaoZeroWidthSpace + util.MarkdownNeutralize(title) + "\n" + link
			}

			require.Equal(t, "🔴 "+util.MarkdownNeutralize(markdownMemberName)+" 스페이스 시작\n"+link, message)
		})
	}
}

func TestXSpaceUsesDatabaseTemplateAndPreservesMissingError(t *testing.T) {
	pool := dbtest.NewPool(t)
	envelope := domain.AlarmQueueEnvelope{
		SourceKind:   domain.AlarmDispatchSourceKindXSpace,
		Notification: domain.AlarmNotification{RoomID: testAlarmRoomID, AlarmType: domain.AlarmTypeLive},
		XSpace:       &domain.XSpaceDispatchPayload{SpaceID: "1abc", CreatorID: "123", ChannelID: testAlarmChannelID, StartedAt: time.Now().UTC()},
	}
	_, err := pool.Exec(t.Context(), `UPDATE notification_templates SET body = '{{.MemberName}}: {{.URL}}' WHERE template_key = $1 AND channel_id IS NULL`, domain.TemplateKeyXSpaceStarted)
	require.NoError(t, err)

	group := alarmDispatchGroup{envelopes: []domain.AlarmQueueEnvelope{envelope}}
	message, handled, err := renderAlarmDispatchGroupSource(t.Context(), template.NewRenderer(pool, slog.Default()), nil, xSpaceTestMembers{testAlarmChannelID: "소라"}, group)
	require.NoError(t, err)
	require.True(t, handled)
	require.Equal(t, "소라: https://x.com/i/spaces/1abc", message)

	_, err = pool.Exec(t.Context(), `DELETE FROM notification_templates WHERE template_key = $1`, domain.TemplateKeyXSpaceStarted)
	require.NoError(t, err)

	message, handled, err = renderAlarmDispatchGroupSource(t.Context(), template.NewRenderer(pool, slog.Default()), nil, xSpaceTestMembers{testAlarmChannelID: "소라"}, group)
	require.ErrorIs(t, err, template.ErrTemplateNotFound)
	require.True(t, handled)
	require.Empty(t, message)
}

// xSpaceTestMembers는 채널 ID별 짧은 한국어 표시명을 돌려준다. X 스페이스 알림 이름은 payload가 아니라 members에서 온다.
type xSpaceTestMembers map[string]string

func (m xSpaceTestMembers) FindMemberByChannelID(_ context.Context, channelID string) (*domain.Member, error) {
	name, ok := m[channelID]
	if !ok {
		return nil, domain.ErrMemberNotFound
	}

	return &domain.Member{ChannelID: channelID, Name: "English", ShortKoreanName: name}, nil
}

func (xSpaceTestMembers) FindMemberByName(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (xSpaceTestMembers) FindMemberByAlias(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (xSpaceTestMembers) FindMembersByName(context.Context, string) ([]*domain.Member, error) {
	return nil, nil
}

func (xSpaceTestMembers) FindMembersByAlias(context.Context, string) ([]*domain.Member, error) {
	return nil, nil
}

func (xSpaceTestMembers) GetChannelIDs(context.Context) ([]string, error) { return nil, nil }

func (xSpaceTestMembers) LoadAllMembers(context.Context) ([]*domain.Member, error) { return nil, nil }
