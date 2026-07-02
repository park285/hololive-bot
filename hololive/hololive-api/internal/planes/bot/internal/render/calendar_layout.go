package render

import (
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

const (
	scaleFactor     = 5
	canvasWidth     = 620 * scaleFactor
	maxCanvasPixels = 48_000_000
	// 최종 출력 크기(카카오 인라인 표시 근사). 내부는 canvasWidth(고해상도)로 그린 뒤
	// calendarOutputWidth로 다운스케일해 전송한다 = SSAA + 카카오 재압축 손실 최소화.
	// 항목이 많으면 축소 대신 calendarPageInnerH 예산으로 페이지를 나눈다.
	calendarOutputWidth  = 1024
	calendarOutputHeight = 1536
	calendarPageInnerH   = canvasWidth * calendarOutputHeight / calendarOutputWidth
	// iris SendMultipleImages 상한
	calendarMaxPages = 8
	maxCanvasH       = min(4000*scaleFactor, maxCanvasPixels/canvasWidth)
	paddingX         = 28 * scaleFactor
	entryIndent      = 20 * scaleFactor
	separatorH       = 1 * scaleFactor
)

type calendarMetrics struct {
	sf                                                     float64
	paddingY, headerH, dateSectGap, dateHeaderH, entryRowH int
	avatarSize, avatarGap                                  int
	badgePadX, badgePadY, badgeH, badgeRadius              int
	fonts                                                  calendarFonts
	strings                                                *messagestrings.Store
}

func newCalendarMetrics() calendarMetrics {
	sf := float64(scaleFactor)
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
