package livequery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const pendingAfterPositive = "1 second"

// reducer는 종결됐거나 positive가 이긴 종료 증거도 보존할 수 있다.
func TestRepositoryRetainedPendingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, stateSQL, offset, kind, channel string
		status                                Status
		reason                                Reason
		items                                 int
		diagnostics                           Diagnostics
	}{
		{name: "newer repeated end after terminal state", stateSQL: `UPDATE youtube_live_sessions SET status='ENDED'; UPDATE youtube_live_reconciliation_heads SET status='ENDED'`, offset: pendingAfterPositive, status: Complete, reason: Covered, diagnostics: Diagnostics{EndedPendingEnds: 1}},
		{name: "older end loses to live positive", offset: "-1 second", status: Complete, reason: Covered, items: 1},
		{name: "same-time end loses to live positive", offset: "0 seconds", status: Complete, reason: Covered, items: 1},
		{name: "newer end remains unresolved", offset: pendingAfterPositive, status: Unavailable, reason: ConfirmingEnd},
		{name: "upcoming positive defeats older cancel", stateSQL: `UPDATE youtube_live_sessions SET status='UPCOMING'; UPDATE youtube_live_reconciliation_heads SET status='UPCOMING',last_upcoming_positive_at=last_live_positive_at,last_live_positive_at=NULL`, offset: "-1 second", kind: "EXPLICIT_CANCEL", status: Complete, reason: Covered},
		{name: "newer cancel remains unresolved", stateSQL: `UPDATE youtube_live_sessions SET status='UPCOMING'; UPDATE youtube_live_reconciliation_heads SET status='UPCOMING',last_upcoming_positive_at=last_live_positive_at,last_live_positive_at=NULL`, offset: pendingAfterPositive, kind: "EXPLICIT_CANCEL", status: Unavailable, reason: ConfirmingEnd},
		{name: "channel mismatch survives obsolete end", offset: "-1 second", channel: "UC_other", status: Unavailable, reason: Inconsistent},
		{name: "ended session live head", stateSQL: `UPDATE youtube_live_sessions SET status='ENDED'`, offset: "-1 second", status: Complete, reason: Covered, diagnostics: Diagnostics{EndedPendingEnds: 1, EndedHeadMismatches: 1}},
		{name: "ended session missing head", stateSQL: `UPDATE youtube_live_sessions SET status='ENDED'; DELETE FROM youtube_live_reconciliation_heads`, offset: pendingAfterPositive, status: Complete, reason: Covered, diagnostics: Diagnostics{EndedPendingEnds: 1, EndedHeadMismatches: 1}},
		{name: "live session missing head", stateSQL: `DELETE FROM youtube_live_reconciliation_heads`, offset: pendingAfterPositive, status: Unavailable, reason: Inconsistent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := queryFixture(t)
			addCoverage(t, pool)
			addLive(t, pool)

			if tc.stateSQL != "" {
				execFixture(t, pool, tc.stateSQL)
			}

			if tc.kind == "" {
				tc.kind = "EXPLICIT_END"
			}

			if tc.channel == "" {
				tc.channel = "UC_live_query"
			}

			_, err := pool.Exec(t.Context(), `INSERT INTO youtube_live_pending_ends
 (video_id,channel_id,kind,observation_id,effective_at,received_at,scheduled_for,negative_eligible,scope_covers)
 SELECT s.video_id,$1,$2,900002,COALESCE(h.last_live_positive_at,h.last_upcoming_positive_at,now()-interval '30 seconds')+$3::interval,now(),now(),true,true
 FROM youtube_live_sessions s LEFT JOIN youtube_live_reconciliation_heads h USING(video_id) WHERE s.video_id='livequery01'`, tc.channel, tc.kind, tc.offset)
			require.NoError(t, err)

			result, err := repo.Query(t.Context(), Request{Scope: All, Limit: MaxItems})
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Status)
			require.Equal(t, tc.reason, result.Channels[0].Reason)
			require.Len(t, result.Items, tc.items)
			require.Equal(t, tc.diagnostics, result.DiagnosticCounts())

			var retained int

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_live_pending_ends WHERE video_id='livequery01'`).Scan(&retained))
			require.Equal(t, 1, retained)
		})
	}
}

type channelView struct {
	Reason      Reason
	Diagnostics Diagnostics
}

type endedPendingDiagnosticCase struct {
	name, otherOrg, sessionChannel, sessionStatus string
	request                                       Request
	status                                        Status
	want                                          map[string]channelView
}

func endedPendingDiagnosticCases() []endedPendingDiagnosticCase {
	const (
		pendingChannel = "UC_live_query"
		otherChannel   = "UC_other_query"
		hololiveOrg    = "Hololive"
		endedState     = "ENDED"
	)

	ended := Diagnostics{EndedPendingEnds: 1}
	all := Request{Scope: All, Limit: MaxItems}
	member := func(channelID string) Request { return Request{Scope: Member, ChannelID: channelID, Limit: MaxItems} }

	return []endedPendingDiagnosticCase{
		{
			name: "pending channel query", otherOrg: hololiveOrg, sessionChannel: otherChannel, sessionStatus: endedState, request: member(pendingChannel), status: Complete,
			want: map[string]channelView{pendingChannel: {Covered, ended}},
		},
		{
			name: "session channel query", otherOrg: hololiveOrg, sessionChannel: otherChannel, sessionStatus: endedState, request: member(otherChannel), status: Complete,
			want: map[string]channelView{otherChannel: {Covered, ended}},
		},
		{
			name: "both channels in roster", otherOrg: hololiveOrg, sessionChannel: otherChannel, sessionStatus: endedState, request: all, status: Complete,
			want: map[string]channelView{pendingChannel: {Covered, ended}, otherChannel: {Covered, ended}},
		},
		{
			name: "canonical channel outside operational roster", otherOrg: "VSpo", sessionChannel: otherChannel, sessionStatus: endedState, request: all, status: Complete,
			want: map[string]channelView{pendingChannel: {Covered, ended}},
		},
		{
			name: "canonical channel outside roster queried directly", otherOrg: "VSpo", sessionChannel: otherChannel, sessionStatus: endedState, request: member(otherChannel), status: Complete,
			want: map[string]channelView{otherChannel: {Covered, ended}},
		},
		{
			name: "same channel counted once", otherOrg: hololiveOrg, sessionChannel: pendingChannel, sessionStatus: endedState, request: all, status: Complete,
			want: map[string]channelView{pendingChannel: {Covered, ended}, otherChannel: {Covered, Diagnostics{}}},
		},
		{
			name: "live channel mismatch still blocks", otherOrg: hololiveOrg, sessionChannel: otherChannel, sessionStatus: "LIVE", request: all, status: Unavailable,
			want: map[string]channelView{pendingChannel: {ConfirmingEnd, Diagnostics{}}, otherChannel: {Inconsistent, Diagnostics{}}},
		},
	}
}

// 종결 session에 남은 pending은 pending 채널과 canonical 채널 어느 조회에서도 진단으로 남고 후보는 막지 않는다.
func TestRepositoryEndedPendingDiagnosticsFollowBothChannels(t *testing.T) {
	for _, tc := range endedPendingDiagnosticCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo, pool := queryFixture(t)
			addCoverage(t, pool)
			addLive(t, pool)

			seedCrossChannelPending(t, pool, tc.otherOrg, tc.sessionChannel, tc.sessionStatus)

			result, err := repo.Query(t.Context(), tc.request)
			require.NoError(t, err)
			t.Logf("request=%+v status=%s channels=%+v", tc.request, result.Status, result.Channels)

			got := make(map[string]channelView, len(result.Channels))
			for _, channel := range result.Channels {
				got[channel.ChannelID] = channelView{channel.Reason, channel.Diagnostics}
			}

			require.Equal(t, tc.want, got)
			require.Equal(t, tc.status, result.Status)
			require.Empty(t, result.Items)

			var retained int

			require.NoError(t, pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_live_pending_ends WHERE video_id='livequery01'`).Scan(&retained))
			require.Equal(t, 1, retained)
		})
	}
}
