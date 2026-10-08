package sourceobservation

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

const (
	scopeChannelID = "UC_SCOPE"
	// 종료 후보 없이 pending(D2)이 남은 ENDED 세션이다.
	scopeEndedPendingID = "end-1"
)

// loadUnscopedLiveState는 축소 전 적재의 oracle이다. 주어진 영상을 상태와 관계없이 모두 읽고
// 무시한 부재 이력도 생략하지 않는다.
func loadUnscopedLiveState(ctx context.Context, tx dbx.Tx, videoIDs []string) (live.State, error) {
	state := live.State{Sessions: map[string]live.SessionState{}, PendingEnds: map[string]live.PendingEnd{}}
	if err := loadLiveSessions(ctx, tx, &state, nil, videoIDs); err != nil {
		return live.State{}, err
	}

	if err := loadLiveHeads(ctx, tx, &state, videoIDs, nil); err != nil {
		return live.State{}, err
	}

	markHeadlessHistoriesLoaded(&state)

	if err := loadLivePendingEnds(ctx, tx, &state, videoIDs); err != nil {
		return live.State{}, err
	}

	return state, nil
}

func seedLiveScopeFixture(t *testing.T, pool *pgxpool.Pool, base time.Time) {
	t.Helper()

	ctx := t.Context()
	seed := func(statement string, args ...any) {
		t.Helper()

		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err)
	}

	seed(`
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, scheduled_start_time, started_at, ended_at,
		    last_seen_at, live_first_seen_at, lifecycle_origin, status_observed_at)
		VALUES
		  ('up-1',   $2, 'UPCOMING', 'u', $1::timestamptz + interval '3 hours', NULL, NULL, $1, NULL, 'observed', $1),
		  ('live-1', $2, 'LIVE', 'l1', $1, $1, NULL, $1, $1, 'observed', $1),
		  ('live-2', $2, 'LIVE', 'l2', $1, $1, NULL, $1, $1, 'observed', $1),
		  ('meta-1', $2, 'UPCOMING', 'm', $1::timestamptz + interval '5 hours', NULL, NULL, $1, NULL, 'metadata_only', NULL),
		  ('end-due', $2, 'ENDED', 'd', $1, $1, $1, $1, $1, 'observed', $1),
		  ('end-fut', $2, 'ENDED', 'f', $1, $1, $1, $1, $1, 'observed', $1),
		  ('oth-1', 'UC_OTHER', 'LIVE', 'o', $1, $1, NULL, $1, $1, 'observed', $1)`, base, scopeChannelID)
	seed(`
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, started_at, ended_at, last_seen_at,
		    live_first_seen_at, lifecycle_origin, status_observed_at)
		SELECT 'end-' || g, $2, 'ENDED', 't', $1::timestamptz - g * interval '1 day',
		       $1::timestamptz - g * interval '1 day' + interval '1 hour', $1::timestamptz - g * interval '1 day',
		       $1::timestamptz - g * interval '1 day', 'observed', $1::timestamptz - g * interval '1 day'
		FROM generate_series(1, 40) AS g`, base, scopeChannelID)
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_upcoming_positive_at, last_upcoming_positive_seen_at,
		    last_live_positive_at, last_live_positive_seen_at, ended_at, end_reason, ignored_absence_scheduled_for)
		SELECT 'end-' || g, 'ENDED', $1::timestamptz - g * interval '1 day', $1::timestamptz - g * interval '1 day',
		       $1::timestamptz - g * interval '1 day', $1::timestamptz - g * interval '1 day',
		       $1::timestamptz - g * interval '1 day' + interval '1 hour', 'EXPLICIT_END',
		       ARRAY(SELECT $1::timestamptz - g * interval '1 day' - s * interval '1 minute' FROM generate_series(1, 1000) AS s)
		FROM generate_series(1, 40) AS g`, base)
	seed(`
		INSERT INTO youtube_live_pending_ends (video_id, channel_id, kind, observation_id, effective_at, received_at,
		    scheduled_for, ended_at, negative_eligible, scope_covers)
		VALUES ('end-1', $2, 'EXPLICIT_END', 800, $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		        $1::timestamptz - interval '1 hour', NULL, true, true),
		       ('end-due', $2, 'EXPLICIT_END', 900, $1, $1, $1, NULL, true, true),
		       ('end-fut', $2, 'EXPLICIT_END', 901, $1, $1, $1, NULL, true, true),
		       ('end-7', $2, 'EXPLICIT_END', 902, $1::timestamptz - interval '2 hours', $1::timestamptz - interval '2 hours',
		        $1::timestamptz - interval '2 hours', NULL, true, true),
		       ('end-3', $2, 'EXPLICIT_END', 903, $1::timestamptz - interval '3 hours', $1::timestamptz - interval '3 hours',
		        $1::timestamptz - interval '3 hours', NULL, true, true),
		       ('head-1', $2, 'EXPLICIT_END', 904, $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		        $1::timestamptz - interval '1 hour', NULL, true, true),
		       ('orphan-1', $2, 'EXPLICIT_END', 905, $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		        $1::timestamptz - interval '1 hour', NULL, true, true)`, base, scopeChannelID)
	// head-1은 session 행 없이 ENDED head만 남은 영상이고 orphan-1은 session·head가 모두 없는 영상(D1)이다.
	// 둘 다 저장된 session이 없어 종료를 새로 보관하므로 payload에 오르면 pending을 계속 읽는다.
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_upcoming_positive_at, last_upcoming_positive_seen_at,
		    last_live_positive_at, last_live_positive_seen_at, ended_at, end_reason, ignored_absence_scheduled_for)
		VALUES ('head-1', 'ENDED', $1::timestamptz - interval '3 hours', $1::timestamptz - interval '3 hours',
		        $1::timestamptz - interval '3 hours', $1::timestamptz - interval '3 hours', $1::timestamptz - interval '2 hours',
		        'EXPLICIT_END', ARRAY[$1::timestamptz - interval '4 hours'])`, base)
	// end-due는 session이 ENDED인데 head에 due candidate가 남은 행이다. 채널 범위 적재가 이 정리를 놓치면 안 된다.
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_upcoming_positive_at, last_upcoming_positive_seen_at,
		    last_live_positive_at, last_live_positive_seen_at, end_candidate_kind, end_candidate_observation_id, next_end_check_at,
		    consecutive_absence_slots, first_absence_scheduled_for, last_absence_scheduled_for, last_absence_observation_id,
		    ignored_absence_scheduled_for)
		VALUES
		  ('up-1', 'UPCOMING', $1, $1, NULL, NULL, NULL, NULL, NULL, 0, NULL, NULL, 0,
		      ARRAY[$1::timestamptz - interval '10 minutes', $1::timestamptz - interval '20 minutes']),
		  ('live-1', 'LIVE', $1, $1, $1, $1, NULL, NULL, NULL, 0, NULL, NULL, 0, '{}'),
		  ('live-2', 'LIVE', $1, $1, $1, $1, NULL, NULL, NULL, 1, $1::timestamptz + interval '30 minutes',
		      $1::timestamptz + interval '30 minutes', 777, '{}'),
		  ('end-due', 'LIVE', $1, $1, $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		      'EXPLICIT_END', 900, $1::timestamptz + interval '1 hour', 0, NULL, NULL, 0,
		      ARRAY[$1::timestamptz - interval '3 hours']),
		  ('end-fut', 'LIVE', $1, $1, $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		      'EXPLICIT_END', 901, $1::timestamptz + interval '100 days', 0, NULL, NULL, 0, '{}'),
		  ('oth-1', 'LIVE', $1, $1, $1, $1, NULL, NULL, NULL, 0, NULL, NULL, 0, '{}')`, base)
	seed(`
		INSERT INTO youtube_live_absence_slots (observation_id, scheduled_for, evidence_sha256, effective_at, received_at,
		    scope_sha256, coverage)
		SELECT 700 + g, $1::timestamptz + g * interval '10 minutes', repeat('a', 64), $1::timestamptz + g * interval '10 minutes',
		       $1::timestamptz + g * interval '10 minutes', repeat('b', 64),
		       jsonb_build_object('requested_channel_ids', jsonb_build_array($2::text),
		           'filters', jsonb_build_object('statuses', jsonb_build_array('ENDED', 'LIVE', 'UPCOMING')))
		FROM generate_series(1, 3) AS g`, base, scopeChannelID)
}

func sortLiveScopeDecision(decision *live.Decision) {
	slices.SortFunc(decision.Sessions, func(a, b live.SessionState) int { return cmp.Compare(a.VideoID, b.VideoID) })
	slices.SortFunc(decision.PendingEnds, func(a, b live.PendingEnd) int { return cmp.Compare(a.VideoID, b.VideoID) })
	slices.SortStableFunc(decision.Applications, func(a, b live.Application) int {
		return cmp.Or(cmp.Compare(a.EntityKey, b.EntityKey), cmp.Compare(a.Decision, b.Decision))
	})
}

func requireSameLiveDecision(t *testing.T, full *live.State, fullDecision, narrowedDecision live.Decision, omitted map[string]struct{}) {
	t.Helper()

	sortLiveScopeDecision(&fullDecision)
	sortLiveScopeDecision(&narrowedDecision)
	require.Len(t, narrowedDecision.Sessions, len(fullDecision.Sessions), "dirty sessions differ")

	for i := range fullDecision.Sessions {
		want, got := fullDecision.Sessions[i], narrowedDecision.Sessions[i]

		if _, ok := omitted[got.VideoID]; ok {
			_, loaded := got.IgnoredAbsences.Slots()
			require.False(t, loaded, "%s: omitted history became loaded", got.VideoID)

			got.IgnoredAbsences = want.IgnoredAbsences
		}

		require.Equal(t, want, got, "dirty session %s differs", want.VideoID)
	}

	require.Equal(t, fullDecision.Applications, narrowedDecision.Applications)
	require.Equal(t, fullDecision.AbsenceSlot, narrowedDecision.AbsenceSlot)

	fullPending := map[string]live.PendingEnd{}

	for i := range fullDecision.PendingEnds {
		fullPending[fullDecision.PendingEnds[i].VideoID] = fullDecision.PendingEnds[i]
	}

	for i := range narrowedDecision.PendingEnds {
		pending := narrowedDecision.PendingEnds[i]

		require.Equal(t, fullPending[pending.VideoID], pending, "pending %s differs", pending.VideoID)
		delete(fullPending, pending.VideoID)
	}

	// full에만 있는 pending은 축소 적재가 읽지 않은 행이며 reducer도 바꾸지 않아야 한다.
	for videoID := range fullPending {
		require.Equal(t, full.PendingEnds[videoID], fullPending[videoID], "unloaded pending %s was changed", videoID)
	}
}

type liveScopeScenario struct {
	name          string
	evidence      live.Evidence
	payloadEnded  []string
	unloadedEnded []string
	// frozenPending는 payload에 오른 종료 후보 없는 ENDED 세션의 pending(D2)이다. 읽거나 잠그지 않는다.
	frozenPending []string
	// loadedPending는 종료 후보가 남은 ENDED와 기본 채널 범위 밖에서 payload로 오른 pending이다.
	loadedPending []string
}

func liveScopeEvidence(at time.Time, id int64, completeness contract.Completeness, facts ...live.SessionFact) live.Evidence {
	return live.Evidence{
		Kind: contract.KindLiveSnapshot, ObservationID: id, ObservationKey: fmt.Sprintf("scope-%d", id),
		EvidenceSHA256: fmt.Sprintf("%064d", id), ScopeSHA256: strings.Repeat("ab", 32),
		ScheduledFor: at, EffectiveAt: at, ReceivedAt: at,
		Completeness: completeness, Continuity: contract.ContinuityNotApplicable, Sessions: facts,
		Coverage: contract.GlobalChannelCoverageV1{
			RequestedChannelIDs: []string{scopeChannelID}, GroupKey: scopeChannelID,
			Filters: contract.LiveFiltersV1{Statuses: []string{testStatusEnded, testStatusLive, testStatusUpcoming}},
		},
	}
}

func liveScopeScenarios(base time.Time) []liveScopeScenario {
	at := base.Add(2 * time.Hour)
	ended := base.Add(-time.Hour)
	started := base.Add(90 * time.Minute)

	return []liveScopeScenario{
		{
			name: "youtubejs complete with past ended",
			evidence: liveScopeEvidence(at, 1001, contract.CompletenessComplete,
				live.SessionFact{VideoID: scopeEndedPendingID, ChannelID: scopeChannelID, Status: testStatusEnded, EndedAt: &ended},
				live.SessionFact{VideoID: "end-2", ChannelID: scopeChannelID, Status: testStatusEnded, EndedAt: &ended},
				live.SessionFact{VideoID: "live-1", ChannelID: scopeChannelID, Status: testStatusLive, LiveStartConfirmed: true},
				live.SessionFact{VideoID: "new-1", ChannelID: scopeChannelID, Status: testStatusUpcoming},
				live.SessionFact{VideoID: "head-1", ChannelID: scopeChannelID, Status: testStatusEnded, EndedAt: &ended},
				live.SessionFact{VideoID: "orphan-1", ChannelID: scopeChannelID, Status: testStatusEnded, EndedAt: &ended},
			),
			payloadEnded:  []string{scopeEndedPendingID, "end-2"},
			unloadedEnded: []string{"end-3", "end-7", "end-40"},
			frozenPending: []string{scopeEndedPendingID},
			loadedPending: []string{"head-1", "orphan-1"},
		},
		{
			name: "holodex partial late live",
			evidence: liveScopeEvidence(at, 1002, contract.CompletenessPartial,
				live.SessionFact{VideoID: "up-1", ChannelID: scopeChannelID, Status: testStatusUpcoming},
				live.SessionFact{VideoID: "end-3", ChannelID: scopeChannelID, Status: testStatusLive, StartedAt: &started, LiveStartConfirmed: true},
				live.SessionFact{VideoID: "new-2", ChannelID: scopeChannelID, Status: testStatusLive, StartedAt: &started, LiveStartConfirmed: true},
			),
			payloadEnded:  []string{"end-3"},
			unloadedEnded: []string{scopeEndedPendingID, "end-7", "end-40"},
			frozenPending: []string{"end-3"},
		},
		{
			name:          "complete empty",
			evidence:      liveScopeEvidence(at, 1003, contract.CompletenessComplete),
			unloadedEnded: []string{scopeEndedPendingID, "end-7", "end-40"},
		},
	}
}

// requireNarrowedLiveScope는 축소 적재가 읽은 행과 이력 적재 여부를 확인하고, ENDED 세션 ID를 돌려준다.
func requireNarrowedLiveScope(t *testing.T, narrowed, full *live.State, payload []string, scenario *liveScopeScenario) map[string]struct{} {
	t.Helper()

	for _, videoID := range slices.Concat([]string{"end-due", "end-fut"}, scenario.payloadEnded) {
		require.Contains(t, narrowed.Sessions, videoID)
	}

	for _, videoID := range slices.Concat([]string{"oth-1"}, scenario.unloadedEnded) {
		require.NotContains(t, narrowed.Sessions, videoID)
		require.NotContains(t, narrowed.PendingEnds, videoID)
	}

	for _, videoID := range scenario.frozenPending {
		require.Contains(t, full.PendingEnds, videoID, "fixture must store frozen pending %s", videoID)
		require.NotContains(t, narrowed.PendingEnds, videoID, "frozen ENDED pending %s was loaded", videoID)
	}

	for _, videoID := range slices.Concat([]string{"end-due", "end-fut"}, scenario.loadedPending) {
		require.Contains(t, narrowed.PendingEnds, videoID, "pending %s was not loaded", videoID)
		require.Equal(t, full.PendingEnds[videoID], narrowed.PendingEnds[videoID], "pending %s differs", videoID)
	}

	omitted := map[string]struct{}{}

	for videoID := range narrowed.Sessions {
		session, fullSession := narrowed.Sessions[videoID], full.Sessions[videoID]
		slots, loaded := session.IgnoredAbsences.Slots()

		if session.Present && session.Status == domain.LiveStatusEnded {
			require.False(t, loaded, "%s: ENDED history must not be loaded", videoID)

			omitted[videoID] = struct{}{}

			continue
		}

		fullSlots, _ := fullSession.IgnoredAbsences.Slots()

		require.True(t, loaded, "%s: active history must be loaded", videoID)
		require.Equal(t, fullSlots, slots, "%s: active history differs", videoID)
	}

	for videoID := range full.Sessions {
		if _, ok := narrowed.Sessions[videoID]; ok {
			continue
		}

		session := full.Sessions[videoID]

		require.NotContains(t, payload, videoID)
		require.Equal(t, domain.LiveStatusEnded, session.Status, "non-ENDED session %s was skipped", videoID)
		require.Nil(t, session.Clock.NextEndCheckAt, "candidate session %s was skipped", videoID)
	}

	return omitted
}

func checkLiveScopeScenario(t *testing.T, pool *pgxpool.Pool, base time.Time, channelVideoIDs []string, scenario *liveScopeScenario) {
	t.Helper()

	ctx := t.Context()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(ctx))) }()

	payload := videoIDsOf(&scenario.evidence)

	narrowed, err := loadLiveState(ctx, tx, scenario.evidence.Coverage.RequestedChannelIDs, payload)
	require.NoError(t, err)
	require.NoError(t, loadLiveAbsencesForPositive(ctx, tx, &narrowed, &scenario.evidence))

	full, err := loadUnscopedLiveState(ctx, tx, slices.Concat(channelVideoIDs, payload))
	require.NoError(t, err)
	require.NoError(t, loadLiveAbsencesForPositive(ctx, tx, &full, &scenario.evidence))

	omitted := requireNarrowedLiveScope(t, &narrowed, &full, payload, scenario)

	fullDecision, err := live.Reduce(full, scenario.evidence, time.Hour, scenario.evidence.ReceivedAt)
	require.NoError(t, err)

	narrowedDecision, err := live.Reduce(narrowed, scenario.evidence, time.Hour, scenario.evidence.ReceivedAt)
	require.NoError(t, err)

	requireSameLiveDecision(t, &full, fullDecision, narrowedDecision, omitted)

	// due candidate 정리로 저장되는 ENDED도 생략한 이력을 지우지 않는다.
	require.NoError(t, persistLiveDecision(ctx, tx, &narrowedDecision))

	// 읽지 않은 동결 pending은 삭제 keep-list에 없어도 그 세션이 dirty가 아니므로 남는다.
	for _, videoID := range scenario.frozenPending {
		var observationID int64

		require.NoError(t, tx.QueryRow(ctx, `SELECT observation_id FROM youtube_live_pending_ends WHERE video_id=$1`, videoID).Scan(&observationID),
			"frozen pending %s was deleted", videoID)
		require.Equal(t, full.PendingEnds[videoID].ObservationID, observationID, "frozen pending %s was rewritten", videoID)
	}

	var dueCleared, dueHistoryKept bool

	require.NoError(t, tx.QueryRow(ctx, `
		SELECT next_end_check_at IS NULL, ignored_absence_scheduled_for = ARRAY[$2::timestamptz - interval '3 hours']
		FROM youtube_live_reconciliation_heads WHERE video_id=$1`, "end-due", base).Scan(&dueCleared, &dueHistoryKept))
	require.True(t, dueCleared, "due candidate on ENDED was not cleared")
	require.True(t, dueHistoryKept, "saving an ENDED session without loaded history erased it")
}

// 채널 범위 적재는 payload 밖의 candidate 없는 ENDED를 읽지 않고, payload ENDED와 due candidate가
// 남은 ENDED는 이력 없이 읽는다. 이때 payload에 오른 candidate 없는 ENDED의 pending은 읽지 않지만
// candidate가 남은 ENDED·head만 남은 영상·session 없는 영상의 pending은 읽는다. 전체 적재 oracle과 같은 결정을 내고,
// 저장해도 이력을 지우지 않는다.
func TestLiveStateLoadSkipsUnmentionedEndedSessions(t *testing.T) {
	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	seedLiveScopeFixture(t, pool, base)

	var channelVideoIDs []string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(video_id::text ORDER BY video_id) FROM youtube_live_sessions WHERE channel_id=$1`,
		scopeChannelID).Scan(&channelVideoIDs))

	for _, scenario := range liveScopeScenarios(base) {
		t.Run(scenario.name, func(t *testing.T) {
			checkLiveScopeScenario(t, pool, base, channelVideoIDs, &scenario)
		})
	}
}

