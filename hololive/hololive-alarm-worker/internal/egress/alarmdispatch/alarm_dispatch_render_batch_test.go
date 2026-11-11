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

// countingCollabMembers는 collabTestMembers와 같은 멤버를 돌려주며 전체 멤버 적재 횟수를 센다.
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

// 콜라보 표기명 색인은 claim 배치마다 한 번만 만들어 그 배치의 모든 group이 쓰고, 다음 배치는 멤버를 다시 적재한다.
// 콜라보 이름이 없는 배치는 멤버를 적재하지 않는다.
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

// 두 본문 변형 렌더는 template 버전 확인을 한 문장으로 끝낸다. 파싱 cache가 찬 뒤에는 그 한 문장만 실행한다.
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
