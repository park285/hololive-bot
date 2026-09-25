package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

type collectionTargetSample struct {
	kind                                            string
	valid                                           bool
	targets, neverCompleted, stale, due             int64
	oldestCompletionAge, oldestDueAge, requiredRate float64
	live, upcoming, other                           int64
	stateMismatch, pastDue, pastDue7d               int64
}

type collectionTargetMetrics struct {
	targets, neverCompleted, stale, due             *prometheus.GaugeVec
	oldestCompletionAge, oldestDueAge, requiredRate *prometheus.GaugeVec
	liveState                                       *prometheus.GaugeVec
	liveReview                                      *prometheus.GaugeVec
	success, lastSuccess                            prometheus.Gauge
}

var youtubeCollectionTargets = newCollectionTargetMetrics(prometheus.DefaultRegisterer)

func newCollectionTargetMetrics(reg prometheus.Registerer) *collectionTargetMetrics {
	gauge := func(suffix, help string) *prometheus.GaugeVec {
		v := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_" + suffix, Help: help}, []string{"kind"})
		reg.MustRegister(v)

		return v
	}
	m := &collectionTargetMetrics{
		targets:             gauge("targets", "Current valid enabled YouTube.js collection subjects, bundled per job."),
		neverCompleted:      gauge("never_completed_targets", "Active subjects without a completed collection lease; not a zero age."),
		stale:               gauge("stale_targets", "Active subjects whose last completed YouTube.js collection is older than the configured polling interval; excludes never-completed subjects."),
		due:                 gauge("due_targets", "Active subjects eligible for discovery, including those not in any AP local queue."),
		oldestCompletionAge: gauge("oldest_completion_age_seconds", "Maximum age of a completed collection among active subjects; see never_completed_targets separately."),
		oldestDueAge:        gauge("oldest_due_age_seconds", "Maximum elapsed time past effective discovery due time among active subjects."),
		requiredRate:        gauge("required_rpc_rate", "Nominal YouTube.js helper RPC calls per second required by active target polling intervals; excludes retries."),
		liveState:           prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_live_states", Help: "Distinct live-session videos on enabled, unexpired live_snapshot channel targets in a current, unexpired projection where the head or product session is LIVE/UPCOMING, by reconciliation head state; other includes missing or terminal heads."}, []string{"state"}),
		liveReview:          prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_live_state_review_targets", Help: "Distinct live-session videos on enabled, unexpired live_snapshot channel targets in a current, unexpired projection with a LIVE/UPCOMING head or product session requiring review: head/product mismatch or an UPCOMING head with product schedule before now/over 7 days overdue; reasons overlap and do not prove a broadcast ended."}, []string{"reason"}),
		success:             prometheus.NewGauge(prometheus.GaugeOpts{Name: "hololive_youtube_collection_snapshot_success", Help: "Whether the latest target snapshot completed with a current valid projection."}),
		lastSuccess:         prometheus.NewGauge(prometheus.GaugeOpts{Name: "hololive_youtube_collection_snapshot_last_success_timestamp_seconds", Help: "Unix time of the last complete valid target snapshot."}),
	}
	reg.MustRegister(m.liveState, m.liveReview, m.success, m.lastSuccess)

	return m
}

func (r *Runtime) observeCollectionTargets(ctx context.Context) {
	// 지표 집계는 claim 경로를 오래 점유하지 않으며 실패를 정상 0으로 덮지 않습니다.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	var samples []collectionTargetSample

	err := r.withDB(ctx, func(ctx context.Context) error {
		rows, err := r.pool.Query(ctx, mustSQL("collection_target_observability.sql"))
		if err != nil {
			return fmt.Errorf("query collection target snapshot: %w", err)
		}
		defer rows.Close()

		samples, err = scanCollectionTargets(rows)

		return err
	})
	youtubeCollectionTargets.observe(samples, r.now(), err)
}

func scanCollectionTargets(rows pgx.Rows) ([]collectionTargetSample, error) {
	var samples []collectionTargetSample

	for rows.Next() {
		var s collectionTargetSample

		if err := rows.Scan(&s.kind, &s.valid, &s.targets, &s.neverCompleted, &s.stale,
			&s.oldestCompletionAge, &s.due, &s.oldestDueAge, &s.requiredRate,
			&s.live, &s.upcoming, &s.other,
			&s.stateMismatch, &s.pastDue, &s.pastDue7d); err != nil {
			return nil, fmt.Errorf("scan collection target snapshot: %w", err)
		}

		if !s.valid {
			return nil, errors.New("observe collection targets: no current valid projection")
		}

		samples = append(samples, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection target snapshot: %w", err)
	}

	return samples, nil
}

func (m *collectionTargetMetrics) observe(samples []collectionTargetSample, now time.Time, err error) {
	if err != nil || len(samples) != 4 {
		m.success.Set(0)

		return
	}

	for i := range samples {
		s := &samples[i]
		m.targets.WithLabelValues(s.kind).Set(float64(s.targets))
		m.neverCompleted.WithLabelValues(s.kind).Set(float64(s.neverCompleted))
		m.stale.WithLabelValues(s.kind).Set(float64(s.stale))
		m.due.WithLabelValues(s.kind).Set(float64(s.due))
		m.oldestCompletionAge.WithLabelValues(s.kind).Set(s.oldestCompletionAge)
		m.oldestDueAge.WithLabelValues(s.kind).Set(s.oldestDueAge)
		m.requiredRate.WithLabelValues(s.kind).Set(s.requiredRate)
	}

	// 방송 상태 진단은 작업 종류별 수요가 아니라 동일 SQL 스냅샷의 운영 채널 집계입니다.
	live := &samples[0]
	m.liveState.WithLabelValues("LIVE").Set(float64(live.live))
	m.liveState.WithLabelValues("UPCOMING").Set(float64(live.upcoming))
	m.liveState.WithLabelValues("other").Set(float64(live.other))
	m.liveReview.WithLabelValues("state_mismatch").Set(float64(live.stateMismatch))
	m.liveReview.WithLabelValues("scheduled_before_now").Set(float64(live.pastDue))
	m.liveReview.WithLabelValues("scheduled_overdue_7d").Set(float64(live.pastDue7d))

	m.lastSuccess.Set(float64(now.Unix()))
	m.success.Set(1)
}
