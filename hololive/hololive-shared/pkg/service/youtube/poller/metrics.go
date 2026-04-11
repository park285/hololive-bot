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

package poller

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	pollerMetricsOnce sync.Once

	schedulerRegisteredJobs           prometheus.Gauge
	schedulerDispatchDefer            *prometheus.CounterVec
	schedulerPollDuration             *prometheus.HistogramVec
	communityShortsDetectedPostsTotal *prometheus.CounterVec
)

func ensureMetrics() {
	pollerMetricsOnce.Do(func() {
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
		communityShortsDetectedPostsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "youtube_poller_community_shorts_detected_posts_total",
			Help: "채널별 커뮤니티/쇼츠 감지 게시물 수",
		}, []string{"channel_id", "alarm_type"})
	})
}
