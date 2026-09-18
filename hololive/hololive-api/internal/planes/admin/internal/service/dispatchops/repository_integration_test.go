//go:build integration

package dispatchops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 기존 dispatchoutbox 통합 테스트와 같은 마이그레이션 정본을 사용합니다.
func setupOpsIntegration(t *testing.T) (*Repository, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	setup, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{fmt.Sprintf("dispatchops_test_%d", time.Now().UnixNano())}.Sanitize()
	if _, err := setup.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		setup.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := setup.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
		setup.Close()
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET search_path TO "+schema)
		if err != nil {
			return fmt.Errorf("set integration schema: %w", err)
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "CREATE TYPE alarm_type AS ENUM ('LIVE', 'COMMUNITY', 'SHORTS')"); err != nil {
		t.Fatal(err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("source path unavailable")
	}
	root := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(root, "hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations/058_create_alarm_dispatch_outbox.sql")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("repository root not found")
		}
		root = parent
	}
	for _, migration := range []string{
		"hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations/058_create_alarm_dispatch_outbox.sql",
		"hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations/059_harden_alarm_dispatch_outbox.sql",
		"hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations/065_record_alarm_dispatch_event_collisions.sql",
		"hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations/118_alarm_dispatch_state_shape_check.sql",
		"hololive/hololive-shared/pkg/service/alarm/dispatchoutbox/testdata/epoch1_migrations/122_alarm_dispatch_last_error_size_check.sql",
		"hololive/hololive-api/scripts/migrations/141_alarm_dispatch_send_units.sql",
		"hololive/hololive-api/scripts/migrations/142_alarm_dispatch_send_unit_due_index.sql",
		"hololive/hololive-api/scripts/migrations/143_alarm_dispatch_send_unit_index.sql",
	} {
		data, err := os.ReadFile(filepath.Join(root, migration))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}
	return NewRepository(pool), pool
}

func seedOpsGroup(t *testing.T, pool *pgxpool.Pool, count int, grouped bool) []string {
	t.Helper()
	ctx := t.Context()
	var eventID int64
	err := pool.QueryRow(ctx, `INSERT INTO alarm_dispatch_events(event_key,payload_hash,alarm_type,payload)
 VALUES ('test-event',repeat('a',64),'LIVE','{}') RETURNING id`).Scan(&eventID)
	if err != nil {
		t.Fatal(err)
	}
	var unit any
	var group any
	if grouped {
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO alarm_dispatch_send_units(unit_key,dispatch_group_key,room_id,client_request_id)
   VALUES(repeat('b',64),'ops-group','9007199254740993','ops-test-request-0001') RETURNING id`).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		unit = id
		group = "ops-group"
	}
	ids := make([]string, 0, count)
	for i := 0; i < count; i++ {
		var id string
		err := pool.QueryRow(ctx, `INSERT INTO alarm_dispatch_deliveries
   (event_id,room_id,dedupe_key,dispatch_group_key,send_unit_id,status,attempt_count,dlq_at,last_error,last_error_code)
   VALUES($1,'9007199254740993',$2,$3,$4,'dlq',4,now(),$5,'SEND_FAILED') RETURNING id::text`,
			eventID, fmt.Sprintf("ops-delivery-%d", i), group, unit, strings.Repeat("e", 8192)).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func requestFromDetail(detail Detail) RequeueRequest {
	return RequeueRequest{OperatorID: "operator-1", Reason: "원인 수정 후 확인", DuplicateRiskAck: true, Targets: detail.ReplayTargets}
}

func TestOpsIntegrationGroupReplayAndAudit(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	ids := seedOpsGroup(t, pool, 2, true)
	ctx := t.Context()
	detail, err := repo.Detail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if detail.ReplayBlocked != "" || len(detail.ReplayTargets) != 2 || len(detail.Group) != 2 {
		t.Fatalf("detail: %+v", detail)
	}
	request := requestFromDetail(detail)
	partial := request
	partial.Targets = request.Targets[:1]
	if _, err := repo.Requeue(ctx, ids[0], partial); !errors.Is(err, ErrConflict) {
		t.Fatalf("partial replay: %v", err)
	}
	result, err := repo.Requeue(ctx, ids[0], request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.IDs) != 2 {
		t.Fatalf("replayed %v", result.IDs)
	}
	if _, err := repo.Requeue(ctx, ids[0], request); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate replay: %v", err)
	}
	for _, id := range ids {
		after, err := repo.Detail(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if after.Delivery.Status != "retry" || after.Delivery.AttemptCount != 4 || after.Delivery.SendUnitID != detail.Delivery.SendUnitID || !after.Delivery.UpdatedAt.After(detail.Delivery.UpdatedAt) {
			t.Fatalf("after: %+v", after)
		}
		actions, err := repo.Actions(ctx, id, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(actions.Items) != 1 || actions.Items[0].FromStatus != "dlq" || actions.Items[0].ToStatus != "retry" || !actions.Items[0].DuplicateRiskAck || actions.Items[0].Reason != request.Reason {
			t.Fatalf("audit: %+v", actions)
		}
	}
	var preserved int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM alarm_dispatch_deliveries d JOIN alarm_dispatch_send_units u ON u.id=d.send_unit_id
  WHERE octet_length(last_error)=8192 AND attempt_count=4 AND locked_by IS NULL AND locked_at IS NULL AND lock_expires_at IS NULL
  AND u.client_request_id='ops-test-request-0001' AND d.dedupe_key LIKE 'ops-delivery-%'`).Scan(&preserved); err != nil {
		t.Fatal(err)
	}
	if preserved != 2 {
		t.Fatalf("history/identity modified: %d", preserved)
	}
}

