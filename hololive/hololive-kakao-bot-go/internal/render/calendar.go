package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"unicode/utf8"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-kakao-bot-go/internal/assets/fonts"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var fontMu sync.Mutex

type CalendarCardRenderer struct {
	cacheMu      sync.Mutex
	cache        map[calendarCacheKey][]byte
	cacheOrder   []calendarCacheKey
	rendering    singleflight.Group
	diskCacheDir string
}

type CalendarCardRendererOption func(*CalendarCardRenderer)

func WithCalendarDiskCacheDir(dir string) CalendarCardRendererOption {
	return func(r *CalendarCardRenderer) {
		r.diskCacheDir = strings.TrimSpace(dir)
	}
}

func NewCalendarCardRenderer(options ...CalendarCardRendererOption) *CalendarCardRenderer {
	r := &CalendarCardRenderer{
		cache: make(map[calendarCacheKey][]byte),
	}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r
}

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

func (r *CalendarCardRenderer) RenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	cacheKey := newCalendarCacheKey(month, year, entries)
	if data, ok := r.cachedImage(cacheKey); ok {
		return data, nil
	}

	data, err, _ := r.rendering.Do(cacheKey.string(), func() (any, error) {
		return r.renderCalendarImageOnce(cacheKey, month, year, entries)
	})
	if err != nil {
		return nil, err
	}
	rendered, ok := data.([]byte)
	if !ok {
		return nil, fmt.Errorf("calendar render cache returned %T", data)
	}
	return bytes.Clone(rendered), nil
}

func (r *CalendarCardRenderer) renderCalendarImageOnce(cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) (any, error) {
	if data, ok := r.cachedImage(cacheKey); ok {
		return data, nil
	}
	if data, ok := r.diskCachedImage(cacheKey); ok {
		r.storeCachedImage(cacheKey, data)
		return data, nil
	}
	data, err := r.renderCalendarImage(month, year, entries)
	if err != nil {
		return nil, err
	}
	r.storeCachedImage(cacheKey, data)
	r.storeDiskCachedImage(cacheKey, data)
	return data, nil
}

func (r *CalendarCardRenderer) renderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	photos := fetchMemberPhotos(entries)

	fontMu.Lock()
	defer fontMu.Unlock()

	grouped := groupEntriesByDay(entries)

	// 자연 높이(compact=1)가 출력 비율(1024x1536)에 대응하는 내부 높이를 넘으면
	// compact<1로 행·아바타·폰트를 비례 축소해 1024x1536 안에 담는다.
	naturalH := calculateCanvasHeight(newCalendarMetrics(1), grouped)
	targetInnerH := canvasWidth * calendarOutputHeight / calendarOutputWidth
	compact := 1.0
	if naturalH > targetInnerH {
		compact = float64(targetInnerH) / float64(naturalH)
	}
	m := newCalendarMetrics(compact)

	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return nil, err
	}
	m.fonts = f

	height := min(calculateCanvasHeight(m, grouped), maxCanvasH)

	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, height))
	fillRect(img, img.Bounds(), colWhite)

	drawCalendarHeader(img, m, month, year, entries)
	drawCalendarBody(img, m, month, grouped, photos)

	out := downscaleToOutputWidth(img)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode calendar png: %w", err)
	}
	return buf.Bytes(), nil
}

// 고해상도 캔버스를 카카오 표시폭(calendarOutputWidth)으로 다운스케일한다.
// 큰 캔버스→작은 출력 = 슈퍼샘플링(SSAA)이라 텍스트·아바타가 선명해지고,
// 카카오가 인라인 표시 시 추가 다운스케일/재압축을 거의 하지 않아 화질 손실이 작다.
func downscaleToOutputWidth(src *image.RGBA) image.Image {
	b := src.Bounds()
	if b.Dx() <= calendarOutputWidth {
		return src
	}
	nw := calendarOutputWidth
	nh := b.Dy() * nw / b.Dx()
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

func drawCalendarHeader(img *image.RGBA, m calendarMetrics, month, year int, entries []domain.CalendarEntry) {
	drawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, fmt.Sprintf("%d년 %d월 기념일", year, month))

	bc, ac := countByKind(entries)
	statText := fmt.Sprintf("총 %d건 · 생일 %d · 데뷔주년 %d", len(entries), bc, ac)
	drawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, statText)

	fillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)
}

func drawCalendarBody(img *image.RGBA, m calendarMetrics, month int, grouped []dayGroup, photos map[string]image.Image) {
	y := m.headerH + separatorH + m.paddingY

	if len(grouped) == 0 {
		drawText(img, m.fonts.name, paddingX, y+int(24*m.sf), colSlate500, "등록된 기념일이 없습니다.")
		return
	}

	for _, group := range grouped {
		if y >= maxCanvasH-m.paddingY {
			break
		}
		y = drawDayGroup(img, m, month, group, y, photos)
	}
}

