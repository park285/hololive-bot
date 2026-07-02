package render

import (
	"fmt"
	"sync"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
)

var fontMu sync.Mutex

type calendarFonts struct {
	title, name, date, badge, stat, avatar font.Face
}

func loadCalendarFonts(sf float64) (calendarFonts, error) {
	var f calendarFonts
	var err error
	if f.title, err = fonts.CaptionFaceSized(30 * sf); err != nil {
		return f, fmt.Errorf("load title font: %w", err)
	}
	if f.name, err = fonts.CaptionFaceSized(22 * sf); err != nil {
		return f, fmt.Errorf("load name font: %w", err)
	}
	if f.date, err = fonts.CaptionFaceSized(16 * sf); err != nil {
		return f, fmt.Errorf("load date font: %w", err)
	}
	if f.badge, err = fonts.CaptionFaceSized(15 * sf); err != nil {
		return f, fmt.Errorf("load badge font: %w", err)
	}
	if f.stat, err = fonts.CaptionFaceSized(14 * sf); err != nil {
		return f, fmt.Errorf("load stat font: %w", err)
	}
	if f.avatar, err = fonts.CaptionFaceSized(34 * sf); err != nil {
		return f, fmt.Errorf("load avatar font: %w", err)
	}
	return f, nil
}
