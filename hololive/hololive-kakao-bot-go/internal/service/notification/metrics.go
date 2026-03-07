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

package notification

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	alarmMetricsInitOnce sync.Once

	alarmServiceOperationDuration *prometheus.HistogramVec
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
