package livequery

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// reducer는 종결됐거나 positive가 이긴 종료 증거도 보존할 수 있다.
func TestRepositoryRetainedPendingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, stateSQL, offset, kind, channel string
		status                                Status
		reason                                Reason
		items                                 int
	}{
		{name: "newer repeated end after terminal state", stateSQL: `UPDATE youtube_live_sessions SET status='ENDED'; UPDATE youtube_live_reconciliation_heads SET status='ENDED'`, offset: "1 second", status: Complete, reason: Covered},
		{name: "older end loses to live positive", offset: "-1 second", status: Complete, reason: Covered, items: 1},
		{name: "same-time end loses to live positive", offset: "0 seconds", status: Complete, reason: Covered, items: 1},
		{name: "newer end remains unresolved", offset: "1 second", status: Unavailable, reason: ConfirmingEnd},
		{name: "upcoming positive defeats older cancel", stateSQL: `UPDATE youtube_live_sessions SET status='UPCOMING'; UPDATE youtube_live_reconciliation_heads SET status='UPCOMING',last_upcoming_positive_at=last_live_positive_at,last_live_positive_at=NULL`, offset: "-1 second", kind: "EXPLICIT_CANCEL", status: Complete, reason: Covered},
		{name: "newer cancel remains unresolved", stateSQL: `UPDATE youtube_live_sessions SET status='UPCOMING'; UPDATE youtube_live_reconciliation_heads SET status='UPCOMING',last_upcoming_positive_at=last_live_positive_at,last_live_positive_at=NULL`, offset: "1 second", kind: "EXPLICIT_CANCEL", status: Unavailable, reason: ConfirmingEnd},
		{name: "channel mismatch survives obsolete end", offset: "-1 second", channel: "UC_other", status: Unavailable, reason: Inconsistent},
		{name: "status mismatch survives obsolete end", stateSQL: `UPDATE youtube_live_sessions SET status='ENDED'`, offset: "-1 second", status: Unavailable, reason: Inconsistent},
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
 SELECT video_id,$1,$2,900002,COALESCE(last_live_positive_at,last_upcoming_positive_at)+$3::interval,now(),now(),true,true
 FROM youtube_live_reconciliation_heads WHERE video_id='livequery01'`, tc.channel, tc.kind, tc.offset)
			require.NoError(t, err)

			result, err := repo.Query(t.Context(), Request{Scope: All, Limit: MaxItems})
			require.NoError(t, err)
			require.Equal(t, tc.status, result.Status)
			require.Equal(t, tc.reason, result.Channels[0].Reason)
			require.Len(t, result.Items, tc.items)
		})
	}
}
