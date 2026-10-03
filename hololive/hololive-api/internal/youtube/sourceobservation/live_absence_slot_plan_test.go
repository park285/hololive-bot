package sourceobservation

import (
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

type absenceSlotPlanNode struct {
	Relation      string                `json:"Relation Name"`
	Rows          float64               `json:"Actual Rows"`
	Loops         float64               `json:"Actual Loops"`
	FilterRemoved float64               `json:"Rows Removed by Filter"`
	RecheckRemove float64               `json:"Rows Removed by Index Recheck"`
	Plans         []absenceSlotPlanNode `json:"Plans"`
}

func TestLiveAbsenceCurrentSlotLookupIsBoundedBySlotNotChannelHistory(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	current := time.Date(2026, time.September, 26, 0, 0, 0, 0, time.UTC)

	// 운영처럼 slot마다 채널 하나만 담기므로 채널 GIN 조건은 해당 채널의 전체 이력과 일치합니다.
	_, err := pool.Exec(ctx, `
		INSERT INTO youtube_live_absence_slots
		SELECT g, $1::timestamptz - g * INTERVAL '1 minute', repeat('a', 64),
		       $1::timestamptz - g * INTERVAL '1 minute', $1::timestamptz, repeat('b', 64),
		       jsonb_build_object('requested_channel_ids', jsonb_build_array($2::text))
		FROM generate_series(0, 20000) AS g
	`, current, testChannelID)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, "ANALYZE youtube_live_absence_slots")
	require.NoError(t, err)

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			require.NoError(t, err)

			defer func() { require.NoError(t, tx.Rollback(ctx)) }()

			_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
			require.NoError(t, err)

			var raw []byte

			// 새 positive 채널이 없는 정상 consume은 현재 slot의 coverage만 다시 읽습니다.
			err = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+mustSQL("repository_live_absence_slots.sql"),
				[]string{}, current, current, []string{testChannelID}).Scan(&raw)
			require.NoError(t, err)

			var plans []struct {
				Plan absenceSlotPlanNode `json:"Plan"`
			}

			require.NoError(t, jsonv2.Unmarshal(raw, &plans))
			require.Len(t, plans, 1)
			require.LessOrEqual(t, absenceSlotVisits(plans[0].Plan), float64(16),
				"current slot lookup must read the scheduled slot, not the retained absence history of its channel")
		})
	}
}

func absenceSlotVisits(node absenceSlotPlanNode) float64 {
	visits := float64(0)

	if node.Relation == "youtube_live_absence_slots" {
		visits = (node.Rows + node.FilterRemoved + node.RecheckRemove) * node.Loops
	}

	for _, child := range node.Plans {
		visits += absenceSlotVisits(child)
	}

	return visits
}
