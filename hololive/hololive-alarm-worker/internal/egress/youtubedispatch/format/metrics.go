package format

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// memberNameMissingTotal은 members에 한국어 표시명이 없어 misc/vtuber_fallback 문구로 알림을 만든 횟수다.
// 첫 호출에서 기본 registerer에 등록한다.
var memberNameMissingTotal = sync.OnceValue(func() prometheus.Counter {
	return promauto.NewCounter(prometheus.CounterOpts{
		Name: "hololive_youtube_outbox_member_name_missing_total",
		Help: "YouTube notifications rendered with misc/vtuber_fallback because the member has no Korean display name.",
	})
})
