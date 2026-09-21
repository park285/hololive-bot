package xspaces

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type fakeCollector struct {
	observations []Observation
	err          error
	calls        int
}

func (f *fakeCollector) Collect(context.Context, sessions.Cookies, []string) ([]Observation, error) {
	f.calls++
	return f.observations, f.err
}

type ledgerPublisher struct{ repo *dispatchoutbox.PgxRepository }

func (p ledgerPublisher) PublishDispatchBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error) {
	for i := range envelopes {
		envelopes[i].Version = 1
	}

	result, err := p.repo.InsertBatch(ctx, dispatchoutbox.PublishBatchInput{Envelopes: envelopes, Status: dispatchoutbox.StatusPending})
	if err != nil {
		return result, fmt.Errorf("insert test dispatch: %w", err)
	}

	return result, nil
}

func TestRunnerReconnectionAndStableStartAcrossRestart(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	store, err := sessions.NewStore(pool, bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)

	cookies := sessions.Cookies{AuthToken: strings.Repeat("a", 40), CSRFToken: strings.Repeat("b", 64)}
	accepted, err := store.Submit(ctx, cookies, "0")
	require.NoError(t, err)
	require.True(t, accepted)

	now := time.Now().UTC().Truncate(time.Millisecond)
	collector := &fakeCollector{observations: []Observation{{SpaceID: "1space", CreatorID: "123", Title: "최초 제목", StartedAt: now}}}
	config := Config{Targets: []Target{{UserID: "123", ChannelID: "UC" + strings.Repeat("a", 22), MemberName: "소라"}}, PollSeconds: 120}
	makeRunner := func() *Runner {
		r, buildErr := NewRunner(config, store, StartStore{Pool: pool}, collector, ledgerPublisher{dispatchoutbox.NewPgxRepositoryFromPool(pool, nil)},
			func(context.Context, string) ([]string, error) { return []string{"room-a", "room-b"}, nil }, slog.New(slog.DiscardHandler))
		require.NoError(t, buildErr)

		r.now = func() time.Time { return now }

		return r
	}
	require.NoError(t, makeRunner().RunOnce(ctx))

	status, err := store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "connected", status.State)
	require.Equal(t, "accepted", status.CandidateState)

	collector.observations[0].Title = "바뀐 제목"
	now = now.Add(3 * time.Minute)

	require.NoError(t, makeRunner().RunOnce(ctx))

	var events, deliveries, collisions int

	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_events WHERE category='x_space'`).Scan(&events))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries`).Scan(&deliveries))
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_event_collisions`).Scan(&collisions))
	require.Equal(t, 1, events)
	require.Equal(t, 2, deliveries)
	require.Zero(t, collisions)
	verifyRunnerReconnection(t, store, collector, &now, makeRunner, cookies)
}

func verifyRunnerReconnection(t *testing.T, store *sessions.Store, collector *fakeCollector, now *time.Time, makeRunner func() *Runner, cookies sessions.Cookies) {
	t.Helper()

	ctx := t.Context()

	collector.err = &CollectionError{Code: "authentication"}
	*now = now.Add(3 * time.Minute)

	require.Error(t, makeRunner().RunOnce(ctx))

	status, err := store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "error", status.State)
	require.Equal(t, "authentication_pending", status.LastError)

	// 재시작 직후에도 DB의 대기 시각을 지키고, 다음 조회 성공만으로 재인증 없이 복구한다.
	calls := collector.calls

	require.NoError(t, makeRunner().RunOnce(ctx))
	require.Equal(t, calls, collector.calls)

	collector.err = nil
	*now = now.Add(3 * time.Minute)

	require.NoError(t, makeRunner().RunOnce(ctx))

	status, err = store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "connected", status.State)
	require.Empty(t, status.LastError)

	// 네트워크 장애가 끼면 이전 인증 거부와 연속된 것으로 세지 않는다.
	for _, code := range []string{"authentication", "upstream", "authentication"} {
		collector.err = &CollectionError{Code: code}
		*now = now.Add(3 * time.Minute)

		require.Error(t, makeRunner().RunOnce(ctx))

		status, err = store.Status(ctx)
		require.NoError(t, err)
		require.Equal(t, "error", status.State)
	}

	// 두 번의 연속 거부는 서로 다른 runner에서도 확정하고 이후 요청을 멈춘다.
	collector.err = &CollectionError{Code: "authentication"}
	*now = now.Add(3 * time.Minute)

	require.Error(t, makeRunner().RunOnce(ctx))

	status, err = store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "auth_required", status.State)

	calls = collector.calls

	*now = now.Add(3 * time.Minute)

	require.NoError(t, makeRunner().RunOnce(ctx))
	require.Equal(t, calls, collector.calls)

	accepted, err := store.Submit(ctx, cookies, "1")
	require.NoError(t, err)
	require.True(t, accepted)

	collector.err = nil

	require.NoError(t, makeRunner().RunOnce(ctx))

	status, err = store.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, "connected", status.State)
}

func TestConfigDisabledAndInvalidTargets(t *testing.T) {
	t.Setenv("X_SPACES_CONFIG_FILE", "")

	config, err := LoadConfig()
	require.ErrorIs(t, err, ErrDisabled)
	require.Nil(t, config)

	cfg := Config{Targets: []Target{{UserID: "123", ChannelID: "UC" + strings.Repeat("a", 22), MemberName: "테스트"}}, PollSeconds: 120}
	require.NoError(t, cfg.Validate())

	cfg.Targets = append(cfg.Targets, cfg.Targets[0])
	require.Error(t, cfg.Validate())
}
