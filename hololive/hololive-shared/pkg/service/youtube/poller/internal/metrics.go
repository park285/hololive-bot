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
	JobClaimTotal                     *prometheus.CounterVec
	JobLeaseRenewTotal                *prometheus.CounterVec
	JobMarkCompletedTotal             *prometheus.CounterVec
	JobReleaseTotal                   *prometheus.CounterVec
	OutboxInsertTotal                 *prometheus.CounterVec
	CommunityShortsDetectedPostsTotal *prometheus.CounterVec
	PublishedAtResolutionAttemptTotal *prometheus.CounterVec
	PublishedAtResolutionSuccessTotal *prometheus.CounterVec
	PublishedAtResolutionFailureTotal *prometheus.CounterVec
	PublishedAtResolverSkippedTotal   *prometheus.CounterVec
	PublishedAtResolverEnqueuedTotal  *prometheus.CounterVec
	PublishedAtResolverPageCandidates prometheus.Gauge
	PublishedAtResolverScannedTotal   *prometheus.CounterVec
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
	m.JobMarkCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_mark_completed_total",
		Help: "poller별 distributed job completion marker 결과",
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
	m.registerPublishedAtResolverMetrics()
}

func (m *Metrics) registerPublishedAtResolverMetrics() {
	m.PublishedAtResolutionAttemptTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolution_attempt_total",
		Help: "resolver published_at 해석 시도 횟수",
	}, []string{"kind"})
	m.PublishedAtResolutionSuccessTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolution_success_total",
		Help: "resolver published_at 해석 성공 횟수",
	}, []string{"kind"})
	m.PublishedAtResolutionFailureTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolution_failure_total",
		Help: "resolver published_at 해석 실패 횟수",
	}, []string{"kind"})
	m.PublishedAtResolverSkippedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolver_skipped_total",
		Help: "resolver가 후보를 enqueue 없이 건너뛴 횟수",
	}, []string{"kind", "reason"})
	m.PublishedAtResolverEnqueuedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolver_enqueued_total",
		Help: "resolver가 enqueue한 알림 수",
	}, []string{"kind"})
	m.PublishedAtResolverPageCandidates = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_poller_published_at_resolver_page_candidates",
		Help: "현재 resolver iteration에서 마지막으로 조회한 candidate page 길이",
	})
	m.PublishedAtResolverScannedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolver_scanned_total",
		Help: "resolver가 iteration 동안 스캔한 candidate 수",
	}, []string{"kind"})
}

func (m *Metrics) ObservePublishedAtResolutionAttempt(kind domain.OutboxKind) {
	m.PublishedAtResolutionAttemptTotal.WithLabelValues(string(kind)).Inc()
}

func (m *Metrics) ObserveJobClaim(pollerName, result string) {
	m.JobClaimTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveJobLeaseRenew(pollerName, result string) {
	m.JobLeaseRenewTotal.WithLabelValues(pollerName, result).Inc()
}

func (m *Metrics) ObserveJobMarkCompleted(pollerName, result string) {
	m.JobMarkCompletedTotal.WithLabelValues(pollerName, result).Inc()
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

func boolResult(ok bool, err error) string {
	if err != nil {
		return "error"
	}
	if ok {
		return "success"
	}
	return "lost"
}

func (m *Metrics) ObservePublishedAtResolutionSuccess(kind domain.OutboxKind) {
	m.PublishedAtResolutionSuccessTotal.WithLabelValues(string(kind)).Inc()
}

func (m *Metrics) ObservePublishedAtResolutionFailure(kind domain.OutboxKind) {
	m.PublishedAtResolutionFailureTotal.WithLabelValues(string(kind)).Inc()
}

func (m *Metrics) ObservePublishedAtResolverSkipped(kind domain.OutboxKind, reason string) {
	m.PublishedAtResolverSkippedTotal.WithLabelValues(string(kind), reason).Inc()
}

func (m *Metrics) ObservePublishedAtResolverEnqueued(kind domain.OutboxKind) {
	m.PublishedAtResolverEnqueuedTotal.WithLabelValues(string(kind)).Inc()
}

func (m *Metrics) SetPublishedAtResolverPageCandidates(count int) {
	m.PublishedAtResolverPageCandidates.Set(float64(count))
}

func (m *Metrics) ObservePublishedAtResolverScanned(kind domain.OutboxKind) {
	m.PublishedAtResolverScannedTotal.WithLabelValues(string(kind)).Inc()
}
