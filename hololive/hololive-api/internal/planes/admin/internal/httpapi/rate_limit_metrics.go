package httpapi

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var apiRateLimitCheckFailuresTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "hololive_admin_rate_limit_check_failures_total",
		Help: "Number of admin holo API requests rejected with 503 because the rate limiter check failed.",
	},
)
