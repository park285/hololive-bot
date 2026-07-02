package render

import (
	"errors"
	"fmt"
	"image"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
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

func paginateLiveEntries(m *liveMetrics, entries []LiveCardEntry) (pages [][]LiveCardEntry, omitted int) {
	base := m.headerH + separatorH + m.paddingY
	perPage := max(1, (calendarPageInnerH-base-m.paddingY)/m.rowH)

	pages = make([][]LiveCardEntry, 0, (len(entries)+perPage-1)/perPage)
	for start := 0; start < len(entries); start += perPage {
		pages = append(pages, entries[start:min(start+perPage, len(entries))])
	}
	if len(pages) <= calendarMaxPages {
		return pages, 0
	}
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

	img := cardkit.NewCanvas(canvasWidth, min(height, maxCanvasH), colWhite)

	drawLiveHeader(img, m, total)
	y := m.headerH + separatorH + m.paddingY
	for _, e := range entries {
		drawLiveRow(img, m, paddingX+entryIndent, y, e, photos)
		y += m.rowH
	}
	if footer != "" {
		cardkit.DrawText(img, m.fonts.date, paddingX, y+int(24*m.sf), colSlate500, footer)
	}

	return cardkit.EncodePNG(img, calendarOutputWidth)
}

func drawLiveHeader(img *image.RGBA, m *liveMetrics, total int) {
	cardkit.DrawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, m.liveHeaderText())
	cardkit.DrawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, m.liveSummaryText(total))
	cardkit.FillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)
}

func drawLiveRow(img *image.RGBA, m *liveMetrics, x, y int, e LiveCardEntry, photos map[string]image.Image) {
	cardkit.AvatarCircle(img, x+m.avatarSize/2, y+m.rowH/2, m.avatarSize/2, photos[e.Photo], e.Name, m.avatarStyle())

	nameX := x + m.avatarSize + m.avatarGap
	badgeLeft := canvasWidth - paddingX
	if e.Chzzk {
		by := y + (m.rowH-m.badgeH)/2
		badgeLeft = cardkit.BadgeRightAligned(img, canvasWidth-paddingX, by, m.liveChzzkBadge(), m.chzzkBadgeStyle())
	}

	name := cardkit.ClampToWidth(m.fonts.name, cardkit.DropUncoveredRunes(m.fonts.name, e.Name), badgeLeft-nameX-m.avatarGap)
	cardkit.DrawText(img, m.fonts.name, nameX, y+int(48*m.sf), colSlate800, name)

	title := cardkit.ClampToWidth(m.fonts.date, cardkit.DropUncoveredRunes(m.fonts.date, e.Title), canvasWidth-paddingX-nameX)
	cardkit.DrawText(img, m.fonts.date, nameX, y+int(80*m.sf), colSlate500, title)
}

func (m *liveMetrics) avatarStyle() cardkit.AvatarStyle {
	return cardkit.AvatarStyle{
		Ring:        colSlate200,
		RingWidth:   int(m.sf) + 1,
		Accent:      colAmber600,
		Background:  colWhite,
		Initials:    m.fonts.avatar,
		TextColor:   colWhite,
		InitialDrop: int(12 * m.sf),
	}
}

func (m *liveMetrics) chzzkBadgeStyle() cardkit.BadgeStyle {
	return cardkit.BadgeStyle{
		Face:         m.fonts.badge,
		Background:   colEmerald50,
		Text:         colEmerald600,
		PadX:         m.badgePadX,
		PadY:         m.badgePadY,
		Height:       m.badgeH,
		Radius:       m.badgeRadius,
		BaselineLift: int(2 * m.sf),
	}
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
