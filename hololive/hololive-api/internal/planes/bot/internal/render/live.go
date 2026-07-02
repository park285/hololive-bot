package render

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"

	"golang.org/x/image/font"

	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type LiveCardEntry struct {
	Name  string
	Title string
	Photo string
	Chzzk bool
}

type LiveCardRenderer struct {
	strings *messagestrings.Store
}

type LiveCardRendererOption func(*LiveCardRenderer)

func WithLiveStrings(store *messagestrings.Store) LiveCardRendererOption {
	return func(r *LiveCardRenderer) {
		r.strings = store
	}
}

func NewLiveCardRenderer(options ...LiveCardRendererOption) *LiveCardRenderer {
	r := &LiveCardRenderer{}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r
}

type liveMetrics struct {
	calendarMetrics
	rowH int
}

func newLiveMetrics() liveMetrics {
	base := newCalendarMetrics()
	return liveMetrics{calendarMetrics: base, rowH: int(116 * base.sf)}
}

func (r *LiveCardRenderer) RenderLiveImages(entries []LiveCardEntry) ([][]byte, error) {
	if len(entries) == 0 {
		return nil, errors.New("live card: no entries")
	}
	photos := fetchPhotosByURL(livePhotoURLs(entries))

	fontMu.Lock()
	defer fontMu.Unlock()

	m := newLiveMetrics()
	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return nil, err
	}
	m.fonts = f
	m.strings = r.strings

	pageEntries, omitted := paginateLiveEntries(&m, entries)
	pages := make([][]byte, 0, len(pageEntries))
	for i, chunk := range pageEntries {
		footer := ""
		if omitted > 0 && i == len(pageEntries)-1 {
			footer = m.liveOverflowText(omitted)
		}
		data, encErr := encodeLivePage(&m, len(entries), chunk, footer, photos)
		if encErr != nil {
			return nil, encErr
		}
		pages = append(pages, data)
	}
	return pages, nil
}

func livePhotoURLs(entries []LiveCardEntry) []string {
	urls := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Photo != "" {
			urls = append(urls, e.Photo)
		}
	}
	return urls
}

func paginateLiveEntries(m *liveMetrics, entries []LiveCardEntry) ([][]LiveCardEntry, int) {
	base := m.headerH + separatorH + m.paddingY
	perPage := max(1, (calendarPageInnerH-base-m.paddingY)/m.rowH)

	pages := make([][]LiveCardEntry, 0, (len(entries)+perPage-1)/perPage)
	for start := 0; start < len(entries); start += perPage {
		pages = append(pages, entries[start:min(start+perPage, len(entries))])
	}
	if len(pages) <= calendarMaxPages {
		return pages, 0
	}
	omitted := 0
	for _, page := range pages[calendarMaxPages:] {
		omitted += len(page)
	}
	return pages[:calendarMaxPages], omitted
}

func encodeLivePage(m *liveMetrics, total int, entries []LiveCardEntry, footer string, photos map[string]image.Image) ([]byte, error) {
	height := m.headerH + separatorH + m.paddingY + len(entries)*m.rowH + m.paddingY
	if footer != "" {
		height += int(40 * m.sf)
	}
	height = min(height, maxCanvasH)

	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, height))
	fillRect(img, img.Bounds(), colWhite)

	drawLiveHeader(img, m, total)
	y := m.headerH + separatorH + m.paddingY
	for _, e := range entries {
		drawLiveRow(img, m, paddingX+entryIndent, y, e, photos)
		y += m.rowH
	}
	if footer != "" {
		drawText(img, m.fonts.date, paddingX, y+int(24*m.sf), colSlate500, footer)
	}

	out := downscaleToOutputWidth(img)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode live png: %w", err)
	}
	return buf.Bytes(), nil
}

func drawLiveHeader(img *image.RGBA, m *liveMetrics, total int) {
	drawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, m.liveHeaderText())
	drawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, m.liveSummaryText(total))
	fillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)
}

func drawLiveRow(img *image.RGBA, m *liveMetrics, x, y int, e LiveCardEntry, photos map[string]image.Image) {
	drawLiveAvatar(img, m, x, y, e, photos)

	nameX := x + m.avatarSize + m.avatarGap
	badgeLeft := canvasWidth - paddingX
	if e.Chzzk {
		badgeLeft = drawLiveBadge(img, m, y, m.liveChzzkBadge())
	}

	name := clampToWidth(m.fonts.name, dropUncoveredRunes(m.fonts.name, e.Name), badgeLeft-nameX-m.avatarGap)
	drawText(img, m.fonts.name, nameX, y+int(48*m.sf), colSlate800, name)

	title := clampToWidth(m.fonts.date, dropUncoveredRunes(m.fonts.date, e.Title), canvasWidth-paddingX-nameX)
	drawText(img, m.fonts.date, nameX, y+int(80*m.sf), colSlate500, title)
}

func drawLiveAvatar(img *image.RGBA, m *liveMetrics, x, y int, e LiveCardEntry, photos map[string]image.Image) {
	cx := x + m.avatarSize/2
	cy := y + m.rowH/2
	r := m.avatarSize / 2

	fillCircle(img, cx, cy, r+int(m.sf)+1, colSlate200)

	if e.Photo != "" {
		if photo, ok := photos[e.Photo]; ok {
			drawCircularImage(img, photo, cx, cy, r, colWhite)
			return
		}
	}

	fillCircle(img, cx, cy, r, colAmber600)
	initial := firstRune(dropUncoveredRunes(m.fonts.avatar, e.Name))
	if initial != "" {
		iw := measureText(m.fonts.avatar, initial)
		drawText(img, m.fonts.avatar, cx-iw/2, cy+int(12*m.sf), colWhite, initial)
	}
}

func drawLiveBadge(img *image.RGBA, m *liveMetrics, y int, text string) (badgeLeft int) {
	bw := measureText(m.fonts.badge, text)
	bx := canvasWidth - paddingX - bw - m.badgePadX*2
	by := y + (m.rowH-m.badgeH)/2
	fillRoundedRect(img, image.Rect(bx, by, bx+bw+m.badgePadX*2, by+m.badgeH), m.badgeRadius, colEmerald50)
	drawText(img, m.fonts.badge, bx+m.badgePadX, by+m.badgeH-m.badgePadY-int(2*m.sf), colEmerald600, text)
	return bx
}

// 스트림 제목은 임의 유니코드(이모지 등)를 포함할 수 있고, 임베드 폰트 밖의
// rune은 두부(notdef 박스)로 그려진다 — 그리기 전에 커버되지 않는 rune을 떨군다.
func dropUncoveredRunes(face font.Face, s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if _, ok := face.GlyphAdvance(r); ok {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func (m *liveMetrics) liveStr(key, fallback string) string {
	return m.strings.GetOr(messagestrings.NamespaceLiveCard, key, fallback)
}

func (m *liveMetrics) liveHeaderText() string {
	return m.liveStr("header", "현재 라이브")
}

func (m *liveMetrics) liveSummaryText(total int) string {
	return fmt.Sprintf(m.liveStr("summary", "총 %d건"), total)
}

func (m *liveMetrics) liveChzzkBadge() string {
	return m.liveStr("badge_chzzk", "치지직")
}

func (m *liveMetrics) liveOverflowText(omitted int) string {
	return fmt.Sprintf(m.liveStr("overflow_footer", "외 %d건 생략"), omitted)
}
