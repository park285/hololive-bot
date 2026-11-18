package render

import (
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const (
	scaleFactor          = 4
	canvasWidth          = 620 * scaleFactor
	maxCanvasPixels      = 48_000_000
	calendarOutputWidth  = 1024
	calendarOutputHeight = 1536
	calendarTargetInnerH = canvasWidth * calendarOutputHeight / calendarOutputWidth
	maxCanvasH           = min(4000*scaleFactor, maxCanvasPixels/canvasWidth)
	paddingX             = 28 * scaleFactor
	entryIndent          = 20 * scaleFactor
	separatorH           = 1 * scaleFactor
)

type calendarMetrics struct {
	sf                                                     float64
	paddingY, headerH, dateSectGap, dateHeaderH, entryRowH int
	avatarSize, avatarGap                                  int
	badgePadX, badgePadY, badgeH, badgeRadius              int
	fonts                                                  calendarFonts
	strings                                                *messagestrings.Store
}

func newCalendarMetrics(compact float64) calendarMetrics {
	sf := float64(scaleFactor) * compact

	return calendarMetrics{
		sf:          sf,
		paddingY:    int(20 * sf),
		headerH:     int(82 * sf),
		dateSectGap: int(12 * sf),
		dateHeaderH: int(34 * sf),
		entryRowH:   int(104 * sf),
		avatarSize:  int(90 * sf),
		avatarGap:   int(18 * sf),
		badgePadX:   int(12 * sf),
		badgePadY:   int(5 * sf),
		badgeH:      int(32 * sf),
		badgeRadius: int(9 * sf),
	}
}
