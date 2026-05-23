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
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling/batchrepo"
)

var (
	pollerMetricsOnce sync.Once

	schedulerRegisteredJobs           prometheus.Gauge
	schedulerDispatchDefer            *prometheus.CounterVec
	schedulerPollDuration             *prometheus.HistogramVec
	jobClaimTotal                     *prometheus.CounterVec
	jobLeaseRenewTotal                *prometheus.CounterVec
	jobMarkCompletedTotal             *prometheus.CounterVec
	jobReleaseTotal                   *prometheus.CounterVec
	outboxInsertTotal                 *prometheus.CounterVec
	communityShortsDetectedPostsTotal *prometheus.CounterVec
	publishedAtResolutionAttemptTotal *prometheus.CounterVec
	publishedAtResolutionSuccessTotal *prometheus.CounterVec
	publishedAtResolutionFailureTotal *prometheus.CounterVec
	publishedAtResolverSkippedTotal   *prometheus.CounterVec
	publishedAtResolverEnqueuedTotal  *prometheus.CounterVec
	publishedAtResolverPageCandidates prometheus.Gauge
	publishedAtResolverScannedTotal   *prometheus.CounterVec
)

func ensureMetrics() {
	pollerMetricsOnce.Do(func() {
		registerSchedulerMetrics()
		registerJobClaimMetrics()
		registerContentMetrics()
	})
}

func registerSchedulerMetrics() {
	schedulerRegisteredJobs = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_poller_scheduler_job_count",
		Help: "현재 YouTube poller scheduler에 등록된 job 수",
	})
	schedulerDispatchDefer = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_scheduler_dispatch_deferred_total",
		Help: "worker channel 포화 등으로 scheduler dispatch가 defer된 횟수",
	}, []string{"reason"})
	schedulerPollDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "youtube_poller_poll_duration_seconds",
		Help:    "poller별 channel poll 실행 시간",
		Buckets: prometheus.DefBuckets,
	}, []string{"poller", "status"})
}

func registerJobClaimMetrics() {
	jobClaimTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_claim_total",
		Help: "poller별 distributed job claim 결과",
	}, []string{"poller", "result"})
	jobLeaseRenewTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_lease_renew_total",
		Help: "poller별 distributed job lease renew 결과",
	}, []string{"poller", "result"})
	jobMarkCompletedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_mark_completed_total",
		Help: "poller별 distributed job completion marker 결과",
	}, []string{"poller", "result"})
	jobReleaseTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_job_release_total",
		Help: "poller별 distributed job lease release 결과",
	}, []string{"poller", "result"})
}

func registerContentMetrics() {
	outboxInsertTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_outbox_insert_total",
		Help: "YouTube notification outbox insert 결과",
	}, []string{"kind", "result"})
	communityShortsDetectedPostsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_community_shorts_detected_posts_total",
		Help: "커뮤니티/쇼츠 감지 게시물 수",
	}, []string{"alarm_type"})
	registerPublishedAtResolverMetrics()
}

func registerPublishedAtResolverMetrics() {
	publishedAtResolutionAttemptTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolution_attempt_total",
		Help: "resolver published_at 해석 시도 횟수",
	}, []string{"kind"})
	publishedAtResolutionSuccessTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolution_success_total",
		Help: "resolver published_at 해석 성공 횟수",
	}, []string{"kind"})
	publishedAtResolutionFailureTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolution_failure_total",
		Help: "resolver published_at 해석 실패 횟수",
	}, []string{"kind"})
	publishedAtResolverSkippedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolver_skipped_total",
		Help: "resolver가 후보를 enqueue 없이 건너뛴 횟수",
	}, []string{"kind", "reason"})
	publishedAtResolverEnqueuedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolver_enqueued_total",
		Help: "resolver가 enqueue한 알림 수",
	}, []string{"kind"})
	publishedAtResolverPageCandidates = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "youtube_poller_published_at_resolver_page_candidates",
		Help: "현재 resolver iteration에서 마지막으로 조회한 candidate page 길이",
	})
	publishedAtResolverScannedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "youtube_poller_published_at_resolver_scanned_total",
		Help: "resolver가 iteration 동안 스캔한 candidate 수",
	}, []string{"kind"})
}

func init() {
	ensureMetrics()
	batchrepo.ObserveOutboxInsert = observeOutboxInsert
}

func observePublishedAtResolutionAttempt(kind domain.OutboxKind) {
	ensureMetrics()
	publishedAtResolutionAttemptTotal.WithLabelValues(string(kind)).Inc()
}

func observeJobClaim(pollerName, result string) {
	ensureMetrics()
	jobClaimTotal.WithLabelValues(pollerName, result).Inc()
}

func observeJobLeaseRenew(pollerName, result string) {
	ensureMetrics()
	jobLeaseRenewTotal.WithLabelValues(pollerName, result).Inc()
}

func observeJobMarkCompleted(pollerName, result string) {
	ensureMetrics()
	jobMarkCompletedTotal.WithLabelValues(pollerName, result).Inc()
}

func observeJobRelease(pollerName, result string) {
	ensureMetrics()
	jobReleaseTotal.WithLabelValues(pollerName, result).Inc()
}

func observeOutboxInsert(kind domain.OutboxKind, result string, count int64) {
	if count <= 0 {
		return
	}
	ensureMetrics()
	outboxInsertTotal.WithLabelValues(string(kind), result).Add(float64(count))
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

func observePublishedAtResolutionSuccess(kind domain.OutboxKind) {
	ensureMetrics()
	publishedAtResolutionSuccessTotal.WithLabelValues(string(kind)).Inc()
}

func observePublishedAtResolutionFailure(kind domain.OutboxKind) {
	ensureMetrics()
	publishedAtResolutionFailureTotal.WithLabelValues(string(kind)).Inc()
}

func observePublishedAtResolverSkipped(kind domain.OutboxKind, reason string) {
	ensureMetrics()
	publishedAtResolverSkippedTotal.WithLabelValues(string(kind), reason).Inc()
}

func observePublishedAtResolverEnqueued(kind domain.OutboxKind) {
	ensureMetrics()
	publishedAtResolverEnqueuedTotal.WithLabelValues(string(kind)).Inc()
}

func setPublishedAtResolverPageCandidates(count int) {
	ensureMetrics()
	publishedAtResolverPageCandidates.Set(float64(count))
}

func observePublishedAtResolverScanned(kind domain.OutboxKind) {
	ensureMetrics()
	publishedAtResolverScannedTotal.WithLabelValues(string(kind)).Inc()
}
