package handlers

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
	dbmocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/template"
)

type forbiddenLiveProvider struct {
	domain.StreamProvider

	calls int
}

func (p *forbiddenLiveProvider) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	p.calls++
	return nil, errors.New("live command must not call a YouTube provider")
}

func TestLiveCommandDatabaseSmoke(t *testing.T) {
	pool := dbtest.NewPool(t)

	runSQL := func(sql string) {
		t.Helper()

		_, err := pool.Exec(t.Context(), sql)
		require.NoError(t, err)
	}
	runSQL(`UPDATE members SET is_graduated=true,status='graduated';
 INSERT INTO members(slug,channel_id,english_name,org,sync_source) VALUES('command-query','UC_command_query','Query Member','Hololive','manual');
 UPDATE youtube_collection_projection_generations SET status='RETIRED' WHERE status='CURRENT';
 INSERT INTO youtube_collection_projection_generations(status,row_count,projection_sha256,valid_until,activated_at)
 VALUES('CURRENT',1,repeat('a',64),now()+interval '1 hour',now());
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled,valid_until)
 SELECT generation,'UC_command_query','live_snapshot',20,120000,true,valid_until FROM youtube_collection_projection_generations WHERE status='CURRENT';`)

	deps, _, message := liveCardTestDeps(t, []*domain.Member{{ChannelID: "UC_command_query", Name: "Query Member"}})

	deps.Formatter = formatter.NewResponseFormatter("!", template.NewRenderer(pool, deps.Logger))
	deps.LiveQuery = livequery.New(&dbmocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }})

	provider := &forbiddenLiveProvider{}

	deps.Holodex = provider

	errorsSent := 0

	deps.SendError = func(context.Context, string, string) error { errorsSent++; return nil }

	command := NewLiveCommand(deps)
	execute := func() {
		t.Helper()

		for range 2 {
			require.NoError(t, command.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, nil))
		}

		require.Zero(t, provider.calls)
		require.Zero(t, errorsSent)
	}
	execute()
	require.Equal(t, "현재 방송 상태를 확인할 수 없습니다.", *message)
	runSQL(`INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at) VALUES('cmdlive0001','UC_command_query','LIVE','DB 방송',now());
 INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at) VALUES('cmdlive0001','LIVE',now(),now());`)
	execute()
	require.Contains(t, *message, "cmdlive0001")
	// 부분 결과여도 확정 방송만 표시하고 채널 진단과 조회 시각은 붙이지 않는다.
	require.NotContains(t, *message, "조회 미완료")
	require.NotContains(t, *message, "기준:")
	runSQL(`INSERT INTO youtube_live_absence_slots(observation_id,scheduled_for,evidence_sha256,effective_at,received_at,scope_sha256,coverage)
 VALUES(910001,now(),repeat('b',64),now(),now(),repeat('c',64),'{"requested_channel_ids":["UC_command_query"],"filters":{"statuses":["LIVE"]}}');`)
	execute()
	require.Contains(t, *message, "cmdlive0001")
	require.NotContains(t, *message, "조회 미완료")
	runSQL(`UPDATE youtube_live_sessions SET status='ENDED';UPDATE youtube_live_reconciliation_heads SET status='ENDED';`)
	execute()
	require.Contains(t, *message, deps.Formatter.LiveQuery(t.Context(), livequery.Result{Status: livequery.Complete}, ""))
	require.NotContains(t, *message, "조회 미완료")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, command.Execute(ctx, &domain.CommandContext{Room: testRoomID}, nil), context.Canceled)
	require.Zero(t, errorsSent)
	pool.Close()
	require.NoError(t, command.Execute(t.Context(), &domain.CommandContext{Room: testRoomID}, nil))
	require.Equal(t, 1, errorsSent)
	require.Zero(t, provider.calls)
	t.Log("actual command/repository/template: cold/warm upstream=0; unknown, partial without diagnostics, confirmed-empty, cancellation and DB error preserved")
}
