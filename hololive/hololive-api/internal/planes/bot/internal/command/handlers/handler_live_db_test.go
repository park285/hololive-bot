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
 VALUES('CURRENT',2,repeat('a',64),now()+interval '1 hour',now());
 INSERT INTO youtube_collection_targets(projection_generation,subject_key,observation_kind,priority,poll_interval_ms,enabled)
 SELECT generation,'UC_command_query',kind,20,120000,true FROM youtube_collection_projection_generations
 CROSS JOIN (VALUES('live_snapshot'),('channel_live_check')) kinds(kind) WHERE status='CURRENT';`)

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
	t.Logf("actual command unknown reply: %s", *message)
	runSQL(`INSERT INTO youtube_live_sessions(video_id,channel_id,status,title,started_at) VALUES('cmdlive0001','UC_command_query','LIVE','DB 방송',now());
 INSERT INTO youtube_live_reconciliation_heads(video_id,status,last_live_positive_at,last_live_positive_seen_at) VALUES('cmdlive0001','LIVE',now(),now());`)
	execute()
	require.Contains(t, *message, "cmdlive0001")
	runSQL(`INSERT INTO youtube_channel_live_checks
 (channel_id,provider,outcome,selected_video_id,channel_identity_confirmed,evidence_sha256,scheduled_for,effective_at,observed_at,received_at)
 VALUES('UC_command_query','youtubejs','UPCOMING_VIDEO','waiting0001',true,repeat('b',64),now(),now(),now(),now());`)
	execute()
	require.Contains(t, *message, "cmdlive0001")
	t.Logf("actual command positive despite negative /live: %s", *message)
	runSQL(`UPDATE youtube_live_sessions SET status='ENDED';
 INSERT INTO youtube_live_pending_ends(video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 VALUES('cmdorphan01','UC_command_query','EXPLICIT_END',910002,now(),now(),now(),true,true);`)
	execute()
	t.Logf("actual command confirmed-empty reply (D1/D2 diagnostics retained): %s", *message)

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
