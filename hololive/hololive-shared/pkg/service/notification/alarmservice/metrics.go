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

package alarmservice

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	alarmMetricsInitOnce sync.Once

	alarmServiceOperationDuration *prometheus.HistogramVec
	alarmCacheRebuildTotal        *prometheus.CounterVec
	alarmCacheRebuildDuration     *prometheus.HistogramVec
	alarmCacheRebuildLoaded       *prometheus.GaugeVec
)

func initAlarmMetrics() {
	alarmMetricsInitOnce.Do(func() {
		alarmServiceOperationDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_service_operation_duration_seconds",
				Help:    "Alarm service operation duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "result"},
		)
		alarmCacheRebuildTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_cache_rebuild_total",
				Help: "Alarm cache rebuild attempts by operation and result.",
			},
			[]string{"operation", "result"},
		)
		alarmCacheRebuildDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_cache_rebuild_duration_seconds",
				Help:    "Alarm cache rebuild duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "result"},
		)
		alarmCacheRebuildLoaded = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "hololive_alarm_cache_rebuild_loaded",
				Help: "Last successful alarm cache rebuild loaded counts by operation and resource.",
			},
			[]string{"operation", "resource"},
		)
	})
}

func observeAlarmServiceOperation(operation string, startedAt time.Time, err error) {
	initAlarmMetrics()
	alarmServiceOperationDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func alarmOperationResult(err error) string {
	if err != nil {
		return "error"
	}

	return "ok"
}

func observeAlarmCacheRebuild(operation string, err error) {
	initAlarmMetrics()
	alarmCacheRebuildTotal.WithLabelValues(operation, alarmOperationResult(err)).Inc()
}

func observeAlarmCacheRebuildDuration(operation string, startedAt time.Time, err error) {
	initAlarmMetrics()
	alarmCacheRebuildDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func observeAlarmCacheRebuildLoaded(operation string, alarmsLoaded, roomsLoaded, channelsLoaded int) {
	initAlarmMetrics()
	alarmCacheRebuildLoaded.WithLabelValues(operation, "alarms").Set(float64(alarmsLoaded))
	alarmCacheRebuildLoaded.WithLabelValues(operation, "rooms").Set(float64(roomsLoaded))
	alarmCacheRebuildLoaded.WithLabelValues(operation, "channels").Set(float64(channelsLoaded))
}