func TestOpsIntegrationAuditFailureRollsBack(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	ids := seedOpsGroup(t, pool, 2, true)
	ctx := t.Context()
	detail, err := repo.Detail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "ALTER TABLE alarm_dispatch_admin_actions ADD CONSTRAINT test_reject_audit CHECK (false)"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Requeue(ctx, ids[0], requestFromDetail(detail)); err == nil {
		t.Fatal("audit failure accepted")
	}
	var failures int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alarm_dispatch_deliveries WHERE status='dlq'").Scan(&failures); err != nil {
		t.Fatal(err)
	}
	if failures != 2 {
		t.Fatalf("partial update after audit failure: %d", failures)
	}
}

func TestOpsIntegrationConcurrentReplaySingleWinner(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	ids := seedOpsGroup(t, pool, 2, true)
	detail, err := repo.Detail(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	request := requestFromDetail(detail)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := repo.Requeue(t.Context(), ids[0], request); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded, conflicts := 0, 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", succeeded, conflicts)
	}
	var audits int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM alarm_dispatch_admin_actions").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("duplicate audits: %d", audits)
	}
}

func TestOpsIntegrationPaginationAndLargeIDs(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	if _, err := pool.Exec(t.Context(), "SELECT setval('alarm_dispatch_deliveries_id_seq',9007199254740992)"); err != nil {
		t.Fatal(err)
	}
	ids := seedOpsGroup(t, pool, PageSize+1, false)
	first, err := repo.List(t.Context(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != PageSize || first.Items[0].ID != ids[len(ids)-1] || first.NextBeforeID == "" {
		t.Fatalf("page: %+v", first)
	}
	second, err := repo.List(t.Context(), Filter{BeforeID: first.NextBeforeID})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != ids[0] || second.NextBeforeID != "" {
		t.Fatalf("second: %+v", second)
	}
	filtered, err := repo.List(t.Context(), Filter{Status: "sent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Items) != 0 {
		t.Fatal("status filter ignored")
	}
	summary, err := repo.Summary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Counts) != 1 || summary.Counts[0].Count != "51" {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestOpsIntegrationStaleRevisionAndSentSibling(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	ids := seedOpsGroup(t, pool, 2, true)
	ctx := t.Context()
	detail, err := repo.Detail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE alarm_dispatch_deliveries SET updated_at=updated_at+interval '1 microsecond' WHERE id::text=$1", ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Requeue(ctx, ids[0], requestFromDetail(detail)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replay: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE alarm_dispatch_deliveries SET status='sent',sent_at=now() WHERE id::text=$1", ids[1]); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Detail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if after.ReplayBlocked != "group_not_terminal_failure" || len(after.ReplayTargets) != 0 {
		t.Fatalf("sent sibling allowed: %+v", after)
	}
}

func TestOpsIntegrationActionPaginationUsesNumericOrder(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	ids := seedOpsGroup(t, pool, 1, false)
	id, err := ParseID(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(t.Context(), `INSERT INTO alarm_dispatch_admin_actions
  (delivery_id,action,operator_id,reason,from_status,to_status,duplicate_risk_ack)
  SELECT $1,'manual_requeue','test-operator','pagination test','dlq','retry',true
  FROM generate_series(1,51)`, id)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.Actions(t.Context(), ids[0], "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != PageSize || first.Items[0].ID != "51" || first.NextBeforeID != "2" {
		t.Fatalf("first: %+v", first)
	}
	second, err := repo.Actions(t.Context(), ids[0], first.NextBeforeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != "1" || second.NextBeforeID != "" {
		t.Fatalf("second: %+v", second)
	}
}
