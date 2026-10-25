package youtubejs

import (
	"context"
	"regexp"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
)

// proofStates는 helper 발급 controller가 보고하는 상태입니다. NONE은 아직 controller가 없는 상태입니다.
var proofStates = []string{"NONE", "COLD", "WARMING", "READY", "UNAVAILABLE", "EXPIRED", "STOPPED"}

// proofErrorCode는 helper가 last_error에 싣는 유한한 오류 코드 형식입니다. 다른 문자열은 label로 쓰지 않습니다.
var proofErrorCode = regexp.MustCompile(`^[a-z_]{1,64}$`)

// EnableMetrics는 scrape마다 helper /health를 읽어 PO 발급 상태를 노출합니다.
// 발급 실패는 helper readiness를 바꾸지 않으므로 /ready 대신 이 메트릭으로 무토큰 저하를 드러냅니다.
// /ready를 주기적으로 호출하지 않는 배포에서도 scrape 시점 값이 되도록 health 결과를 저장하지 않습니다.
func (h *Helper) EnableMetrics(registerer prometheus.Registerer) {
	if registerer == nil {
		registerer = prometheus.DefaultRegisterer
	}

	registerer.MustRegister(&proofCollector{
		helper: h,
		up: prometheus.NewDesc("youtubejs_helper_health_scrape_success",
			"Whether the YouTube.js helper /health response was read and decoded at scrape time.", nil, nil),
		state: prometheus.NewDesc("youtubejs_po_proof_state",
			"Current PO issuance state reported by the helper; NONE means no issuance controller exists yet.", []string{"state"}, nil),
		lastError: prometheus.NewDesc("youtubejs_po_proof_last_error",
			"Last PO issuance error code reported by the helper; absent when cleared.", []string{"code"}, nil),
		bootstrapAttempts: prometheus.NewDesc("youtubejs_po_proof_bootstrap_attempts_total",
			"PO issuer bootstrap attempts since helper start.", nil, nil),
		bootstrapSuccesses: prometheus.NewDesc("youtubejs_po_proof_bootstrap_successes_total",
			"Successful PO issuer bootstraps since helper start.", nil, nil),
		upstreamRequests: prometheus.NewDesc("youtubejs_po_proof_upstream_requests_total",
			"Upstream requests made by PO issuance since helper start.", nil, nil),
		minted: prometheus.NewDesc("youtubejs_po_proof_minted_total",
			"PO tokens minted since helper start.", nil, nil),
		attached: prometheus.NewDesc("youtubejs_po_proof_attached_total",
			"PO tokens the helper attached to player requests since helper start.", nil, nil),
	})
}

type proofCollector struct {
	helper             *Helper
	up                 *prometheus.Desc
	state              *prometheus.Desc
	lastError          *prometheus.Desc
	bootstrapAttempts  *prometheus.Desc
	bootstrapSuccesses *prometheus.Desc
	upstreamRequests   *prometheus.Desc
	minted             *prometheus.Desc
	attached           *prometheus.Desc
}

func (c *proofCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.up, c.state, c.lastError, c.bootstrapAttempts, c.bootstrapSuccesses, c.upstreamRequests, c.minted, c.attached,
	} {
		ch <- desc
	}
}

// Collect는 health 읽기에 실패하면 발급 상태를 추정하지 않고 scrape 실패만 내보냅니다.
func (c *proofCollector) Collect(ch chan<- prometheus.Metric) {
	// scrape에는 요청 context가 없으므로 helper health 제한 시간만 적용합니다.
	body, err := c.helper.readHealth(context.Background())
	if err != nil {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)

		return
	}

	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)

	proof := body.Proof

	current := "NONE"

	if proof != nil {
		current = proof.State
	}

	if !slices.Contains(proofStates, current) {
		// 모르는 상태를 알려진 상태로 바꾸지 않고 따로 드러냅니다.
		ch <- prometheus.MustNewConstMetric(c.state, prometheus.GaugeValue, 1, "UNRECOGNIZED")
	}

	for _, state := range proofStates {
		value := 0.0

		if state == current {
			value = 1
		}

		ch <- prometheus.MustNewConstMetric(c.state, prometheus.GaugeValue, value, state)
	}

	if proof == nil {
		return
	}

	if proof.LastError != "" {
		code := proof.LastError
		if !proofErrorCode.MatchString(code) {
			code = "unrecognized"
		}

		ch <- prometheus.MustNewConstMetric(c.lastError, prometheus.GaugeValue, 1, code)
	}

	for desc, value := range map[*prometheus.Desc]uint64{
		c.bootstrapAttempts:  proof.BootstrapAttempts,
		c.bootstrapSuccesses: proof.BootstrapSuccesses,
		c.upstreamRequests:   proof.UpstreamRequests,
		c.minted:             proof.MintedTotal,
		c.attached:           proof.AttachedTotal,
	} {
		ch <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, float64(value))
	}
}
