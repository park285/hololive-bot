package dispatchoutbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

const (
	insertPlanSendUnitRows = 20000
	insertPlanInputRows    = 10
)

// 전달 행 적재는 입력 행마다 send unit 하나를 unique(unit_key)로 찾아야 한다. 입력 recordset 열 형식이 unit_key의
// character(64)와 다르면 비교가 text로 바뀌어 인덱스를 못 쓰고 보존 기간 안의 send unit 전체를 읽는다.
// 운영 풀은 prepared statement를 재사용하므로 custom plan과 generic plan을 모두 확인한다.
func TestInsertDeliveryBatchLooksUpSendUnitsByUnitKey(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	eventID := seedInsertPlanSendUnits(ctx, t, pool)
	param := insertPlanInput(t, eventID)

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		t.Run(mode, func(t *testing.T) {
			plan, inserted := explainInsertDeliveryBatch(ctx, t, pool, mode, param)
			require.Equal(t, insertPlanInputRows, inserted, "every input row must resolve its send unit")

			stats := insertPlanStats{}
			stats.collect(&plan)

			assert.False(t, stats.seqScans["alarm_dispatch_send_units"], "%s plan must not sequentially scan send units", mode)
			assert.True(t, stats.indexLookups["alarm_dispatch_send_units"], "%s plan must look up send units by index", mode)
			assert.LessOrEqual(t, stats.examinedRows["alarm_dispatch_send_units"], float64(insertPlanInputRows),
				"%s plan read send units beyond the input rows", mode)
		})
	}
}

// seedInsertPlanSendUnits는 delivery가 참조할 event 하나와 보존 기간 안의 send unit 이력을 만든다.
func seedInsertPlanSendUnits(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	var eventID int64

	require.NoError(t, pool.QueryRow(ctx, `INSERT INTO alarm_dispatch_events (event_key, payload_hash, alarm_type, payload)
		VALUES ('insert-plan-event', repeat('a', 64), 'LIVE', '{}'::jsonb)
		RETURNING id`).Scan(&eventID))

	_, err := pool.Exec(ctx, `INSERT INTO alarm_dispatch_send_units (unit_key, dispatch_group_key, room_id, client_request_id)
		SELECT encode(sha256(('insert-plan-unit-' || n)::bytea), 'hex'), 'insert-plan-group-' || n, 'room-insert-plan',
		       'insert-plan-request-' || n
		FROM generate_series(1, $1::int) AS n`, insertPlanSendUnitRows)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `ANALYZE alarm_dispatch_send_units`)
	require.NoError(t, err)

	return eventID
}

// insertPlanInput은 운영 적재와 같은 deliveryBatchRow JSON으로 가장 최근 send unit들을 참조한다.
func insertPlanInput(t *testing.T, eventID int64) string {
	t.Helper()

	var rows []deliveryBatchRow

	for n := insertPlanSendUnitRows - insertPlanInputRows + 1; n <= insertPlanSendUnitRows; n++ {
		unitKey := sha256.Sum256(fmt.Appendf(nil, "insert-plan-unit-%d", n))

		rows = append(rows, deliveryBatchRow{
			EventID:          eventID,
			RoomID:           "room-insert-plan",
			DedupeKey:        fmt.Sprintf("insert-plan-dedupe-%d", n),
			ClaimKeys:        []string{},
			DeliveryContext:  []byte(`{}`),
			DispatchGroupKey: fmt.Sprintf("insert-plan-group-%d", n),
			SendUnitKey:      hex.EncodeToString(unitKey[:]),
			ClientRequestID:  fmt.Sprintf("insert-plan-request-%d", n),
		})
	}

	raw, err := jsonv2.Marshal(rows)
	require.NoError(t, err)

	return jsonbRecordsetParam(raw)
}

func explainInsertDeliveryBatch(ctx context.Context, t *testing.T, pool *pgxpool.Pool, mode, param string) (insertPlanNode, int) {
	t.Helper()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	// EXPLAIN ANALYZE는 delivery를 실제로 넣으므로 다음 모드가 같은 데이터로 계획되게 되돌린다.
	defer func() { require.NoError(t, tx.Rollback(context.WithoutCancel(ctx))) }()

	_, err = tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, "PREPARE insert_delivery_plan AS "+mustSQL("repository_insert_0183_02.sql"))
	require.NoError(t, err)

	// prepared statement는 rollback 뒤에도 연결에 남으므로 풀에 돌려주기 전에 지운다.
	defer func() {
		_, deallocateErr := conn.Exec(context.WithoutCancel(ctx), "DEALLOCATE insert_delivery_plan")
		require.NoError(t, deallocateErr)
	}()

	var planJSON []byte

	literal := "'" + strings.ReplaceAll(param, "'", "''") + "'"
	require.NoError(t, tx.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) EXECUTE insert_delivery_plan("+literal+")").Scan(&planJSON))

	var inserted int

	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE dedupe_key LIKE 'insert-plan-dedupe-%'").Scan(&inserted))

	var plans []struct {
		Plan insertPlanNode `json:"Plan"`
	}

	require.NoError(t, jsonv2.Unmarshal(planJSON, &plans))
	require.Len(t, plans, 1)

	return plans[0].Plan, inserted
}

type insertPlanNode struct {
	NodeType string  `json:"Node Type"`
	Relation string  `json:"Relation Name"`
	Rows     float64 `json:"Actual Rows"`
	Loops    float64 `json:"Actual Loops"`
	// 필터와 lossy bitmap 재검사가 버린 행도 heap에서 읽은 행이다.
	RemovedByFilter  float64          `json:"Rows Removed by Filter"`
	RemovedByRecheck float64          `json:"Rows Removed by Index Recheck"`
	Plans            []insertPlanNode `json:"Plans"`
}

type insertPlanStats struct {
	seqScans     map[string]bool
	indexLookups map[string]bool
	examinedRows map[string]float64
}

func (s *insertPlanStats) collect(node *insertPlanNode) {
	if s.seqScans == nil {
		s.seqScans, s.indexLookups, s.examinedRows = map[string]bool{}, map[string]bool{}, map[string]float64{}
	}

	if node.Relation != "" && node.NodeType != "ModifyTable" {
		s.examinedRows[node.Relation] += (node.Rows + node.RemovedByFilter + node.RemovedByRecheck) * max(node.Loops, 1)

		s.seqScans[node.Relation] = s.seqScans[node.Relation] || node.NodeType == "Seq Scan"
		s.indexLookups[node.Relation] = s.indexLookups[node.Relation] ||
			node.NodeType == "Index Scan" || node.NodeType == "Index Only Scan" || node.NodeType == "Bitmap Heap Scan"
	}

	for i := range node.Plans {
		s.collect(&node.Plans[i])
	}
}
