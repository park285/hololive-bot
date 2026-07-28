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

package member

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	memberCacheEpoch = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hololive_member_cache_epoch",
		Help: "Most recently reconciled durable member cache epoch in this process.",
	})
	memberCacheEpochReconcileTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_member_cache_epoch_reconcile_total",
		Help: "Member cache durable epoch reconciliation attempts.",
	}, []string{"reason", "result"})
	memberCacheEpochNotificationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_member_cache_epoch_notifications_total",
		Help: "Member cache epoch Pub/Sub notification outcomes.",
	}, []string{"result"})
	memberCacheBypassTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hololive_member_cache_bypass_total",
		Help: "Member cache reads routed directly to PostgreSQL because epoch authority was unavailable.",
	}, []string{"operation", "reason"})
)
