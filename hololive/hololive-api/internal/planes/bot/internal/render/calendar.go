package render

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type CalendarCardRenderer struct {
	cacheMu      sync.Mutex
	cache        map[calendarCacheKey][]byte
	cacheOrder   []calendarCacheKey
	rendering    singleflight.Group
	diskMu       sync.Mutex
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
		cache: make(map[calendarCacheKey][]byte),
	}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	return r
}

func (r *CalendarCardRenderer) RenderCalendarImage(month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	cacheKey := newCalendarCacheKey(month, year, entries)
	if data, ok := r.cachedImage(cacheKey); ok {
		return data, nil
	}

	result, err, _ := r.rendering.Do(cacheKey.string(), func() (any, error) {
		return r.renderCalendarImageOnce(cacheKey, month, year, entries)
	})
	if err != nil {
		return nil, err
	}
	data, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("calendar render cache returned %T", result)
	}
	return bytes.Clone(data), nil
}

func (r *CalendarCardRenderer) renderCalendarImageOnce(cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) (any, error) {
	if data, ok := r.cachedImage(cacheKey); ok {
		return data, nil
	}
	if data, ok := r.diskCachedImage(cacheKey); ok {
		r.storeCachedImage(cacheKey, data)
		return data, nil
	}
	data, diskCacheable, err := r.renderCalendarImage(month, year, entries)
	if err != nil {
		return nil, err
	}
	r.storeCachedImage(cacheKey, data)
	if diskCacheable {
		r.storeDiskCachedImage(cacheKey, data)
	}
	return data, nil
}

func (r *CalendarCardRenderer) renderCalendarImage(month, year int, entries []domain.CalendarEntry) (data []byte, diskCacheable bool, err error) {
	photos, diskCacheable := fetchMemberPhotos(entries)

	fontMu.Lock()
	defer fontMu.Unlock()

	grouped := groupEntriesByDay(entries)

	m := newCalendarMetrics(calendarCompactRatio(grouped))
	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return nil, false, err
	}
	m.fonts = f
	m.strings = r.strings

	img := cardkit.NewCanvas(canvasWidth, min(calculateCanvasHeight(&m, grouped), maxCanvasH), colWhite)

	drawCalendarHeader(img, &m, month, year, entries)
	drawCalendarBody(img, &m, month, grouped, photos)

	data, err = cardkit.EncodePNG(img, calendarOutputWidth)
	if err != nil {
		return nil, false, err
	}
	return data, diskCacheable, nil
}

func calendarCompactRatio(grouped []dayGroup) float64 {
	m := newCalendarMetrics(1)
	naturalH := calculateCanvasHeight(&m, grouped)
	if naturalH <= calendarTargetInnerH {
		return 1
	}
	return float64(calendarTargetInnerH) / float64(naturalH)
}

func drawCalendarHeader(img *image.RGBA, m *calendarMetrics, month, year int, entries []domain.CalendarEntry) {
	cardkit.DrawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, m.headerText(year, month))

	bc, ac := countByKind(entries)
	statText := m.summaryText(len(entries), bc, ac)
	cardkit.DrawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, statText)

	cardkit.FillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)
}

func drawCalendarBody(img *image.RGBA, m *calendarMetrics, month int, grouped []dayGroup, photos map[string]image.Image) int {
	y := m.headerH + separatorH + m.paddingY

	if len(grouped) == 0 {
		cardkit.DrawText(img, m.fonts.name, paddingX, y+int(24*m.sf), colSlate500, m.emptyText())
		return y + int(60*m.sf)
	}

	for _, group := range grouped {
		y = drawDayGroup(img, m, month, group, y, photos)
	}
	return y
}

func drawDayGroup(img *image.RGBA, m *calendarMetrics, month int, group dayGroup, y int, photos map[string]image.Image) int {
	cardkit.DrawText(img, m.fonts.date, paddingX, y+int(22*m.sf), colSlate500, m.dayText(month, group.day))
	cardkit.FillRect(img, image.Rect(paddingX, y+m.dateHeaderH-separatorH, canvasWidth-paddingX, y+m.dateHeaderH), colSlate200)
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

	var photo image.Image
	if entry.Member != nil {
		photo = photos[entry.Member.Photo]
	}
	cardkit.AvatarCircle(img, x+m.avatarSize/2, y+m.entryRowH/2, m.avatarSize/2, photo, name, m.entryAvatarStyle(style.accent))

	nameX := x + m.avatarSize + m.avatarGap
	badgeLeft := canvasWidth - paddingX
	if style.badgeText != "" {
		by := y + (m.entryRowH-m.badgeH)/2
		badgeLeft = cardkit.BadgeRightAligned(img, canvasWidth-paddingX, by, style.badgeText, m.entryBadgeStyle(style))
	}
	name = cardkit.ClampToWidth(m.fonts.name, name, badgeLeft-nameX-m.avatarGap)
	cardkit.DrawText(img, m.fonts.name, nameX, y+m.entryRowH/2+int(8*m.sf), colSlate800, name)
}

func (m *calendarMetrics) entryAvatarStyle(accent color.RGBA) cardkit.AvatarStyle {
	return cardkit.AvatarStyle{
		Ring:        colSlate200,
		RingWidth:   int(m.sf) + 1,
		Accent:      accent,
		Background:  colWhite,
		Initials:    m.fonts.avatar,
		TextColor:   colWhite,
		InitialDrop: int(12 * m.sf),
	}
}

func (m *calendarMetrics) entryBadgeStyle(s entryStyle) cardkit.BadgeStyle {
	return cardkit.BadgeStyle{
		Face:         m.fonts.badge,
		Background:   s.badgeBg,
		Text:         s.accent,
		PadX:         m.badgePadX,
		PadY:         m.badgePadY,
		Height:       m.badgeH,
		Radius:       m.badgeRadius,
		BaselineLift: int(2 * m.sf),
	}
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

func (m *calendarMetrics) unknownName() string {
	return m.calStr("unknown", "알 수 없음")
}
