package alarmdispatch

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type countingCollabMembers struct {
	collabTestMembers

	loads int
}

func (m *countingCollabMembers) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	m.loads++

	return collabTestMemberList(), nil
}

func collabRunnerTestEnvelope(roomID string, collabNames ...string) dispatchoutbox.AlarmQueueEnvelope {
	envelope := alarmDispatchRunnerTestEnvelope(roomID, nil)

	envelope.Notification.Stream.CollaboTalentNames = collabNames

	return envelope
}

func TestAlarmDispatchRunnerBuildsCollabIndexOncePerBatch(t *testing.T) {
	renderer, store := newAlarmDispatchTestRendering(t)
	members := &countingCollabMembers{}
	consumer := &alarmDispatchRunnerTestConsumer{batches: [][]dispatchoutbox.AlarmQueueEnvelope{
		{collabRunnerTestEnvelope("room-a", "星街すいせい"), collabRunnerTestEnvelope("room-b", "星街すいせい")},
		{collabRunnerTestEnvelope("room-c", "星街すいせい")},
		{collabRunnerTestEnvelope("room-d")},
	}}
	sender := &alarmDispatchRunnerTestSender{}
	runner := Runner{members: members, consumer: consumer, sender: sender, renderer: renderer, messageStrings: store, maxBatch: 10}

	for batch, wantLoads := range []int{1, 2, 2} {
		processed, err := runner.runOnce(t.Context())
		require.NoError(t, err)
		require.True(t, processed)
		assert.Equal(t, wantLoads, members.loads, "member loads after batch %d", batch+1)
	}

	require.Len(t, sender.messages, 4)

	for _, message := range sender.messages[:3] {
		assert.Contains(t, message, "스이세이")
	}
}

type alarmRenderStatementCounter struct {
	statements atomic.Int64
}

func (c *alarmRenderStatementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.statements.Add(1)

	return ctx
}

func (*alarmRenderStatementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestRenderTemplateVariantsResolvesBothVariantsInOneStatement(t *testing.T) {
	counter := &alarmRenderStatementCounter{}
	cfg := dbtest.NewPool(t).Config()

	cfg.ConnConfig.Tracer = counter

	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	renderer := template.NewRenderer(pool, slog.New(slog.DiscardHandler))
	view := xSpaceStartView{MemberName: "소라", Title: "제목", URL: "https://x.com/i/spaces/1abc"}

	_, err = renderTemplateVariants(t.Context(), renderer, domain.TemplateKeyXSpaceStarted, view)
	require.NoError(t, err)
	counter.statements.Store(0)

	message, err := renderTemplateVariants(t.Context(), renderer, domain.TemplateKeyXSpaceStarted, view)
	require.NoError(t, err)
	assert.Contains(t, message.Plain, "소라")
	assert.Contains(t, message.RichText, "소라")
	assert.Equal(t, int64(1), counter.statements.Load())
}
