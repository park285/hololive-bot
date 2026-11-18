package handlers

import "github.com/prometheus/client_golang/prometheus/testutil"

func imageTextFallbackCount(command, reason string) float64 {
	return testutil.ToFloat64(imageTextFallbackTotal.WithLabelValues(command, reason))
}
