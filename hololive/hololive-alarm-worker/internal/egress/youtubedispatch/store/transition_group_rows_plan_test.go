package store

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

type groupRowsQueryPlan struct {
	NodeType    string               `json:"Node Type"`
	SubplanName string               `json:"Subplan Name"`
	CTEName     string               `json:"CTE Name"`
	ActualLoops float64              `json:"Actual Loops"`
	Plans       []groupRowsQueryPlan `json:"Plans"`
}

func TestTransitionLogicalGroupRowsPreparedPlanBoundsRequestScans(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedGroupQueryPlanRows(t, pool)

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		for _, count := range []int{1, 50, 100} {
			t.Run(fmt.Sprintf("%s/tuples-%d", mode, count), func(t *testing.T) {
				assertGroupQueryPreparedPlan(t, pool, mode, count)
			})
		}
	}
}

func seedGroupQueryPlanRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO youtube_notification_outbox (kind, channel_id, content_id, payload)
		SELECT 'NEW_VIDEO', 'channel-group-plan',
			CASE WHEN n > 19900 THEN ' plan-video-' || (n - 19900) || ' '
				ELSE 'plan-video-' || n END,
			'{}'::jsonb
		FROM generate_series(1, 20000) AS n`)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), `
		INSERT INTO youtube_notification_delivery (outbox_id, room_id, status, created_at)
		SELECT outbox.id, 'plan-room-' || room, 'PENDING', now() - interval '1 minute'
		FROM youtube_notification_outbox AS outbox
		CROSS JOIN generate_series(0, 4) AS room
		WHERE outbox.channel_id = 'channel-group-plan'`)
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), "ANALYZE youtube_notification_outbox; ANALYZE youtube_notification_delivery")
	require.NoError(t, err)
}

func assertGroupQueryPreparedPlan(t *testing.T, pool *pgxpool.Pool, mode string, count int) {
	t.Helper()

	execute, wanted := groupQueryExecuteFixture(t, pool, count)
	tx, err := pool.Begin(t.Context())
	require.NoError(t, err)

	defer func() { require.NoError(t, tx.Rollback(t.Context())) }()

	_, err = tx.Exec(t.Context(), "SELECT set_config('plan_cache_mode', $1, true)", mode)
	require.NoError(t, err)

	// EXPLAIN 자체의 bind가 아니라 실제 SELECT를 PREPARE해야 generic 계획도 검증한다.
	_, err = tx.Exec(t.Context(), "PREPARE group_rows_plan(bigint[], text[], text[], text[], bigint) AS "+mustSQL("transition_group_rows.sql"))
	require.NoError(t, err)

	defer func() {
		_, deallocateErr := tx.Exec(t.Context(), "DEALLOCATE group_rows_plan")
		require.NoError(t, deallocateErr)
	}()

	var raw []byte

	err = tx.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+execute).Scan(&raw)
	require.NoError(t, err)

	var documents []struct {
		Plan          groupRowsQueryPlan `json:"Plan"`
		ExecutionTime float64            `json:"Execution Time"`
	}

	require.NoError(t, jsonv2.Unmarshal(raw, &documents))
	require.Len(t, documents, 1)
	assertGroupQueryRequestScans(t, documents[0].Plan)

	// 계획의 row 수만으로 동등성을 주장하지 않고 실제 정렬된 ID 전체를 확인한다.
	var rows []transitionRow

	err = deliverysql.SelectDeliverySQL(t.Context(), tx, &rows, "prepared group SQL result", execute)
	require.NoError(t, err)
	require.Equal(t, wanted, groupQueryRowIDs(rows))
	t.Logf("prepared %s: tuples=%d tuple-only=%d direct-only=1 ids=%d/order match; execution=%.3fms",
		mode, count, count, len(wanted), documents[0].ExecutionTime)
}

func assertGroupQueryRequestScans(t *testing.T, plan groupRowsQueryPlan) {
	t.Helper()

	pending := []groupRowsQueryPlan{plan}
	requestScans := 0

	for len(pending) > 0 {
		node := pending[len(pending)-1]

		pending = pending[:len(pending)-1]
		pending = append(pending, node.Plans...)

		require.False(t, strings.HasPrefix(node.SubplanName, "SubPlan"), "correlated request subplan returned")

		if node.NodeType == "Function Scan" || (node.NodeType == "CTE Scan" && node.CTEName == "requested") {
			requestScans++

			require.LessOrEqual(t, node.ActualLoops, float64(1), "request relation was evaluated once per delivery: %+v", node)
		}
	}

	require.Positive(t, requestScans, "prepared plan must include the actual request relation")
}

func groupQueryExecuteFixture(t *testing.T, pool *pgxpool.Pool, count int) (string, []int64) {
	t.Helper()

	// 요청 tuple과 방도 identity도 다른 행은 direct ID 분기에서만 반환되어야 한다.
	directOnlyID := groupQueryPlanDeliveryID(t, pool, "plan-video-101", "plan-room-4")
	wanted := make([]int64, 0, count*2+1)
	idLiterals := make([]string, 0, count+1)
	rooms := make([]string, 0, count)
	kinds := make([]string, 0, count)
	candidates := make([]string, 0, count)

	wanted = append(wanted, directOnlyID)
	idLiterals = append(idLiterals, strconv.FormatInt(directOnlyID, 10))

	for index := range count {
		contentID := fmt.Sprintf("plan-video-%d", index+1)
		id := groupQueryPlanDeliveryID(t, pool, contentID, "plan-room-0")
		siblingID := groupQueryPlanDeliveryID(t, pool, " "+contentID+" ", "plan-room-0")

		// 공백을 제거하면 같은 identity인 sibling은 direct IDs에 넣지 않는다.
		wanted = append(wanted, id, siblingID)
		idLiterals = append(idLiterals, strconv.FormatInt(id, 10))
		rooms = append(rooms, "'plan-room-0'")
		kinds = append(kinds, "'NEW_VIDEO'")
		candidates = append(candidates, "'"+contentID+"'")
	}

	slices.Sort(wanted)

	// 후보 없는 direct-only 요청 1개도 group 수에 포함한다. LogicalGroupLimit는 production과 같은 100이다.
	// 모든 SQL literal은 이 합성 fixture 소유다.
	limit := (count+1)*(100+1) + len(idLiterals)
	execute := fmt.Sprintf("EXECUTE group_rows_plan(ARRAY[%s]::bigint[], ARRAY[%s]::text[], ARRAY[%s]::text[], ARRAY[%s]::text[], %d)",
		strings.Join(idLiterals, ","), strings.Join(rooms, ","), strings.Join(kinds, ","), strings.Join(candidates, ","), limit)

	return execute, wanted
}

func groupQueryPlanDeliveryID(t *testing.T, pool *pgxpool.Pool, contentID, roomID string) int64 {
	t.Helper()

	var id int64

	err := pool.QueryRow(t.Context(), `
		SELECT delivery.id
		FROM youtube_notification_delivery AS delivery
		JOIN youtube_notification_outbox AS outbox ON outbox.id = delivery.outbox_id
		WHERE outbox.content_id = $1 AND delivery.room_id = $2`, contentID, roomID).Scan(&id)
	require.NoError(t, err)

	return id
}
