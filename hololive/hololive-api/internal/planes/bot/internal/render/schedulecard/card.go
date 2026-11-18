package schedulecard

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
)

type Status string

const (
	StatusPlanned   Status = "planned"
	StatusTentative Status = "tentative"
	StatusCancelled Status = "cancelled" //nolint:misspell // 저장소 RowStatus와 DB CHECK의 계약 철자라, canceled로 바꾸면 저장된 행을 거부한다.
	StatusOff       Status = "off"
)

type Format string

const (
	Format16x9 Format = "16x9"
	Format4x3  Format = "4x3"
	Format3x4  Format = "3x4"
)

const MaxItems = 100

const (
	MaxPages      = 8
	MaxPageBytes  = 20 << 20
	MaxAlbumBytes = 30 << 20
)

var ErrRenderBudget = errors.New("schedule exceeds image budget")

var ErrInvalidCard = errors.New("invalid schedule card")

type Item struct {
	Day       time.Time
	Start     string
	End       string
	PrevStart string
	MovedFrom time.Time
	Converted bool
	FromPost  bool
	Status    Status
	Title     string
	With      []string
	Place     string
	Tags      []string
	Note      string
}

type Card struct {
	ChannelID   string
	MemberName  string
	WeekStart   time.Time
	WeekEnd     time.Time
	ExtractedAt time.Time
	Items       []Item
	Photo       image.Image
}

const supersample = 2

var (
	jst = time.FixedZone("JST", 9*60*60)

	renderMu = semaphore.NewWeighted(1)
)

func (f Format) size() (w, h int, ok bool) {
	switch f {
	case Format16x9:
		return 1600, 900, true
	case Format4x3:
		return 1440, 1080, true
	case Format3x4:
		return 1080, 1440, true
	}

	return 0, 0, false
}

func Render(card Card, format Format) ([]byte, error) {
	ctx := context.Background()
	if err := renderMu.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("acquire render lock: %w", err)
	}

	defer renderMu.Release(1)

	return renderClassic(ctx, card, format)
}

func renderClassic(ctx context.Context, card Card, format Format) ([]byte, error) {
	w, h, ok := format.size()
	if !ok {
		return nil, fmt.Errorf("%w: unknown format %q", ErrInvalidCard, format)
	}

	fs, err := loadFontSet()
	if err != nil {
		return nil, fmt.Errorf("render schedule card: %w", err)
	}

	v, err := prepare(card, fs.drawable())
	if err != nil {
		return nil, err
	}

	img := paint(fs, themeFor(card.ChannelID), v, w, h)

	return encodeCard(ctx, img, MaxPageBytes)
}

func RenderPages(ctx context.Context, card Card, format Format, template cardtemplate.ID) ([][]byte, error) {
	if err := renderMu.Acquire(ctx, 1); err != nil {
		return nil, fmt.Errorf("wait for drawing: %w", context.Cause(ctx))
	}

	defer renderMu.Release(1)

	switch template {
	case cardtemplate.Classic:
		page, renderErr := renderClassic(ctx, card, format)
		if renderErr != nil {
			return nil, renderErr
		}

		return [][]byte{page}, nil
	case cardtemplate.Orbit:
		return renderOrbitPages(ctx, card, format)
	default:
		return nil, fmt.Errorf("%w: unknown template %q", ErrInvalidCard, template)
	}
}

type pngBuffer struct {
	buf   []byte
	limit int
}

func (b *pngBuffer) Write(data []byte) (int, error) {
	if len(data) > b.limit-len(b.buf) {
		return 0, ErrRenderBudget
	}

	b.buf = append(b.buf, data...)

	return len(data), nil
}

func encodeCard(ctx context.Context, img *image.RGBA, limit int) ([]byte, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}

	buf := pngBuffer{limit: limit}

	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode schedule card png: %w", err)
	}

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}

	return buf.buf, nil
}

func paint(fs *fontSet, th theme, v view, w, h int) *image.RGBA {
	c := newCanvas(w, h, supersample, fs)
	th(c, v)

	return c.Downscale(w, h)
}
