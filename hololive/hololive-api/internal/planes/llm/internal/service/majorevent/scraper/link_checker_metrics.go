package scraper

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 링크 검사 HEAD→GET 예외 계약(docs/current/contracts/majorevent.md)의 telemetry다. 각 시계열은 GET 재확인 뒤의
// 링크 상태(ok, failed, blocked)별로 세며, ok는 HEAD만으로는 실패였을 링크를 GET이 확인한 경우다.
var linkGetFallbackTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hololive_majorevent_link_get_fallback_total",
	Help: "Major event link checks re-run with GET after a HEAD rejection or transport failure, by resulting link status.",
}, []string{"result"})

func observeLinkGetFallback(status domain.MajorEventLinkStatus) {
	linkGetFallbackTotal.WithLabelValues(string(status)).Inc()
}