func seedEndedHistoryFixture(t *testing.T, pool *pgxpool.Pool, base time.Time, history, activeHistory []time.Time) {
	t.Helper()

	ctx := t.Context()
	seed := func(statement string, args ...any) {
		t.Helper()

		_, err := pool.Exec(ctx, statement, args...)
		require.NoError(t, err)
	}

	seed(`
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, started_at, ended_at, last_seen_at,
		    live_first_seen_at, lifecycle_origin, status_observed_at)
		VALUES ('end-fin', $2, 'ENDED', 'f', $1, $1, $1, $1, 'observed', $1),
		       ('end-con', $2, 'ENDED', 'c', $1, $1, $1, $1, 'observed', $1)`, base, testChannelID)
	seed(`
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, scheduled_start_time, last_seen_at,
		    lifecycle_origin, status_observed_at)
		VALUES ('up-hist', $2, 'UPCOMING', 'u', $1::timestamptz + interval '1 day', $1, 'observed', $1)`, base, testChannelID)
	seed(`
		INSERT INTO youtube_live_pending_ends (video_id, channel_id, kind, observation_id, effective_at, received_at,
		    scheduled_for, ended_at, negative_eligible, scope_covers)
		VALUES ('end-fin', $2, 'EXPLICIT_END', 900, $1, $1, $1, NULL, true, true),
		       ('end-con', $2, 'EXPLICIT_END', 901, $1, $1, $1, NULL, true, true)`, base, testChannelID)
	// session이 ENDED인데 head에 due candidate가 남은 행이다. finalizer는 end-fin을 먼저 고른다.
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_live_positive_at, last_live_positive_seen_at,
		    end_candidate_kind, end_candidate_observation_id, next_end_check_at, ignored_absence_scheduled_for)
		VALUES ('end-fin', 'LIVE', $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		        'EXPLICIT_END', 900, NOW() - interval '2 hours', $2),
		       ('end-con', 'LIVE', $1::timestamptz - interval '1 hour', $1::timestamptz - interval '1 hour',
		        'EXPLICIT_END', 901, NOW() - interval '1 hour', $2)`, base, history)
	seed(`
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, last_upcoming_positive_at,
		    last_upcoming_positive_seen_at, ignored_absence_scheduled_for)
		VALUES ('up-hist', 'UPCOMING', $1, $1, $2)`, base, activeHistory)
}

// 이력 없이 읽은 ENDED 세션의 due candidate를 finalizer와 YouTube.js consumer가 정리해도
// 큰 배열과 TOAST 값은 그대로 남는다. 적재한 활성 세션의 이력은 계속 늘어난다.
func TestLiveEndedSessionSaveKeepsIgnoredHistory(t *testing.T) {
	pool, repo, consumer, proof := startLivePersist(t)
	ctx := t.Context()
	base := time.Now().UTC().Truncate(time.Microsecond).Add(-3 * time.Hour)
	history := make([]time.Time, 17000)
	activeHistory := []time.Time{base.Add(-2 * time.Hour), base.Add(-time.Hour)}

	for i := range history {
		history[i] = base.Add(-time.Duration(i+1) * time.Minute)
	}

	seedEndedHistoryFixture(t, pool, base, history, activeHistory)

	before := liveHeadToastIDs(t, pool)
	require.NotEmpty(t, before, "fixture must use external TOAST storage")

	processed, err := repo.FinalizeNextDueLiveEnd(ctx, time.Hour)
	require.NoError(t, err)
	require.True(t, processed, "finalizer did not process the due ENDED candidate")
	require.True(t, liveHeadCandidateCleared(t, pool, "end-fin"))
	require.False(t, liveHeadCandidateCleared(t, pool, "end-con"), "finalizer must handle one due head per call")

	// YouTube.js streams 탭은 과거 ENDED를 payload에 싣는다. 채널 범위의 due candidate도 함께 정리된다.
	publishConsumeLive(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, liveSession("end-con", testStatusEnded))
	require.True(t, liveHeadCandidateCleared(t, pool, "end-con"))

	for _, videoID := range []string{"end-fin", "end-con"} {
		require.True(t, storedIgnoredEquals(t, pool, videoID, history), "%s: saving without loaded history erased it", videoID)
	}

	require.Equal(t, before, liveHeadToastIDs(t, pool), "omitted histories must keep their TOAST values")

	var activeCount int

	require.NoError(t, pool.QueryRow(ctx, `SELECT cardinality(ignored_absence_scheduled_for) FROM youtube_live_reconciliation_heads WHERE video_id='up-hist'`).Scan(&activeCount))
	require.Equal(t, len(activeHistory)+1, activeCount, "loaded active history must still record the new ignored absence")
}

func liveHeadCandidateCleared(t *testing.T, pool *pgxpool.Pool, videoID string) bool {
	t.Helper()

	var cleared bool

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT next_end_check_at IS NULL AND end_candidate_kind IS NULL FROM youtube_live_reconciliation_heads WHERE video_id=$1`,
		videoID).Scan(&cleared))

	return cleared
}
