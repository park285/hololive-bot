package render

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"strings"
	"sync"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/cardkit"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type CalendarCardRenderer struct {
	cacheMu      sync.Mutex
	cache        map[calendarCacheKey][]byte
	cacheOrder   []calendarCacheKey
	renderMu     sync.Mutex
	inflight     map[calendarCacheKey]*calendarRenderCall
	diskMu       sync.Mutex
	diskCacheDir string
	strings      *messagestrings.Store
}

type calendarCachePolicy struct {
	memoryCacheable bool
	diskCacheable   bool
}

type renderedCalendarImage struct {
	data        []byte
	cachePolicy calendarCachePolicy
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

func (r *CalendarCardRenderer) RenderCalendarImageContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("calendar render context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar render request: %w", err)
	}

	cacheKey := newCalendarCacheKey(month, year, entries)
	if data, ok := r.cachedImage(cacheKey); ok {
		return data, nil
	}

	out, err := r.renderCoalesced(ctx, cacheKey, month, year, entries)
	if err != nil {
		return out, fmt.Errorf("render coalesced: %w", err)
	}

	return out, nil
}

func (r *CalendarCardRenderer) renderCalendarImageOnce(ctx context.Context, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar render attempt: %w", err)
	}

	if data, ok := r.cachedImage(cacheKey); ok {
		return data, nil
	}

	if data, ok := r.diskCachedImage(cacheKey); ok {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("after disk cache read: %w", err)
		}

		r.storeCachedImage(cacheKey, data)

		return data, nil
	}

	rendered, err := r.renderCalendarImage(ctx, month, year, entries)
	if err != nil {
		return nil, fmt.Errorf("render calendar image: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("after calendar image render: %w", err)
	}

	r.storeRenderedImage(cacheKey, rendered)

	return rendered.data, nil
}

func (r *CalendarCardRenderer) storeRenderedImage(cacheKey calendarCacheKey, rendered renderedCalendarImage) {
	if rendered.cachePolicy.memoryCacheable {
		r.storeCachedImage(cacheKey, rendered.data)
	}

	if rendered.cachePolicy.diskCacheable {
		r.storeDiskCachedImage(cacheKey, rendered.data)
	}
}

func (r *CalendarCardRenderer) renderCalendarImage(ctx context.Context, month, year int, entries []domain.CalendarEntry) (renderedCalendarImage, error) {
	photoResult, err := fetchMemberPhotos(ctx, entries)
	if err != nil {
		return renderedCalendarImage{}, fmt.Errorf("fetch member photos: %w", err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return renderedCalendarImage{}, fmt.Errorf("after member photo fetch: %w", ctxErr)
	}

	fontMu.Lock()
	defer fontMu.Unlock()

	if ctxErr2 := ctx.Err(); ctxErr2 != nil {
		return renderedCalendarImage{}, fmt.Errorf("after font lock acquire: %w", ctxErr2)
	}

	grouped := groupEntriesByDay(entries)

	m := newCalendarMetrics(calendarCompactRatio(grouped))

	f, err := loadCalendarFonts(m.sf)
	if err != nil {
		return renderedCalendarImage{}, fmt.Errorf("load calendar fonts: %w", err)
	}

	m.fonts = f
	m.strings = r.strings

	img := cardkit.NewCanvas(canvasWidth, min(calculateCanvasHeight(&m, grouped), maxCanvasH), colWhite)

	drawCalendarHeader(ctx, img, &m, month, year, entries)
	drawCalendarBody(ctx, img, &m, month, grouped, photoResult.photos)

	if ctxErr3 := ctx.Err(); ctxErr3 != nil {
		return renderedCalendarImage{}, fmt.Errorf("after calendar draw: %w", ctxErr3)
	}

	data, err := cardkit.EncodePNG(img, calendarOutputWidth)
	if err != nil {
		return renderedCalendarImage{}, fmt.Errorf("encode PNG: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return renderedCalendarImage{}, fmt.Errorf("after PNG encode: %w", err)
	}

	return renderedCalendarImage{
		data:        data,
		cachePolicy: photoResult.cachePolicy,
	}, nil
}

func calendarCompactRatio(grouped []dayGroup) float64 {
	m := newCalendarMetrics(1)
	naturalH := calculateCanvasHeight(&m, grouped)

	if naturalH <= calendarTargetInnerH {
		return 1
	}

	return float64(calendarTargetInnerH) / float64(naturalH)
}

func drawCalendarHeader(ctx context.Context, img *image.RGBA, m *calendarMetrics, month, year int, entries []domain.CalendarEntry) {
	cardkit.DrawText(img, m.fonts.title, paddingX, int(42*m.sf), colSlate800, m.headerText(ctx, year, month))

	bc, ac := countByKind(entries)
	statText := m.summaryText(ctx, len(entries), bc, ac)
	cardkit.DrawText(img, m.fonts.stat, paddingX, int(68*m.sf), colSlate500, statText)

	cardkit.FillRect(img, image.Rect(paddingX, m.headerH, canvasWidth-paddingX, m.headerH+separatorH), colSlate200)
}

func drawCalendarBody(ctx context.Context, img *image.RGBA, m *calendarMetrics, month int, grouped []dayGroup, photos map[string]image.Image) int {
	y := m.headerH + separatorH + m.paddingY

	if len(grouped) == 0 {
		cardkit.DrawText(img, m.fonts.name, paddingX, y+int(24*m.sf), colSlate500, m.emptyText(ctx))

		return y + int(60*m.sf)
	}

	for _, group := range grouped {
		y = drawDayGroup(ctx, img, m, month, group, y, photos)
	}

	return y
}

func drawDayGroup(ctx context.Context, img *image.RGBA, m *calendarMetrics, month int, group dayGroup, y int, photos map[string]image.Image) int {
	cardkit.DrawText(img, m.fonts.date, paddingX, y+int(22*m.sf), colSlate500, m.dayText(ctx, month, group.day))
	cardkit.FillRect(img, image.Rect(paddingX, y+m.dateHeaderH-separatorH, canvasWidth-paddingX, y+m.dateHeaderH), colSlate200)

	y += m.dateHeaderH

	for _, entry := range group.entries {
		drawEntryRow(ctx, img, m, paddingX+entryIndent, y, entry, photos)

		y += m.entryRowH
	}

	return y + m.dateSectGap
}

type entryStyle struct {
	accent, badgeBg color.RGBA
	badgeText       string
}

func resolveEntryStyle(ctx context.Context, m *calendarMetrics, entry domain.CalendarEntry) entryStyle {
	switch entry.Kind {
	case domain.CelebrationKindBirthday:
		return entryStyle{colAmber600, colAmber50, m.badgeBirthday(ctx)}
	case domain.CelebrationKindAnniversary:
		return entryStyle{colEmerald600, colEmerald50, m.anniversaryBadge(ctx, entry.Ordinal)}
	case domain.CelebrationKindBirthdayStream:
		return entryStyle{colSlate500, colSlate100, ""}
	default:
		return entryStyle{colSlate500, colSlate100, ""}
	}
}

func drawEntryRow(ctx context.Context, img *image.RGBA, m *calendarMetrics, x, y int, entry domain.CalendarEntry, photos map[string]image.Image) {
	name := entryDisplayName(ctx, m, entry.Member)
	style := resolveEntryStyle(ctx, m, entry)

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
	case domain.CelebrationKindBirthdayStream:
	}
}