func drawDayGroup(img *image.RGBA, m calendarMetrics, month int, group dayGroup, y int, photos map[string]image.Image) int {
	drawText(img, m.fonts.date, paddingX, y+int(22*m.sf), colSlate500, fmt.Sprintf("%d월 %d일", month, group.day))
	fillRect(img, image.Rect(paddingX, y+m.dateHeaderH-separatorH, canvasWidth-paddingX, y+m.dateHeaderH), colSlate200)
	y += m.dateHeaderH

	for _, entry := range group.entries {
		if y >= maxCanvasH-m.paddingY {
			break
		}
		drawEntryRow(img, m, paddingX+entryIndent, y, entry, photos)
		y += m.entryRowH
	}
	return y + m.dateSectGap
}

type entryStyle struct {
	accent, badgeBg color.RGBA
	badgeText       string
}

func resolveEntryStyle(entry domain.CalendarEntry) entryStyle {
	switch entry.Kind {
	case domain.CelebrationKindBirthday:
		return entryStyle{colAmber600, colAmber50, "생일"}
	case domain.CelebrationKindAnniversary:
		return entryStyle{colEmerald600, colEmerald50, fmt.Sprintf("데뷔 %d주년", entry.Ordinal)}
	default:
		return entryStyle{colSlate500, colSlate100, ""}
	}
}

func drawEntryRow(img *image.RGBA, m calendarMetrics, x, y int, entry domain.CalendarEntry, photos map[string]image.Image) {
	name := entryDisplayName(entry.Member)
	style := resolveEntryStyle(entry)

	drawEntryAvatar(img, m, x, y, entry, style.accent, name, photos)

	nameX := x + m.avatarSize + m.avatarGap
	drawText(img, m.fonts.name, nameX, y+m.entryRowH/2+int(8*m.sf), colSlate800, name)

	if style.badgeText != "" {
		drawEntryBadge(img, m, y, style)
	}
}

func drawEntryAvatar(img *image.RGBA, m calendarMetrics, x, y int, entry domain.CalendarEntry, accent color.RGBA, name string, photos map[string]image.Image) {
	cx := x + m.avatarSize/2
	cy := y + m.entryRowH/2
	r := m.avatarSize / 2

	fillCircle(img, cx, cy, r+int(m.sf)+1, colSlate200)

	if entry.Member != nil && entry.Member.Photo != "" {
		if photo, ok := photos[entry.Member.Photo]; ok {
			drawCircularImage(img, photo, cx, cy, r, colWhite)
			return
		}
	}

	fillCircle(img, cx, cy, r, accent)
	initial := firstRune(name)
	if initial != "" {
		iw := measureText(m.fonts.avatar, initial)
		drawText(img, m.fonts.avatar, cx-iw/2, cy+int(12*m.sf), colWhite, initial)
	}
}

func drawEntryBadge(img *image.RGBA, m calendarMetrics, y int, s entryStyle) {
	bw := measureText(m.fonts.badge, s.badgeText)
	bx := canvasWidth - paddingX - bw - m.badgePadX*2
	by := y + (m.entryRowH-m.badgeH)/2
	fillRoundedRect(img, image.Rect(bx, by, bx+bw+m.badgePadX*2, by+m.badgeH), m.badgeRadius, s.badgeBg)
	drawText(img, m.fonts.badge, bx+m.badgePadX, by+m.badgeH-m.badgePadY-int(2*m.sf), s.accent, s.badgeText)
}

func firstRune(s string) string {
	if s == "" {
		return ""
	}
	r, _ := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return ""
	}
	return string(r)
}

func countByKind(entries []domain.CalendarEntry) (birthday, anniversary int) {
	for _, e := range entries {
		addKindCount(e.Kind, &birthday, &anniversary)
	}
	return
}

func addKindCount(kind domain.CelebrationKind, birthday, anniversary *int) {
	switch kind {
	case domain.CelebrationKindBirthday:
		(*birthday)++
	case domain.CelebrationKindAnniversary:
		(*anniversary)++
	}
}

type dayGroup struct {
	day     int
	entries []domain.CalendarEntry
}

func groupEntriesByDay(entries []domain.CalendarEntry) []dayGroup {
	var groups []dayGroup
	var current *dayGroup
	for _, e := range entries {
		if current == nil || current.day != e.Day {
			if current != nil {
				groups = append(groups, *current)
			}
			current = &dayGroup{day: e.Day}
		}
		current.entries = append(current.entries, e)
	}
	if current != nil {
		groups = append(groups, *current)
	}
	return groups
}

func calculateCanvasHeight(m calendarMetrics, groups []dayGroup) int {
	h := m.headerH + separatorH + m.paddingY
	if len(groups) == 0 {
		return h + int(60*m.sf) + m.paddingY
	}
	for _, g := range groups {
		h += m.dateHeaderH + len(g.entries)*m.entryRowH + m.dateSectGap
	}
	return h + m.paddingY
}

func entryDisplayName(m *domain.Member) string {
	if m == nil {
		return "알 수 없음"
	}
	if m.ShortKoreanName != "" {
		return m.ShortKoreanName
	}
	if m.NameKo != "" {
		return m.NameKo
	}
	return m.Name
}
