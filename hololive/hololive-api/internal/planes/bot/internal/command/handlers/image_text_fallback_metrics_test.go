package handlers

import "github.com/prometheus/client_golang/prometheus/testutil"

// 증가분을 보는 테스트는 병렬로 돌리지 않는다.
func imageTextFallbackCount(command, reason string) float64 {
	return testutil.ToFloat64(imageTextFallbackTotal.WithLabelValues(command, reason))
}
