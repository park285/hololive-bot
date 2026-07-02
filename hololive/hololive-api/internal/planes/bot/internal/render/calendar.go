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

	"github.com/kapu/hololive-api/internal/planes/bot/internal/assets/fonts"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

var fontMu sync.Mutex

type CalendarCardRenderer struct {
	cacheMu      sync.Mutex
	cache        map[calendarCacheKey][][]byte
	cacheOrder   []calendarCacheKey
	rendering    singleflight.Group
	diskCacheDir string
	strings      *messagestrings.Store
}

type CalendarCardRendererOption func(*CalendarCardRenderer)

func WithCalendarDiskCacheDir(dir string) CalendarCardRendererOption {
	return func(r *CalendarCardRenderer) {
		r.diskCacheDir = strings.TrimSpace(dir)
	}
}

func WithCalendarStrings(store *messagestrings.Store) CalendarCardRendererOption {
	return func(r *CalendarCardRenderer) {
		r.strings = store
	}
}

func NewCalendarCardRenderer(options ...CalendarCardRendererOption) *CalendarCardRenderer {
	r := &CalendarCardRenderer{
		cache: make(map[calendarCacheKey][][]byte),
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

func (r *CalendarCardRenderer) RenderCalendarImages(month, year int, entries []domain.CalendarEntry) ([][]byte, error) {
	cacheKey := newCalendarCacheKey(month, year, entries)
	if pages, ok := r.cachedImages(cacheKey); ok {
		return pages, nil
	}

	result, err, _ := r.rendering.Do(cacheKey.string(), func() (any, error) {
		return r.renderCalendarPagesOnce(cacheKey, month, year, entries)
	})
	if err != nil {
		return nil, err
	}
	pages, ok := result.([][]byte)
	if !ok {
		return nil, fmt.Errorf("calendar render cache returned %T", result)
	}
	return clonePages(pages), nil
}

func (r *CalendarCardRenderer) renderCalendarPagesOnce(cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) (any, error) {
	if pages, ok := r.cachedImages(cacheKey); ok {
		return pages, nil
	}
	if pages, ok := r.diskCachedImages(cacheKey); ok {
		r.storeCachedImages(cacheKey, pages)
		return pages, nil
	}
	pages, diskCacheable, err := r.renderCalendarPages(month, year, entries)
	if err != nil {
		return nil, err
	}
	r.storeCachedImages(cacheKey, pages)
	if diskCacheable {
		r.storeDiskCachedImages(cacheKey, pages)
	}
	return pages, nil
}

func (r *CalendarCardRenderer) renderCalendarPages(month, year int, entries []domain.CalendarEntry) (pages [][]byte, diskCacheable bool, err error) {
	photos, diskCacheable := fetchMemberPhotos(entries)

	fontMu.Lock()
	defer fontMu.Unlock()

	m := newCalendarMetrics()
	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return nil, false, err
	}
	m.fonts = f
	m.strings = r.strings

	pageGroups, omitted := paginateDayGroups(&m, groupEntriesByDay(entries))

	pages = make([][]byte, 0, len(pageGroups))
	for i, groups := range pageGroups {
		footer := ""
		if omitted > 0 && i == len(pageGroups)-1 {
			footer = m.overflowText(omitted)
		}
		data, encErr := encodeCalendarPage(&m, month, year, entries, groups, footer, photos)
		if encErr != nil {
			return nil, false, encErr
		}
		pages = append(pages, data)
	}
	return pages, diskCacheable, nil
}

// 일자 그룹 경계에서만 페이지를 나눈다. 단일 그룹이 페이지 예산을 넘으면 그 그룹만으로
// 예산 초과 페이지를 만들며(축소·행 드롭 대신 세로로 긴 출력 허용), 페이지 수가
// calendarMaxPages를 넘으면 초과분을 잘라 생략 건수를 반환한다(마지막 페이지 푸터용).
func paginateDayGroups(m *calendarMetrics, groups []dayGroup) ([][]dayGroup, int) {
	if len(groups) == 0 {
		return [][]dayGroup{nil}, 0
	}

	base := m.headerH + separatorH + m.paddingY
	var pages [][]dayGroup
	var current []dayGroup
	h := base
	for _, g := range groups {
		gh := m.dateHeaderH + len(g.entries)*m.entryRowH + m.dateSectGap
		if len(current) > 0 && h+gh+m.paddingY > calendarPageInnerH {
			pages = append(pages, current)
			current, h = nil, base
		}
		current = append(current, g)
		h += gh
	}
	pages = append(pages, current)

	if len(pages) <= calendarMaxPages {
		return pages, 0
	}
	omitted := 0
	for _, page := range pages[calendarMaxPages:] {
		for _, g := range page {
			omitted += len(g.entries)
		}
	}
	return pages[:calendarMaxPages], omitted
}

func encodeCalendarPage(m *calendarMetrics, month, year int, entries []domain.CalendarEntry, groups []dayGroup, footer string, photos map[string]image.Image) ([]byte, error) {
	height := calculateCanvasHeight(m, groups)
	if footer != "" {
		height += int(40 * m.sf)
	}
	height = min(height, maxCanvasH)

	img := image.NewRGBA(image.Rect(0, 0, canvasWidth, height))
	fillRect(img, img.Bounds(), colWhite)

	drawCalendarHeader(img, m, month, year, entries)
	y := drawCalendarBody(img, m, month, groups, photos)
	if footer != "" {
		drawText(img, m.fonts.date, paddingX, y+int(24*m.sf), colSlate500, footer)
	}

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

func drawCalendarHeader(img *image.RGBA, m *calendarMetrics, month, year int, entries []domain.CalendarEntry) {
	drawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, m.headerText(year, month))

	bc, ac := countByKind(entries)
	statText := m.summaryText(len(entries), bc, ac)
	drawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, statText)

	fillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)
}

