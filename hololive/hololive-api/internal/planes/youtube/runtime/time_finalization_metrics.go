package runtime

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

type timeFinalizationSample struct {
	metric, label, detail string
	value                 int64
}

type timeFinalizationMetrics struct {
	outcomes, queue, delay *prometheus.GaugeVec
}

func newTimeFinalizationMetrics(reg prometheus.Registerer) *timeFinalizationMetrics {
	m := &timeFinalizationMetrics{
		outcomes: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hololive_youtube_collection_time_finalization_outcomes",
			Help: "Retained time finalization rows by outcome and last reason; not a cumulative event counter.",
		}, []string{"outcome", "reason"}),
		queue: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hololive_youtube_collection_time_finalization_queue_length",
			Help: "New eligible ended sessions before a terminal result, and all pending backlog rows regardless of batch activation.",
		}, []string{"queue"}),
		delay: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "hololive_youtube_collection_time_finalization_recent_finalized",
			Help: "Finalizations completed in the last 24 hours whose end-to-finalization delay is at most le seconds (cumulative); new starts at session status observation, backlog at retry base. Gauge, not a lifetime histogram.",
		}, []string{"queue", "le"}),
	}
	reg.MustRegister(m.outcomes, m.queue, m.delay)

	return m
}

func (r *Runtime) loadTimeFinalizationSamples(ctx context.Context) ([]timeFinalizationSample, error) {
	rows, err := r.pool.Query(ctx, mustSQL("time_finalization_observability.sql"))
	if err != nil {
		return nil, fmt.Errorf("query time finalization snapshot: %w", err)
	}
	defer rows.Close()

	return scanTimeFinalizationSamples(rows)
}

func scanTimeFinalizationSamples(rows pgx.Rows) ([]timeFinalizationSample, error) {
	var samples []timeFinalizationSample

	for rows.Next() {
		var s timeFinalizationSample

		if err := rows.Scan(&s.metric, &s.label, &s.detail, &s.value); err != nil {
			return nil, fmt.Errorf("scan time finalization snapshot: %w", err)
		}

		samples = append(samples, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate time finalization snapshot: %w", err)
	}

	return samples, nil
}

func (m *timeFinalizationMetrics) observe(samples []timeFinalizationSample) {
	// 최종 상태·사유 변경으로 사라진 조합을 남기지 않는다. 실패한 조회는 이 함수를 호출하지 않는다.
	m.outcomes.Reset()

	for _, s := range samples {
		switch s.metric {
		case "outcomes":
			m.outcomes.WithLabelValues(s.label, s.detail).Set(float64(s.value))
		case "queue":
			m.queue.WithLabelValues(s.label).Set(float64(s.value))
		case "delay":
			m.delay.WithLabelValues(s.label, s.detail).Set(float64(s.value))
		}
	}
}
