package render

import (
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const (
	calendarOutputWidth  = 1024
	calendarOutputHeight = 1536
)

type calendarMetrics struct {
	strings *messagestrings.Store
}

func newCalendarMetrics(_ float64) calendarMetrics {
	return calendarMetrics{}
}
