package handlers

import "github.com/prometheus/client_golang/prometheus/testutil"

// imageTextFallbackCount는 전역 counter 값을 읽는다. 증가분을 보는 테스트는 병렬로 돌리지 않는다.
func imageTextFallbackCount(command, reason string) float64 {
	return testutil.ToFloat64(imageTextFallbackTotal.WithLabelValues(command, reason))
}
