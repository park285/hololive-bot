package youtubejs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 발급이 계속 실패해도 helper는 READY이므로 /ready 대신 scrape 시점의 PO 상태 메트릭으로 드러나야 한다.
func TestHLP014(t *testing.T) {
	base := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	defer cancel()

	helper, _, err := Start(ctx, fixtureConfig(t, base, "proof-unavailable"))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	reg := prometheus.NewPedanticRegistry()
	helper.EnableMetrics(reg)

	const expected = `
# HELP youtubejs_helper_health_scrape_success Whether the YouTube.js helper /health response was read and decoded at scrape time.
# TYPE youtubejs_helper_health_scrape_success gauge
youtubejs_helper_health_scrape_success 1
# HELP youtubejs_po_proof_attached_total PO tokens the helper attached to player requests since helper start.
# TYPE youtubejs_po_proof_attached_total counter
youtubejs_po_proof_attached_total 5
# HELP youtubejs_po_proof_bootstrap_attempts_total PO issuer bootstrap attempts since helper start.
# TYPE youtubejs_po_proof_bootstrap_attempts_total counter
youtubejs_po_proof_bootstrap_attempts_total 3
# HELP youtubejs_po_proof_bootstrap_successes_total Successful PO issuer bootstraps since helper start.
# TYPE youtubejs_po_proof_bootstrap_successes_total counter
youtubejs_po_proof_bootstrap_successes_total 1
# HELP youtubejs_po_proof_last_error Last PO issuance error code reported by the helper; absent when cleared.
# TYPE youtubejs_po_proof_last_error gauge
youtubejs_po_proof_last_error{code="challenge_structure"} 1
# HELP youtubejs_po_proof_minted_total PO tokens minted since helper start.
# TYPE youtubejs_po_proof_minted_total counter
youtubejs_po_proof_minted_total 2
# HELP youtubejs_po_proof_state Current PO issuance state reported by the helper; NONE means no issuance controller exists yet.
# TYPE youtubejs_po_proof_state gauge
youtubejs_po_proof_state{state="COLD"} 0
youtubejs_po_proof_state{state="EXPIRED"} 0
youtubejs_po_proof_state{state="NONE"} 0
youtubejs_po_proof_state{state="READY"} 0
youtubejs_po_proof_state{state="STOPPED"} 0
youtubejs_po_proof_state{state="UNAVAILABLE"} 1
youtubejs_po_proof_state{state="WARMING"} 0
# HELP youtubejs_po_proof_upstream_requests_total Upstream requests made by PO issuance since helper start.
# TYPE youtubejs_po_proof_upstream_requests_total counter
youtubejs_po_proof_upstream_requests_total 4
`

	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected)); err != nil {
		t.Fatal(err)
	}

	if err := helper.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 종료된 helper는 상태를 추정하지 않고 읽기 실패만 드러낸다.
	const closed = `
# HELP youtubejs_helper_health_scrape_success Whether the YouTube.js helper /health response was read and decoded at scrape time.
# TYPE youtubejs_helper_health_scrape_success gauge
youtubejs_helper_health_scrape_success 0
`

	if err := testutil.GatherAndCompare(reg, strings.NewReader(closed)); err != nil {
		t.Fatal(err)
	}

	assertNoResidue(t, base)
}
