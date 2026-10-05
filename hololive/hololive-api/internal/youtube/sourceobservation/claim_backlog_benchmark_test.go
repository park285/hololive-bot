package sourceobservation

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
)

// BenchmarkClaimBacklog는 활성 backlog와 PROCESSED 이력 규모별 claim 1회 실행 시간과 CTE별 읽은 행을 재는 수동 측정
// 도구이며 gate가 아니다. 결과는 db-hotpath 계획(docs/current/plans/2026-10-05-db-hotpath-optimization.md)에 남긴다.
//
//	go test -run '^$' -bench BenchmarkClaimBacklog -benchtime 3x ./internal/youtube/sourceobservation
func BenchmarkClaimBacklog(b *testing.B) {
	kinds := []string{"video_list", "shorts_list"}
	ctes := map[string]map[string]bool{
		"active-rows":    claimBacklogActiveCTEs,
		"exhausted-rows": {"CTE exhausted_candidates": true},
		"replay-rows":    {"CTE replay_expired_candidates": true},
		"candidate-rows": claimBacklogCandidateCTEs,
	}

	for _, active := range []int{5000, 50000, 200000} {
		for _, history := range []int{0, 200000} {
			b.Run(fmt.Sprintf("active=%d/history=%d", active, history), func(b *testing.B) {
				pool := dbtest.NewPool(b)
				seedClaimBacklogRows(b, pool, active, history)

				for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
					b.Run(mode, func(b *testing.B) {
						var (
							plan        claimBacklogPlanNode
							executionMS float64
						)

						for b.Loop() {
							raw, err := explainClaimBacklog(b.Context(), pool, mode, "60s", kinds)
							require.NoError(b, err)

							var plans []struct {
								Plan          claimBacklogPlanNode `json:"Plan"`
								ExecutionTime float64              `json:"Execution Time"`
							}

							require.NoError(b, jsonv2.Unmarshal(raw, &plans))
							require.Len(b, plans, 1)

							plan = plans[0].Plan

							executionMS += plans[0].ExecutionTime
						}

						b.ReportMetric(executionMS/float64(b.N), "exec-ms/op")

						for name, scope := range ctes {
							b.ReportMetric(claimBacklogVisits(&plan, scope, false), name)
						}
					})
				}
			})
		}
	}
}
