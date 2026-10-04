//go:build integration

package dispatchops

import (
	"errors"
	"testing"
)

func TestOpsIntegrationSettlement(t *testing.T) {
	for _, action := range []string{"cancel", "quarantine"} {
		for _, grouped := range []bool{true, false} {
			t.Run(action+map[bool]string{true: "_group", false: "_legacy"}[grouped], func(t *testing.T) {
				repo, pool := setupOpsIntegration(t)
				ids := seedOpsGroup(t, pool, 2, grouped)
				detail, err := repo.Detail(t.Context(), ids[0])
				if err != nil {
					t.Fatal(err)
				}
				request := SettleRequest{OperatorID: "operator-1", Reason: "더 이상 발송하지 않음", Action: action}
				for _, item := range detail.Group {
					request.Targets = append(request.Targets, Revision{ID: item.ID, UpdatedAt: item.UpdatedAt})
				}
				stale := request
				stale.Targets = append([]Revision(nil), request.Targets...)
				stale.Targets[0].UpdatedAt = stale.Targets[0].UpdatedAt.Add(-1)
				if _, err := repo.Settle(t.Context(), ids[0], stale); !errors.Is(err, ErrConflict) {
					t.Fatalf("stale: %v", err)
				}
				if grouped {
					partial := request
					partial.Targets = request.Targets[:1]
					if _, err := repo.Settle(t.Context(), ids[0], partial); !errors.Is(err, ErrConflict) {
						t.Fatalf("partial: %v", err)
					}
				}
				result, err := repo.Settle(t.Context(), ids[0], request)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.IDs) != len(detail.Group) {
					t.Fatal(result)
				}
				if _, err := repo.Settle(t.Context(), ids[0], request); !errors.Is(err, ErrConflict) {
					t.Fatalf("duplicate: %v", err)
				}
				updated, err := repo.Detail(t.Context(), ids[0])
				if err != nil {
					t.Fatal(err)
				}
				expected := "quarantined"
				if action == "cancel" {
					expected = "cancelled"
				}
				for _, item := range updated.Group {
					if item.Status != expected || item.AttemptCount != 4 || item.ErrorCode != "SEND_FAILED" || item.SentAt != nil || (action == "cancel" && item.CancelledAt == nil) || (action == "quarantine" && item.QuarantinedAt == nil) {
						t.Fatalf("delivery: %+v", item)
					}
					history, err := repo.Actions(t.Context(), item.ID, "")
					if err != nil {
						t.Fatal(err)
					}
					if len(history.Items) != 1 {
						t.Fatal(history)
					}
					audit := history.Items[0]
					if audit.Action != "manual_"+action || audit.OperatorID != request.OperatorID || audit.Reason != request.Reason || audit.DuplicateRiskAck || audit.FromStatus != "dlq" || audit.ToStatus != expected {
						t.Fatal(audit)
					}
				}
				if action == "cancel" {
					if _, err := repo.Requeue(t.Context(), ids[0], RequeueRequest{OperatorID: request.OperatorID, Reason: request.Reason, DuplicateRiskAck: true, Targets: request.Targets}); !errors.Is(err, ErrConflict) {
						t.Fatalf("cancelled replay: %v", err)
					}
				}
				if action == "quarantine" && grouped {
					if _, err := repo.Requeue(t.Context(), ids[0], requestFromDetail(updated)); err != nil {
						t.Fatalf("quarantine replay: %v", err)
					}
				}
			})
		}
	}
}

// 재시도 뒤 다시 격리한 행의 보존 기간은 새 격리부터 시작하며,
// 묶음 안에서 이미 격리 중인 행의 시각을 임의로 연장하지 않습니다.
func TestOpsIntegrationRequarantineStartsNewRetention(t *testing.T) {
	repo, pool := setupOpsIntegration(t)
	ids := seedOpsGroup(t, pool, 2, true)
	ctx := t.Context()
	if _, err := pool.Exec(ctx, `UPDATE alarm_dispatch_deliveries SET quarantined_at=now()-interval '100 days',
  status=CASE WHEN id=$1::bigint THEN 'dlq' ELSE 'quarantined' END`, ids[0]); err != nil {
		t.Fatal(err)
	}
	before, err := repo.Detail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	request := SettleRequest{OperatorID: "operator", Reason: "실패 원인 조사", Action: "quarantine", Targets: before.ReplayTargets}
	if _, err := repo.Settle(ctx, ids[0], request); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Detail(ctx, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	for index := range after.Group {
		old, item := &before.Group[index], &after.Group[index]
		if old.QuarantinedAt == nil || item.QuarantinedAt == nil {
			t.Fatal("missing quarantine timestamp")
		}
		if item.ID == ids[0] && !item.QuarantinedAt.After(old.UpdatedAt) {
			t.Fatal("new quarantine inherited an expired retention clock")
		}
		if item.ID == ids[1] && !item.QuarantinedAt.Equal(*old.QuarantinedAt) {
			t.Fatal("existing quarantine retention extended")
		}
	}
}
