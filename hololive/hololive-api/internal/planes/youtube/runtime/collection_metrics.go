package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
)

// collectionTargetJobCount는 collection_target_observability.sql mapping의 실행 job 수입니다.
// 일부 job만 담긴 snapshot을 성공으로 기록하지 않습니다.
const collectionTargetJobCount = 6

type collectionTargetSample struct {
	kind                                                                          string
	valid                                                                         bool
	targets, neverCompleted, stale, due                                           int64
	oldestCompletionAge, oldestDueAge, requiredRate                               float64
	live, upcoming, other                                                         int64
	stateMismatch, pastDue, pastDue7d                                             int64
	retainedTotal, metadataOnly, legacyUnreviewed, closedUnresolved, neverChecked int64
	oldestCheckAge                                                                float64
	unresolvedUnreviewed, legacyNotDue, metadataNeverChecked                      int64
	metadataCheckAge, oldestReviewAge                                             float64
}

type collectionTargetMetrics struct {
	targets, neverCompleted, stale, due             *prometheus.GaugeVec
	oldestCompletionAge, oldestDueAge, requiredRate *prometheus.GaugeVec
	liveState                                       *prometheus.GaugeVec
	liveReview                                      *prometheus.GaugeVec
	lifecycleRecords                                *prometheus.GaugeVec
	lifecycleCheckAge                               *prometheus.GaugeVec
	lifecycleReviewAge                              prometheus.Gauge
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
		neverCompleted:      gauge("never_completed_targets", "Active subjects without a completed collection lease, excluding subjects whose fresher evidence defers the next check (not_before in the future); not a zero age."),
		stale:               gauge("stale_targets", "Active completed subjects past both the polling interval after their last completion and their not_before eligibility; excludes never-completed subjects."),
		due:                 gauge("due_targets", "Active subjects whose effective due time, the later of the lease slot due time and not_before eligibility, has passed, including those not in any AP local queue."),
		oldestCompletionAge: gauge("oldest_completion_age_seconds", "Maximum age of a completed collection among active subjects not deferred by not_before; see never_completed_targets separately."),
		oldestDueAge:        gauge("oldest_due_age_seconds", "Maximum elapsed time past the effective due time (later of lease slot due time and not_before) among active subjects; unleased subjects use their continuous membership start."),
		requiredRate:        gauge("required_rpc_rate", "Base nominal YouTube.js helper RPC calls per second from active target polling intervals; subjects deferred by not_before count at most once per live freshness budget. Excludes retries and in-job enrichment calls."),
		liveState:           prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_live_states", Help: "Distinct live-session videos on enabled, unexpired live_snapshot channel targets in a current, unexpired projection where the head or product session is LIVE/UPCOMING, by reconciliation head state; other includes missing or terminal heads."}, []string{"state"}),
		liveReview:          prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_live_state_review_targets", Help: "Distinct live-session videos on enabled, unexpired live_snapshot channel targets in a current, unexpired projection with a LIVE/UPCOMING head or product session requiring review: actual head/product mismatch, missing LIVE or observed UPCOMING head, unreviewed legacy origin, or an UPCOMING head with product schedule before now/over 7 days overdue; reasons overlap and do not prove a broadcast ended."}, []string{"reason"}),
		success:             prometheus.NewGauge(prometheus.GaugeOpts{Name: "hololive_youtube_collection_snapshot_success", Help: "Whether the latest target snapshot completed with a current valid projection."}),
		lastSuccess:         prometheus.NewGauge(prometheus.GaugeOpts{Name: "hololive_youtube_collection_snapshot_last_success_timestamp_seconds", Help: "Unix time of the last complete valid target snapshot."}),
	}

	m.lifecycleRecords = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_lifecycle_records", Help: "Retained active lifecycle records by classification; review closure requires a currently reviewable record whose receipt matches the exact snapshot digest or the same lifecycle facts (status, schedule, origin, positive/end/absence evidence, availability verdict). unresolved_unreviewed excludes legacy_unknown UPCOMING records scheduled in the future, which stay in legacy_unreviewed and legacy_not_due. Categories may overlap."}, []string{"classification"})
	m.lifecycleCheckAge = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "hololive_youtube_collection_lifecycle_oldest_check_age_seconds", Help: "Age of the oldest actual video check by lifecycle classification; never-checked records are counted separately."}, []string{"classification"})
	m.lifecycleReviewAge = prometheus.NewGauge(prometheus.GaugeOpts{Name: "hololive_youtube_collection_lifecycle_oldest_review_age_seconds", Help: "Age of the oldest currently applicable unresolved review receipt."})
	reg.MustRegister(m.liveState, m.liveReview, m.lifecycleRecords, m.lifecycleCheckAge, m.lifecycleReviewAge, m.success, m.lastSuccess)

	return m
}

func (r *Runtime) observeCollectionTargets(ctx context.Context) {
	// 지표 집계는 claim 경로를 오래 점유하지 않으며 실패를 정상 0으로 덮지 않습니다.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	var samples []collectionTargetSample

	err := r.withDB(ctx, func(ctx context.Context) error {
		rows, err := r.pool.Query(ctx, mustSQL("collection_target_observability.sql"), r.liveFreshnessBudget.Milliseconds())
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
			&s.stateMismatch, &s.pastDue, &s.pastDue7d,
			&s.retainedTotal, &s.metadataOnly, &s.legacyUnreviewed, &s.closedUnresolved, &s.neverChecked, &s.oldestCheckAge,
			&s.unresolvedUnreviewed, &s.legacyNotDue, &s.metadataNeverChecked, &s.metadataCheckAge, &s.oldestReviewAge); err != nil {
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
	if err != nil || len(samples) != collectionTargetJobCount {
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
	m.liveReview.WithLabelValues("legacy_unreviewed").Set(float64(live.legacyUnreviewed))
	m.liveReview.WithLabelValues("unresolved_unreviewed").Set(float64(live.unresolvedUnreviewed))

	for classification, count := range map[string]int64{
		"retained_total": live.retainedTotal, "metadata_only": live.metadataOnly,
		"unresolved_unreviewed": live.unresolvedUnreviewed, "metadata_never_checked": live.metadataNeverChecked,
		"legacy_unreviewed": live.legacyUnreviewed, "legacy_not_due": live.legacyNotDue,
		"closed_unresolved": live.closedUnresolved, "never_checked": live.neverChecked,
	} {
		m.lifecycleRecords.WithLabelValues(classification).Set(float64(count))
	}

	m.lifecycleCheckAge.WithLabelValues("legacy_unreviewed").Set(live.oldestCheckAge)
	m.lifecycleCheckAge.WithLabelValues("metadata_only").Set(live.metadataCheckAge)
	m.lifecycleReviewAge.Set(live.oldestReviewAge)
	m.liveReview.WithLabelValues("scheduled_before_now").Set(float64(live.pastDue))
	m.liveReview.WithLabelValues("scheduled_overdue_7d").Set(float64(live.pastDue7d))

	m.lastSuccess.Set(float64(now.Unix()))
	m.success.Set(1)
}
