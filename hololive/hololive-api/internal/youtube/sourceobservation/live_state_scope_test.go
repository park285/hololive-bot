package sourceobservation

import (
	"cmp"
	"context"
	"fmt"
	"maps"
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
	scopeEndedSeventhID = "end-7"
	scopeEndedLastID    = "end-40"
	scopeNewUpcomingID  = "new-up"
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
		  ('legacy-1', $2, 'UPCOMING', 'g', $1::timestamptz + interval '5 hours', NULL, NULL, $1, NULL, 'legacy_unknown', NULL),
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
		INSERT INTO youtube_live_reconciliation_heads (video_id, status, ignored_absence_scheduled_for)
		VALUES ('meta-1', 'UPCOMING', ARRAY[$1::timestamptz - interval '10 minutes']),
		       ('legacy-1', 'UPCOMING', ARRAY[$1::timestamptz - interval '10 minutes'])`, base)
	seed(`
		INSERT INTO youtube_live_absence_slots (observation_id, scheduled_for, evidence_sha256, effective_at, received_at,
		    scope_sha256, coverage)
		SELECT 700 + g, $1::timestamptz + g * interval '10 minutes', decode(repeat('a', 64), 'hex'), $1::timestamptz + g * interval '10 minutes',
		       $1::timestamptz + g * interval '10 minutes', decode(repeat('b', 64), 'hex'),
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
	loadedPending     []string
	omitActiveHistory bool
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
			unloadedEnded: []string{"end-3", scopeEndedSeventhID, scopeEndedLastID},
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
			unloadedEnded: []string{scopeEndedPendingID, scopeEndedSeventhID, scopeEndedLastID},
			frozenPending: []string{"end-3"},
		},
		{
			name:          "complete empty",
			evidence:      liveScopeEvidence(at, 1003, contract.CompletenessComplete),
			unloadedEnded: []string{scopeEndedPendingID, scopeEndedSeventhID, scopeEndedLastID},
		},
		{
			name: "partial existing positives",
			evidence: liveScopeEvidence(at, 1004, contract.CompletenessPartial,
				live.SessionFact{VideoID: "up-1", ChannelID: scopeChannelID, Status: testStatusUpcoming},
				live.SessionFact{VideoID: "live-1", ChannelID: scopeChannelID, Status: testStatusLive, LiveStartConfirmed: true},
			),
			unloadedEnded:     []string{scopeEndedPendingID, scopeEndedSeventhID, scopeEndedLastID},
			omitActiveHistory: true,
		},
		{
			name: "partial new upcoming replays history",
			evidence: liveScopeEvidence(at, 1005, contract.CompletenessPartial,
				live.SessionFact{VideoID: scopeNewUpcomingID, ChannelID: scopeChannelID, Status: testStatusUpcoming},
			),
			unloadedEnded: []string{scopeEndedPendingID, scopeEndedSeventhID, scopeEndedLastID},
		},
		{
			name: "partial first live replays history",
			evidence: liveScopeEvidence(at, 1006, contract.CompletenessPartial,
				live.SessionFact{VideoID: "up-1", ChannelID: scopeChannelID, Status: testStatusLive, LiveStartConfirmed: true},
			),
			unloadedEnded: []string{scopeEndedPendingID, scopeEndedSeventhID, scopeEndedLastID},
		},
	}
}

