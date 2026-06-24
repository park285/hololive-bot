// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package polling

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"
)

var (
	defaultMetrics     *Metrics
	defaultMetricsOnce sync.Once
)

type Metrics struct {
	SchedulerRegisteredJobs           prometheus.Gauge
	SchedulerDispatchDefer            *prometheus.CounterVec
	SchedulerPollDuration             *prometheus.HistogramVec
	BudgetReserveTotal                *prometheus.CounterVec
	BudgetReserveWaitSeconds          *prometheus.HistogramVec
	BudgetRetryAfterSeconds           *prometheus.HistogramVec
	BudgetInflight                    *prometheus.GaugeVec
	JobClaimTotal                     *prometheus.CounterVec
	JobLeaseRenewTotal                *prometheus.CounterVec
	JobLeaseTTLSeconds                *prometheus.GaugeVec
	JobLeaseElapsedRatio              *prometheus.GaugeVec
	JobLeaseNearExpiryTotal           *prometheus.CounterVec
	JobMarkCompletedTotal             *prometheus.CounterVec
	JobDeferTotal                     *prometheus.CounterVec
	JobReleaseTotal                   *prometheus.CounterVec
	OutboxInsertTotal                 *prometheus.CounterVec
	CommunityShortsDetectedPostsTotal *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	defaultMetricsOnce.Do(func() {
		m := &Metrics{}
		m.registerSchedulerMetrics()
		m.registerJobClaimMetrics()
		m.registerContentMetrics()
		batchrepo.ObserveOutboxInsert = m.ObserveOutboxInsert
		defaultMetrics = m
	})

	if defaultMetrics == nil {
		panic("youtube poller metrics initialization failed")
	}
	return defaultMetrics
}

func (m *Metrics) registerSchedulerMetrics() {
	m.SchedulerRegisteredJobs = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_poller_scheduler_job_count",
		Help: "현재 YouTube poller scheduler에 등록된 job 수",
	})
	m.SchedulerDispatchDefer = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_scheduler_dispatch_deferred_total",
		Help: "worker channel 포화 등으로 scheduler dispatch가 defer된 횟수",
	}, []string{"reason"})
	m.SchedulerPollDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_poller_poll_duration_seconds",
		Help:    "poller별 channel poll 실행 시간",
		Buckets: prometheus.DefBuckets,
	}, []string{"poller", "status"})
	m.BudgetReserveTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_budget_reserve_total",
		Help: "source별 scheduler budget reservation 결과",
	}, []string{"source", "result", "burst_class", "priority"})
	m.BudgetReserveWaitSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_poller_budget_reserve_wait_seconds",
		Help:    "source별 scheduler budget reservation 대기 시간",
		Buckets: prometheus.DefBuckets,
	}, []string{"source"})
	m.BudgetRetryAfterSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_poller_budget_retry_after_seconds",
		Help:    "source별 scheduler budget denial retry_after",
		Buckets: prometheus.DefBuckets,
	}, []string{"source"})
	m.BudgetInflight = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_poller_budget_inflight",
		Help: "source별 scheduler budget reservation inflight 수",
	}, []string{"source"})
}

func (m *Metrics) registerJobClaimMetrics() {
	m.JobClaimTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_claim_total",
		Help: "poller별 distributed job claim 결과",
	}, []string{"poller", "result"})
	m.JobLeaseRenewTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_lease_renew_total",
		Help: "poller별 distributed job lease renew 결과",
	}, []string{"poller", "result"})
	m.JobLeaseTTLSeconds = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_poller_job_lease_ttl_seconds",
		Help: "poller별 distributed job claim lease TTL",
	}, []string{"poller"})
	m.JobLeaseElapsedRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "youtube_poller_job_lease_elapsed_ratio",
		Help: "poller별 distributed job claim lease elapsed ratio",
	}, []string{"poller"})
	m.JobLeaseNearExpiryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_lease_near_expiry_total",
		Help: "poller별 distributed job claim lease near-expiry 횟수",
	}, []string{"poller"})
	m.JobMarkCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_mark_completed_total",
		Help: "poller별 distributed job completion marker 결과",
	}, []string{"poller", "result"})
	m.JobDeferTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_defer_total",
		Help: "poller별 distributed job defer marker 결과",
	}, []string{"poller", "result"})
	m.JobReleaseTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_release_total",
		Help: "poller별 distributed job lease release 결과",
	}, []string{"poller", "result"})
}

