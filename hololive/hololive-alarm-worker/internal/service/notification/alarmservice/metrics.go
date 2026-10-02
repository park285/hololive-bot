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

// alarmMetricSet은 alarm service가 기록하는 Prometheus 수집기 묶음이다. 수집기는 alarmMetrics의 첫 호출에서만
// 만들어지고 등록되므로, 초기화되지 않은 수집기를 읽는 경로가 없다.
type alarmMetricSet struct {
	serviceOperationDuration *prometheus.HistogramVec
	cacheRebuildTotal        *prometheus.CounterVec
	cacheRebuildDuration     *prometheus.HistogramVec
	cacheRebuildLoaded       *prometheus.GaugeVec
	memberNameCallerFallback prometheus.Counter
}

// alarmMetrics는 첫 호출에서 수집기를 기본 registerer에 등록한다. 등록이 panic하면 이후 호출도 같은 값으로
// panic하므로, 반쯤 초기화된 수집기가 관측 경로에 노출되지 않는다.
var alarmMetrics = sync.OnceValue(newAlarmMetricSet)

func newAlarmMetricSet() *alarmMetricSet {
	return &alarmMetricSet{
		serviceOperationDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_service_operation_duration_seconds",
				Help:    "Alarm service operation duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "result"},
		),
		cacheRebuildTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "hololive_alarm_cache_rebuild_total",
				Help: "Alarm cache rebuild attempts by operation and result.",
			},
			[]string{"operation", "result"},
		),
		cacheRebuildDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "hololive_alarm_cache_rebuild_duration_seconds",
				Help:    "Alarm cache rebuild duration in seconds by operation and result.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "result"},
		),
		cacheRebuildLoaded: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "hololive_alarm_cache_rebuild_loaded",
				Help: "Last successful alarm cache rebuild loaded counts by operation and resource.",
			},
			[]string{"operation", "resource"},
		),
		memberNameCallerFallback: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "hololive_alarm_member_name_caller_fallback_total",
				Help: "Alarm cache writes that used the caller member name because member data had no display name.",
			},
		),
	}
}

func observeAlarmMemberNameCallerFallback() {
	alarmMetrics().memberNameCallerFallback.Inc()
}

func observeAlarmServiceOperation(operation string, startedAt time.Time, err error) {
	alarmMetrics().serviceOperationDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func alarmOperationResult(err error) string {
	if err != nil {
		return "error"
	}

	return "ok"
}

func observeAlarmCacheRebuild(operation string, err error) {
	alarmMetrics().cacheRebuildTotal.WithLabelValues(operation, alarmOperationResult(err)).Inc()
}

func observeAlarmCacheRebuildDuration(operation string, startedAt time.Time, err error) {
	alarmMetrics().cacheRebuildDuration.WithLabelValues(operation, alarmOperationResult(err)).Observe(time.Since(startedAt).Seconds())
}

func observeAlarmCacheRebuildLoaded(operation string, alarmsLoaded, roomsLoaded, channelsLoaded int) {
	loaded := alarmMetrics().cacheRebuildLoaded
	loaded.WithLabelValues(operation, "alarms").Set(float64(alarmsLoaded))
	loaded.WithLabelValues(operation, "rooms").Set(float64(roomsLoaded))
	loaded.WithLabelValues(operation, "channels").Set(float64(channelsLoaded))
}