// requireNarrowedLiveScope는 축소 적재가 읽은 행과 이력 적재 여부를 확인하고, 이력을 생략한 영상 ID를 돌려준다.
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

		metadataWithoutPositive := (session.LifecycleOrigin == live.OriginMetadataOnly || session.LifecycleOrigin == live.OriginLegacyUnknown) &&
			session.Clock.LastUpcomingPositiveAt == nil && session.Clock.LastLivePositiveAt == nil
		if session.HeadPresent && (session.Status == domain.LiveStatusEnded || metadataWithoutPositive || scenario.omitActiveHistory) {
			require.False(t, loaded, "%s: irrelevant history must not be loaded", videoID)

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

func checkLiveScopeScenario(t *testing.T, pool *pgxpool.Pool, channelVideoIDs []string, scenario *liveScopeScenario) {
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

	if scenario.omitActiveHistory {
		// 축소 전 소비자는 positive replay가 없어도 현재 slot을 읽었다.
		require.NoError(t, loadLiveAbsenceSlots(ctx, tx, &full, nil, nil,
			scenario.evidence.ScheduledFor, scenario.evidence.Coverage.RequestedChannelIDs))
	} else {
		require.NoError(t, loadLiveAbsencesForPositive(ctx, tx, &full, &scenario.evidence))
	}

	omitted := requireNarrowedLiveScope(t, &narrowed, &full, payload, scenario)

	fullDecision, err := live.Reduce(full, scenario.evidence, time.Hour, scenario.evidence.ReceivedAt)
	require.NoError(t, err)

	narrowedDecision, err := live.Reduce(narrowed, scenario.evidence, time.Hour, scenario.evidence.ReceivedAt)
	require.NoError(t, err)

	requireSameLiveDecision(t, &full, fullDecision, narrowedDecision, omitted)

	// due candidate 정리로 저장되는 ENDED는 생략한 이력도 비운다.
	require.NoError(t, persistLiveDecision(ctx, tx, &narrowedDecision))

	// 읽지 않은 동결 pending은 삭제 keep-list에 없어도 그 세션이 dirty가 아니므로 남는다.
	for _, videoID := range scenario.frozenPending {
		var observationID int64

		require.NoError(t, tx.QueryRow(ctx, `SELECT observation_id FROM youtube_live_pending_ends WHERE video_id=$1`, videoID).Scan(&observationID),
			"frozen pending %s was deleted", videoID)
		require.Equal(t, full.PendingEnds[videoID].ObservationID, observationID, "frozen pending %s was rewritten", videoID)
	}

	var dueCleared, dueHistoryCleared bool

	require.NoError(t, tx.QueryRow(ctx, `
		SELECT next_end_check_at IS NULL, ignored_absence_scheduled_for = '{}'::timestamptz[]
		FROM youtube_live_reconciliation_heads WHERE video_id=$1`, "end-due").Scan(&dueCleared, &dueHistoryCleared))
	require.True(t, dueCleared, "due candidate on ENDED was not cleared")
	require.True(t, dueHistoryCleared, "saving an ENDED session must clear its unused history")
}

// 채널 범위 적재는 payload 밖의 candidate 없는 ENDED를 읽지 않고, payload ENDED와 due candidate가
// 남은 ENDED는 이력 없이 읽는다. 이때 payload에 오른 candidate 없는 ENDED의 pending은 읽지 않지만
// candidate가 남은 ENDED·head만 남은 영상·session 없는 영상의 pending은 읽는다. 전체 적재 oracle과 같은 결정을 내고,
// 활성 이력은 보존하고 저장되는 ENDED 이력은 비운다.
func TestLiveStateLoadSkipsUnmentionedEndedSessions(t *testing.T) {
	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	seedLiveScopeFixture(t, pool, base)

	var channelVideoIDs []string

	require.NoError(t, pool.QueryRow(t.Context(), `SELECT array_agg(video_id::text ORDER BY video_id) FROM youtube_live_sessions WHERE channel_id=$1`,
		scopeChannelID).Scan(&channelVideoIDs))

	for _, scenario := range liveScopeScenarios(base) {
		t.Run(scenario.name, func(t *testing.T) {
			checkLiveScopeScenario(t, pool, channelVideoIDs, &scenario)
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

// 이력 없이 읽은 ENDED의 due candidate를 정리할 때 큰 배열도 비운다.
// 적재한 활성 세션의 이력은 계속 늘어난다.
func TestLiveEndedSessionSaveClearsIgnoredHistory(t *testing.T) {
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
		require.True(t, storedIgnoredEquals(t, pool, videoID, []time.Time{}), "%s: ENDED history must be cleared", videoID)
	}

	require.Empty(t, liveHeadToastIDs(t, pool), "saving ENDED heads must release their TOAST values")

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

// 신규·첫 positive는 PARTIAL이어도 과거 부재를 복원하고, positive 없는 메타데이터는 그대로 둔다.
func TestLivePartialFirstPositiveReplaysAbsences(t *testing.T) {
	pool := dbtest.NewPool(t)
	base := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	seedLiveScopeFixture(t, pool, base)

	for _, test := range []struct {
		name    string
		videoID string
		status  string
	}{
		{name: "new upcoming", videoID: scopeNewUpcomingID, status: testStatusUpcoming},
		{name: "observed first live", videoID: "up-1", status: testStatusLive},
		{name: "metadata first live", videoID: "meta-1", status: testStatusLive},
		{name: "legacy first live", videoID: "legacy-1", status: testStatusLive},
		{name: "metadata upcoming promoted during replay", videoID: "meta-1", status: testStatusUpcoming},
		{name: "legacy upcoming promoted during replay", videoID: "legacy-1", status: testStatusUpcoming},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(ctx))) }()

			if test.status == testStatusLive {
				// 첫 LIVE 직후에도 이미 무시한 과거 slot은 다시 세지 않는다.
				_, err = tx.Exec(ctx, `UPDATE youtube_live_reconciliation_heads
					SET ignored_absence_scheduled_for=ARRAY[$2::timestamptz]
					WHERE video_id=$1`, test.videoID, base.Add(10*time.Minute))
				require.NoError(t, err)
			}

			evidence := liveScopeEvidence(base.Add(5*time.Minute), 1100, contract.CompletenessPartial,
				live.SessionFact{VideoID: test.videoID, ChannelID: scopeChannelID, Status: test.status, LiveStartConfirmed: true})
			if test.status == testStatusUpcoming && test.videoID != scopeNewUpcomingID {
				evidence.Sessions = append(evidence.Sessions,
					live.SessionFact{VideoID: scopeNewUpcomingID, ChannelID: scopeChannelID, Status: testStatusUpcoming})
			}

			state, err := loadLiveState(ctx, tx, []string{scopeChannelID}, videoIDsOf(&evidence))
			require.NoError(t, err)
			require.NoError(t, loadLiveAbsencesForPositive(ctx, tx, &state, &evidence))
			require.Len(t, state.AbsenceSlots, 3, "first positive must load later retained absence slots")

			got := requirePartialReplayMatchesFullState(t, tx, &state, &evidence)

			idx := slices.IndexFunc(got.Sessions, func(session live.SessionState) bool { return session.VideoID == test.videoID })
			require.NotEqual(t, -1, idx)

			requireFirstPositiveReplay(t, &got.Sessions[idx], test.status, base)
		})
	}
}