func (m *Metrics) registerContentMetrics() {
	m.OutboxInsertTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_outbox_insert_total",
		Help: "YouTube notification outbox insert 결과",
	}, []string{"kind", "result"})
	m.CommunityShortsDetectedPostsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_community_shorts_detected_posts_total",
		Help: "커뮤니티/쇼츠 감지 게시물 수",
	}, []string{"alarm_type"})
}

func (m *Metrics) ObserveJobClaim(pollerName, result string) {
	m.JobClaimTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveJobLeaseRenew(pollerName, result string) {
	m.JobLeaseRenewTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveBudgetReserve(profile BudgetProfile, result string) {
	forBudgetProfileSource(profile, func(source BudgetSource) {
		m.BudgetReserveTotal.WithLabelValues(
			string(source),
			result,
			string(profile.BurstClass),
			string(profile.Priority),
		).Inc()
	})
}

func (m *Metrics) ObserveBudgetReserveWait(profile BudgetProfile, elapsed time.Duration) {
	forBudgetProfileSource(profile, func(source BudgetSource) {
		m.BudgetReserveWaitSeconds.WithLabelValues(string(source)).Observe(elapsed.Seconds())
	})
}

func (m *Metrics) ObserveBudgetRetryAfter(profile BudgetProfile, retryAfter time.Duration) {
	forBudgetProfileSource(profile, func(source BudgetSource) {
		m.BudgetRetryAfterSeconds.WithLabelValues(string(source)).Observe(retryAfter.Seconds())
	})
}

func (m *Metrics) AddBudgetInflight(profile BudgetProfile, delta float64) {
	forBudgetProfileSource(profile, func(source BudgetSource) {
		m.BudgetInflight.WithLabelValues(string(source)).Add(delta)
	})
}

func (m *Metrics) ObserveJobLeaseTTL(pollerName string, ttl time.Duration) {
	m.JobLeaseTTLSeconds.WithLabelValues(pollerName).Set(ttl.Seconds())
}

func (m *Metrics) ObserveJobLeaseElapsedRatio(pollerName string, ratio float64) {
	m.JobLeaseElapsedRatio.WithLabelValues(pollerName).Set(ratio)
}

func (m *Metrics) ObserveJobLeaseNearExpiry(pollerName string) {
	m.JobLeaseNearExpiryTotal.WithLabelValues(pollerName).Inc()
}

func (m *Metrics) ObserveJobMarkCompleted(pollerName, result string) {
	m.JobMarkCompletedTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveJobDefer(pollerName, result string) {
	m.JobDeferTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveJobRelease(pollerName, result string) {
	m.JobReleaseTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveOutboxInsert(kind domain.OutboxKind, result string, count int64) {
	if count <= 0 {
		return
	}
	m.OutboxInsertTotal.WithLabelValues(string(kind), result).Add(float64(count))
}

func (m *Metrics) ObserveCommunityShortsDetectedPosts(alarmType domain.AlarmType, count int) {
	if count <= 0 {
		return
	}
	m.CommunityShortsDetectedPostsTotal.WithLabelValues(string(alarmType)).Add(float64(count))
}

func BoolResult(ok bool, err error) string {
	if err != nil {
		return "error"
	}
	if ok {
		return "success"
	}
	return "lost"
}

func forBudgetProfileSource(profile BudgetProfile, observe func(BudgetSource)) {
	for source := range profile.SourceUnits {
		observe(source)
	}
}