func drawCalendarBody(img *image.RGBA, m *calendarMetrics, month int, grouped []dayGroup, photos map[string]image.Image) int {
	y := m.headerH + separatorH + m.paddingY

	if len(grouped) == 0 {
		drawText(img, m.fonts.name, paddingX, y+int(24*m.sf), colSlate500, m.emptyText())
		return y + int(60*m.sf)
	}

	for _, group := range grouped {
		y = drawDayGroup(img, m, month, group, y, photos)
	}
	return y
}

func drawDayGroup(img *image.RGBA, m *calendarMetrics, month int, group dayGroup, y int, photos map[string]image.Image) int {
	drawText(img, m.fonts.date, paddingX, y+int(22*m.sf), colSlate500, m.dayText(month, group.day))
	fillRect(img, image.Rect(paddingX, y+m.dateHeaderH-separatorH, canvasWidth-paddingX, y+m.dateHeaderH), colSlate200)
	y += m.dateHeaderH

	for _, entry := range group.entries {
		drawEntryRow(img, m, paddingX+entryIndent, y, entry, photos)
		y += m.entryRowH
	}
	return y + m.dateSectGap
}

type entryStyle struct {
	accent, badgeBg color.RGBA
	badgeText       string
}

func resolveEntryStyle(m *calendarMetrics, entry domain.CalendarEntry) entryStyle {
	switch entry.Kind {
	case domain.CelebrationKindBirthday:
		return entryStyle{colAmber600, colAmber50, m.badgeBirthday()}
	case domain.CelebrationKindAnniversary:
		return entryStyle{colEmerald600, colEmerald50, m.anniversaryBadge(entry.Ordinal)}
	default:
		return entryStyle{colSlate500, colSlate100, ""}
	}
}

func drawEntryRow(img *image.RGBA, m *calendarMetrics, x, y int, entry domain.CalendarEntry, photos map[string]image.Image) {
	name := entryDisplayName(m, entry.Member)
	style := resolveEntryStyle(m, entry)

	drawEntryAvatar(img, m, x, y, entry, style.accent, name, photos)

	nameX := x + m.avatarSize + m.avatarGap
	badgeLeft := canvasWidth - paddingX
	if style.badgeText != "" {
		badgeLeft = drawEntryBadge(img, m, y, style)
	}
	name = clampToWidth(m.fonts.name, name, badgeLeft-nameX-m.avatarGap)
	drawText(img, m.fonts.name, nameX, y+m.entryRowH/2+int(8*m.sf), colSlate800, name)
}

func drawEntryAvatar(img *image.RGBA, m *calendarMetrics, x, y int, entry domain.CalendarEntry, accent color.RGBA, name string, photos map[string]image.Image) {
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

func drawEntryBadge(img *image.RGBA, m *calendarMetrics, y int, s entryStyle) (badgeLeft int) {
	bw := measureText(m.fonts.badge, s.badgeText)
	bx := canvasWidth - paddingX - bw - m.badgePadX*2
	by := y + (m.entryRowH-m.badgeH)/2
	fillRoundedRect(img, image.Rect(bx, by, bx+bw+m.badgePadX*2, by+m.badgeH), m.badgeRadius, s.badgeBg)
	drawText(img, m.fonts.badge, bx+m.badgePadX, by+m.badgeH-m.badgePadY-int(2*m.sf), s.accent, s.badgeText)
	return bx
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

func calculateCanvasHeight(m *calendarMetrics, groups []dayGroup) int {
	h := m.headerH + separatorH + m.paddingY
	if len(groups) == 0 {
		return h + int(60*m.sf) + m.paddingY
	}
	for _, g := range groups {
		h += m.dateHeaderH + len(g.entries)*m.entryRowH + m.dateSectGap
	}
	return h + m.paddingY
}

func entryDisplayName(m *calendarMetrics, member *domain.Member) string {
	if member == nil {
		return m.unknownName()
	}
	if member.ShortKoreanName != "" {
		return member.ShortKoreanName
	}
	if member.NameKo != "" {
		return member.NameKo
	}
	return member.Name
}

func (m *calendarMetrics) calStr(key, fallback string) string {
	return m.strings.GetOr(messagestrings.NamespaceCalendar, key, fallback)
}

func (m *calendarMetrics) headerText(year, month int) string {
	return fmt.Sprintf(m.calStr("header_month", "%d년 %d월 기념일"), year, month)
}

func (m *calendarMetrics) summaryText(total, birthday, anniversary int) string {
	return fmt.Sprintf(m.calStr("summary", "총 %d건 · 생일 %d · 데뷔주년 %d"), total, birthday, anniversary)
}

func (m *calendarMetrics) emptyText() string {
	return m.calStr("empty", "등록된 기념일이 없습니다.")
}

func (m *calendarMetrics) dayText(month, day int) string {
	return fmt.Sprintf(m.calStr("day", "%d월 %d일"), month, day)
}

func (m *calendarMetrics) badgeBirthday() string {
	return m.calStr("badge_birthday", "생일")
}

func (m *calendarMetrics) anniversaryBadge(ordinal int) string {
	return fmt.Sprintf(m.calStr("badge_anniversary", "데뷔 %d주년"), ordinal)
}

func (m *calendarMetrics) overflowText(omitted int) string {
	return fmt.Sprintf(m.calStr("overflow_footer", "외 %d건 생략"), omitted)
}

func (m *calendarMetrics) unknownName() string {
	return m.calStr("unknown", "알 수 없음")
}
