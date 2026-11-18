// Package schedulecard는 멤버 한 명의 주간 일정 카드를 PNG로 그린다.
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

// Status는 일정 항목의 상태다.
type Status string

const (
	StatusPlanned   Status = "planned"
	StatusTentative Status = "tentative"
	StatusCancelled Status = "cancelled" //nolint:misspell // 저장소 RowStatus와 DB CHECK의 계약 철자라, canceled로 바꾸면 저장된 행을 거부한다.
	StatusOff       Status = "off"
)

// Format은 카드 판형이다.
type Format string

const (
	Format16x9 Format = "16x9"
	Format4x3  Format = "4x3"
	Format3x4  Format = "3x4"
)

// MaxItems는 카드 하나가 받는 일정 항목 수 상한이다(저장소 행 상한과 같다).
const MaxItems = 100

// Iris가 원자적으로 받는 한 앨범의 multipart 한도다.
const (
	MaxPages      = 8
	MaxPageBytes  = 20 << 20
	MaxAlbumBytes = 30 << 20
)

var ErrRenderBudget = errors.New("schedule exceeds image budget")

// ErrInvalidCard는 카드 입력의 구조(판형, 주 범위, 항목 수, 상태, 시각과 원래 시각의 형식)가 계약을 어겼다는 뜻이다.
var ErrInvalidCard = errors.New("invalid schedule card")

// Item은 일정 항목 하나다. 글 필드는 모델 출력이라 그릴 때 정리하고 길이를 자른다.
// End는 형식만 검사하고 카드에는 시작 시각만 그린다.
// PrevStart는 같은 날 시각이 바뀐 항목의 포스터 원래 시각이고, MovedFrom은 주 안의 다른 날에서 옮겨 온 항목의 원래 날짜다.
// 둘 다 Start·Day와 같으면 바뀌지 않은 것으로 그린다.
// Converted는 다른 시간대의 시각을 JST로 바꾼 항목이다. 주 범위 밖이어도 시각이 있고 휴방이 아니면 가까운 끝 날 칸에 시각 앞 요일을 붙여 그린다.
// FromPost는 포스터가 아니라 뒤에 올라온 글이 그날 일정으로 만든 항목이다. 휴방은 같은 날 방송과 둘 다 포스터가 적은 것일 때만 방송 앞에 함께 그린다.
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

// Card는 카드 하나의 입력이다. 날짜 필드는 연·월·일만 쓰고, ExtractedAt은 JST로 바꿔 표시한다.
// Photo는 키비주얼 프로필 사진이고 nil이면 테마 색으로 채운다.
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

// Render는 card를 format 판형으로 그려 PNG 바이트를 돌려준다. 동시에 불러도 안전하지만 렌더는 한 번에 하나씩 돈다.
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

// RenderPages는 고른 양식의 모든 페이지를 표시 순서대로 돌려준다.
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
