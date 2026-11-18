package schedulecard

import (
	"context"
	"fmt"
	"image"

	xdraw "golang.org/x/image/draw"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

func renderOrbitPages(ctx context.Context, card Card, format Format) ([][]byte, error) {
	if format != Format3x4 {
		return nil, fmt.Errorf("%w: orbit requires %q format", ErrInvalidCard, Format3x4)
	}

	fs, err := loadFontSet()
	if err != nil {
		return nil, fmt.Errorf("render schedule card pages: %w", err)
	}

	v, err := prepareWeek(card, fs.drawable(), cardtemplate.Orbit)
	if err != nil {
		return nil, err
	}

	endFormat := "01. 02"

	if card.WeekStart.Year() != card.WeekEnd.Year() {
		endFormat = "2006. 01. 02"
	}

	v.period = card.WeekStart.Format("2006. 01. 02") + " — " + card.WeekEnd.Format(endFormat)

	pages := layoutOrbit(orbitPieces(fs, v.days))
	if len(pages) > MaxPages {
		return nil, ErrRenderBudget
	}

	if cause := context.Cause(ctx); cause != nil {
		return nil, cause
	}

	w, h, _ := format.size()
	c := newCanvas(w, h, supersample, fs)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	scaler := xdraw.CatmullRom.NewScaler(w, h, c.img.Bounds().Dx(), c.img.Bounds().Dy())
	out := make([][]byte, len(pages))

	remaining := MaxAlbumBytes

	for i, page := range pages {
		if cause := context.Cause(ctx); cause != nil {
			return nil, cause
		}

		paintOrbitPage(c, v, page, i+1, len(pages))
		scaler.Scale(dst, dst.Bounds(), c.img, c.img.Bounds(), xdraw.Src, nil)

		out[i], err = encodeCard(ctx, dst, min(remaining, MaxPageBytes))
		if err != nil {
			return nil, err
		}

		remaining -= len(out[i])
	}

	return out, nil
}