func TestLiveConsumedPartialMatchesFullyLoadedDecision(t *testing.T) {
	pool, _, consumer, proof := startHolodexLivePersist(t)
	ctx := t.Context()
	base := proof.ScheduledFor.Add(-time.Hour)
	seedConsumedPartialFixture(t, pool, base)

	fact := liveSession("partial-live", testStatusLive)

	fact.StartedAt = &base

	envelope := liveSnapshotEnvelopeFromProviderAtGeneration(t, &proof, contract.LiveSnapshotMetadataContractGeneration,
		contract.ProviderHolodex, testHolodexLiveKey, liveSession("partial-up", testStatusUpcoming), fact)

	envelope.Completeness = contract.CompletenessPartial

	prepared, err := contract.PrepareEnvelope(*envelope)
	require.NoError(t, err)

	published, err := publishkit.NewPublisher(pool).PublishBatch(ctx, publishInput(&prepared))
	require.NoError(t, err)
	require.Len(t, published.Results, 1)

	observation := Observation{
		ID: published.Results[0].ObservationID, Provider: prepared.Provider, ObservationKind: prepared.ObservationKind,
		ObservationKey: prepared.ObservationKey, ScopeSHA256: prepared.ScopeSHA256, EvidenceSHA256: prepared.EvidenceSHA256,
		ScheduledFor: prepared.ScheduledFor, Completeness: prepared.Completeness, Continuity: prepared.Continuity, Payload: prepared.Payload,
	}
	require.NoError(t, pool.QueryRow(ctx, `SELECT scheduled_for, received_at FROM source_observations WHERE id=$1`,
		observation.ID).Scan(&observation.EffectiveAt, &observation.ReceivedAt))

	evidence, err := liveEvidenceFromObservation(&observation)
	require.NoError(t, err)

	// 같은 slot의 정본 coverage가 이미 있어도 PARTIAL은 부재를 새로 만들거나 덮어쓰지 않는다.
	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_live_absence_slots
		    (observation_id, scheduled_for, evidence_sha256, effective_at, received_at, scope_sha256, coverage)
		VALUES (1900, $1, decode(repeat('a', 64), 'hex'), $1, $2, decode(repeat('b', 64), 'hex'),
		    jsonb_build_object('requested_channel_ids', jsonb_build_array($3::text),
		        'filters', jsonb_build_object('statuses', jsonb_build_array('UPCOMING', 'LIVE'))))`,
		evidence.ScheduledFor, evidence.ReceivedAt, testChannelID)
	require.NoError(t, err)

	ids := []string{"partial-up", "partial-live", "partial-meta"}
	want, decision := fullyLoadedPartialDecision(t, pool, &evidence, ids)

	before := liveHeadToastIDs(t, pool)
	require.NotEmpty(t, before)
	require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))
	require.NoError(t, dbx.InPgxTx(ctx, pool, func(tx dbx.Tx) error {
		got, err := loadUnscopedLiveState(ctx, tx, ids)
		if err != nil {
			return err
		}

		require.Equal(t, want, got, "consumed PARTIAL must match the fully loaded reducer and persistence")

		return nil
	}))
	require.Equal(t, before, liveHeadToastIDs(t, pool), "ordinary PARTIAL must preserve existing TOAST values")
	assertTableCount(t, pool, "youtube_live_absence_slots", 1)

	for _, application := range decision.Applications {
		var stored string

		require.NoError(t, pool.QueryRow(ctx, `
			SELECT decision FROM source_observation_applications
			WHERE observation_id=$1 AND entity_kind=$2 AND entity_key=$3`,
			observation.ID, application.EntityKind, application.EntityKey).Scan(&stored))
		require.Equal(t, application.Decision, stored)
	}
}

func requirePartialReplayMatchesFullState(t *testing.T, tx dbx.Tx, state *live.State, evidence *live.Evidence) live.Decision {
	t.Helper()

	full, err := loadUnscopedLiveState(t.Context(), tx, slices.Sorted(maps.Keys(state.Sessions)))
	require.NoError(t, err)

	full.AbsenceSlots = state.AbsenceSlots

	omitted := map[string]struct{}{}

	for videoID := range state.Sessions {
		session := state.Sessions[videoID]
		if _, loaded := session.IgnoredAbsences.Slots(); !loaded {
			omitted[videoID] = struct{}{}
		}
	}

	want, err := live.Reduce(full, *evidence, time.Hour, evidence.ReceivedAt)
	require.NoError(t, err)

	got, err := live.Reduce(*state, *evidence, time.Hour, evidence.ReceivedAt)
	require.NoError(t, err)
	requireSameLiveDecision(t, &full, want, got, omitted)

	return got
}

func requireFirstPositiveReplay(t *testing.T, session *live.SessionState, status string, base time.Time) {
	t.Helper()

	if status == testStatusUpcoming {
		slots, loaded := session.IgnoredAbsences.Slots()
		require.True(t, loaded)

		wantSlots := 3

		if session.VideoID != scopeNewUpcomingID {
			wantSlots++
		}

		require.Len(t, slots, wantSlots)
	} else {
		require.Equal(t, 2, session.Clock.ConsecutiveAbsenceSlots)
		require.EqualValues(t, 703, session.LastAbsenceObservationID)
		require.Equal(t, new(base.Add(20*time.Minute)), session.FirstAbsenceScheduledFor)
		require.NotNil(t, session.Clock.EndCandidateObservationID)
	}
}

func seedConsumedPartialFixture(t *testing.T, pool *pgxpool.Pool, base time.Time) {
	t.Helper()

	ctx := t.Context()
	history := toastIgnoredHistory(base.Add(-17000 * time.Minute))
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_sessions (video_id, channel_id, status, title, last_seen_at, lifecycle_origin)
		VALUES ('partial-up', $1, 'UPCOMING', 'up', $2, 'observed'),
		       ('partial-live', $1, 'LIVE', 'live', $2, 'observed'),
		       ('partial-meta', $1, 'UPCOMING', 'meta', $2, 'metadata_only')`, testChannelID, base)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO youtube_live_reconciliation_heads
		    (video_id, status, last_upcoming_positive_at, last_live_positive_at, last_live_positive_seen_at, ignored_absence_scheduled_for)
		VALUES ('partial-up', 'UPCOMING', $1, NULL, NULL, $2),
		       ('partial-live', 'LIVE', $1, $1, $1, $2),
		       ('partial-meta', 'UPCOMING', NULL, NULL, NULL, $2)`, base, history)
	require.NoError(t, err)
}

func fullyLoadedPartialDecision(t *testing.T, pool *pgxpool.Pool, evidence *live.Evidence, ids []string) (live.State, live.Decision) {
	t.Helper()

	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)

	defer rollbackPublishTestTx(context.WithoutCancel(ctx), t, tx, "fully loaded PARTIAL oracle")

	full, err := loadUnscopedLiveState(ctx, tx, ids)
	require.NoError(t, err)
	require.NoError(t, loadLiveAbsenceSlots(ctx, tx, &full, nil, nil, evidence.ScheduledFor, evidence.Coverage.RequestedChannelIDs))
	require.Len(t, full.AbsenceSlots, 1, "the fully loaded oracle must replay the existing current slot")

	decision, err := live.Reduce(full, *evidence, 0, evidence.ReceivedAt)
	require.NoError(t, err)
	require.NoError(t, persistLiveDecision(ctx, tx, &decision))

	want, err := loadUnscopedLiveState(ctx, tx, ids)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))

	return want, decision
}

// 다른 kind의 늦은 LIVE 뒤 같은 slot의 PARTIAL은 이미 저장된 COMPLETE 부재만 재적용한다.
// 새 부재를 만들지 않으며 이미 센 slot과 UPCOMING 때 무시한 slot은 재계산하지 않는다.
func TestLivePartialCurrentSlotReplaysAfterDelayedVideoCheck(t *testing.T) {
	for _, grace := range []time.Duration{0, time.Hour} {
		t.Run(grace.String(), func(t *testing.T) {
			pool, repo, _, holodexProof := startHolodexLivePersist(t)
			ctx := t.Context()
			consumer := NewConsumerWithGraces(repo, 0, grace)
			publisher := publishkit.NewPublisher(pool)
			proof := seedAdditionalLease(t, pool, &holodexProof, contract.KindLiveSnapshot, testChannelID, "youtubejs_channel_live")
			videoProof := seedAdditionalLease(t, pool, &proof, contract.KindVideoLiveCheck, testVideoID, "youtubejs_video_live")

			videoProof = advanceLease(ctx, t, pool, &videoProof, -time.Minute)
			finalizeLeaseRosterCount(t, pool, &holodexProof)

			completeID := seedDelayedLivePromotion(t, pool, publisher, consumer, &proof, &videoProof)
			at := proof.ScheduledFor
			base := at.Add(-time.Hour)
			companion := liveSession("partial-live", testStatusLive)

			companion.StartedAt = &base

			observation := publishHolodexReplayPartial(t, publisher, &holodexProof, companion)
			before := assertPartialCurrentSlotReplay(t, pool, consumer, observation, completeID)

			replayLiveObservation(t, repo, consumer, observation.ID)
			require.Equal(t, 1, liveConsecutiveSlots(t, pool), "same-slot replay must not count twice")
			require.True(t, storedIgnoredEquals(t, pool, "partial-up", slices.Concat(
				toastIgnoredHistory(base.Add(-17000*time.Minute)), []time.Time{at})), "ignored current slot must not be appended twice")
			require.Equal(t, before, liveHeadToastIDs(t, pool))
			assertTableCount(t, pool, "youtube_live_absence_slots", 1)

			// 다음 PARTIAL에는 저장된 slot이 없으므로 빠진 LIVE를 새 부재로 세지 않는다.
			holodexProof = advanceLease(ctx, t, pool, &holodexProof, time.Minute)
			publishHolodexReplayPartial(t, publisher, &holodexProof, companion)
			require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))
			require.Equal(t, 1, liveConsecutiveSlots(t, pool), "PARTIAL cannot supply a new absence slot")
			require.Equal(t, before, liveHeadToastIDs(t, pool))
			assertTableCount(t, pool, "youtube_live_absence_slots", 1)

			proof = advanceLease(ctx, t, pool, &proof, 2*time.Minute)

			lastID := publishConsumeLiveFromProvider(ctx, t, publisher, consumer, &proof,
				contract.ProviderYouTubeJS, testChannelID)
			require.Equal(t, 2, liveConsecutiveSlots(t, pool))
			assertTableCount(t, pool, "youtube_live_absence_slots", 2)
			assertLiveAbsenceEndOutcome(t, pool, at, lastID, grace)
		})
	}
}

func seedDelayedLivePromotion(t *testing.T, pool *pgxpool.Pool, publisher *publishkit.Publisher,
	consumer *Consumer, proof, videoProof *contract.LeaseProof,
) int64 {
	t.Helper()

	ctx := t.Context()
	_, err := pool.Exec(ctx, `UPDATE observation_contract_generations
		SET current_schema_version=$1, current_generation=$2
		WHERE provider='youtubejs' AND observation_kind='video_live_check'`,
		contract.VideoLifecycleSchemaVersion, contract.VideoLifecycleContractGeneration)
	require.NoError(t, err)

	base := proof.ScheduledFor.Add(-time.Hour)
	seedConsumedPartialFixture(t, pool, base)

	_, err = pool.Exec(ctx, `INSERT INTO youtube_live_sessions
		(video_id, channel_id, status, title, last_seen_at, lifecycle_origin)
		VALUES ($1, $2, 'UPCOMING', 'metadata', $3, 'metadata_only')`, testVideoID, testChannelID, base)
	require.NoError(t, err)

	completeID := publishConsumeLiveFromProvider(ctx, t, publisher, consumer, proof,
		contract.ProviderYouTubeJS, testChannelID)
	assertTableCount(t, pool, "youtube_live_absence_slots", 1)
	require.Equal(t, testStatusUpcoming, liveSessionStatus(t, pool))

	// 지원되는 schema/generation의 영상 확인을 실제 Claim/Finalize로 뒤늦게 소비한다.
	publishLiveCheck(ctx, t, publisher, videoLiveCheckEnvelope(t, videoProof, liveVideoCheck(contract.VideoAvailabilityPublic)))
	consumeLiveChecks(ctx, t, consumer)
	require.Equal(t, testStatusLive, liveSessionStatus(t, pool))
	require.Zero(t, liveConsecutiveSlots(t, pool))
	assertLifecycleOrigin(t, pool, string(live.OriginObserved))
	assertTableCount(t, pool, "youtube_live_absence_slots", 1)

	return completeID
}

func publishHolodexReplayPartial(t *testing.T, publisher *publishkit.Publisher, proof *contract.LeaseProof,
	companion contract.LiveSessionV1,
) Observation {
	t.Helper()

	envelope := liveSnapshotEnvelopeFromProviderAtGeneration(t, proof,
		contract.LiveSnapshotMetadataContractGeneration, contract.ProviderHolodex, testHolodexLiveKey, companion)

	envelope.Completeness = contract.CompletenessPartial

	prepared, err := contract.PrepareEnvelope(*envelope)
	require.NoError(t, err)

	published, err := publisher.PublishBatch(t.Context(), publishInput(&prepared))
	require.NoError(t, err)
	require.Len(t, published.Results, 1)

	return Observation{
		ID: published.Results[0].ObservationID, Provider: prepared.Provider, ObservationKind: prepared.ObservationKind,
		ObservationKey: prepared.ObservationKey, ScopeSHA256: prepared.ScopeSHA256, EvidenceSHA256: prepared.EvidenceSHA256,
		ScheduledFor: prepared.ScheduledFor, Completeness: prepared.Completeness, Continuity: prepared.Continuity, Payload: prepared.Payload,
	}
}

func assertPartialCurrentSlotReplay(t *testing.T, pool *pgxpool.Pool, consumer *Consumer,
	observation Observation, completeID int64,
) []int64 {
	t.Helper()

	ctx := t.Context()
	require.NoError(t, pool.QueryRow(ctx, `SELECT scheduled_for, received_at FROM source_observations WHERE id=$1`,
		observation.ID).Scan(&observation.EffectiveAt, &observation.ReceivedAt))

	evidence, err := liveEvidenceFromObservation(&observation)
	require.NoError(t, err)

	ids := []string{testVideoID, "partial-up", "partial-live", "partial-meta"}
	want, _ := fullyLoadedPartialDecision(t, pool, &evidence, ids)
	require.Equal(t, 1, want.Sessions[testVideoID].Clock.ConsecutiveAbsenceSlots)

	before := liveHeadToastIDs(t, pool)
	require.NotEmpty(t, before)
	require.NoError(t, consumer.Consume(ctx, liveClaimOptions()))

	got, err := dbx.InPgxTxWithResult(ctx, pool, func(tx dbx.Tx) (live.State, error) {
		return loadUnscopedLiveState(ctx, tx, ids)
	})
	require.NoError(t, err)
	require.Equal(t, want, got, "current-slot replay must match the fully loaded oracle")

	session := got.Sessions[testVideoID]
	require.NotNil(t, session.FirstAbsenceScheduledFor)
	require.True(t, observation.ScheduledFor.Equal(*session.FirstAbsenceScheduledFor))
	require.Equal(t, completeID, session.LastAbsenceObservationID, "PARTIAL must use the stored COMPLETE evidence")
	require.Nil(t, session.Clock.EndCandidateObservationID)
	require.Equal(t, before, liveHeadToastIDs(t, pool), "unchanged ignored history must retain its TOAST storage")
	assertTableCount(t, pool, "youtube_live_absence_slots", 1)

	return before
}

func assertLiveAbsenceEndOutcome(t *testing.T, pool *pgxpool.Pool, at time.Time, lastID int64, grace time.Duration) {
	t.Helper()

	ctx := t.Context()
	state, err := dbx.InPgxTxWithResult(ctx, pool, func(tx dbx.Tx) (live.State, error) {
		return loadUnscopedLiveState(ctx, tx, []string{testVideoID})
	})
	require.NoError(t, err)

	session := state.Sessions[testVideoID]
	require.NotNil(t, session.FirstAbsenceScheduledFor)
	require.NotNil(t, session.SecondAbsenceScheduledFor)
	require.True(t, at.Equal(*session.FirstAbsenceScheduledFor))
	require.True(t, at.Add(2*time.Minute).Equal(*session.SecondAbsenceScheduledFor))

	if grace == 0 {
		require.Equal(t, domain.LiveStatusEnded, session.Status)
		require.Equal(t, new(live.EndReasonScopedAbsence), session.EndReason)
		require.Nil(t, session.Clock.EndCandidateObservationID)
		require.Empty(t, state.PendingEnds)
	} else {
		require.Equal(t, domain.LiveStatusLive, session.Status)
		require.Equal(t, new(lastID), session.Clock.EndCandidateObservationID)
		require.Equal(t, new(live.EndEvidenceScopedAbsence), session.Clock.EndCandidateKind)
		require.NotNil(t, session.Clock.NextEndCheckAt)
	}
}
