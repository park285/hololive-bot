package render

import (
	"cmp"
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"sync"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type CalendarCardRenderer struct {
	cache       *imageCache[calendarCacheKey]
	renderMu    sync.Mutex
	inflight    map[calendarCacheKey]*calendarRenderCall
	sharedCache sharedRenderCache
	variants    *variantPicker
	strings     *messagestrings.Store
}

type calendarCachePolicy struct {
	memoryCacheable bool
	diskCacheable   bool
}

type renderedCalendarAlbum struct {
	pages       [][]byte
	cachePolicy calendarCachePolicy
}

type CalendarCardRendererOption func(*CalendarCardRenderer)

func WithCalendarImageCache(store sharedcache.Store) CalendarCardRendererOption {
	return func(r *CalendarCardRenderer) {
		r.sharedCache = sharedRenderCache{store: store, ttl: calendarImageCacheTTL, event: "calendar_image_cache_failed"}
	}
}

func WithCalendarStrings(store *messagestrings.Store) CalendarCardRendererOption {
	return func(r *CalendarCardRenderer) {
		r.strings = store
	}
}

func NewCalendarCardRenderer(options ...CalendarCardRendererOption) *CalendarCardRenderer {
	r := &CalendarCardRenderer{
		cache:    newImageCache[calendarCacheKey](calendarImageCacheLimit),
		variants: newVariantPicker(),
	}

	for _, option := range options {
		if option != nil {
			option(r)
		}
	}

	return r
}

func (r *CalendarCardRenderer) RenderCalendarImagesContext(ctx context.Context, month, year int, entries []domain.CalendarEntry) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar album render request: %w", err)
	}

	themeID := r.variants.pick(fmt.Sprintf("%04d-%02d", year, month), calendarThemePool)

	return r.RenderCalendarImagesWithTheme(ctx, month, year, themeID, entries)
}

func (r *CalendarCardRenderer) RenderCalendarImagesWithTheme(ctx context.Context, month, year int, themeID string, entries []domain.CalendarEntry) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar album render request: %w", err)
	}

	themeID = normalizeCalendarTheme(themeID)

	cacheKey := newCalendarCacheKey(month, year, themeID, entries)

	if pages, ok := r.cachedAlbum(cacheKey); ok {
		return pages, nil
	}

	pages, err := r.renderCoalesced(ctx, cacheKey, month, year, entries)
	if err != nil {
		return nil, fmt.Errorf("render coalesced: %w", err)
	}

	return pages, nil
}

func normalizeCalendarTheme(themeID string) string {
	if slices.Contains(calendarThemePool, themeID) {
		return themeID
	}

	return CalendarThemePrismDaylight
}

func (r *CalendarCardRenderer) renderCalendarAlbumOnce(ctx context.Context, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar album render attempt: %w", err)
	}

	if pages, ok := r.cachedAlbum(cacheKey); ok {
		return pages, nil
	}

	if pages, raw, ok := r.sharedCachedAlbum(ctx, cacheKey); ok {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("after shared album cache read: %w", err)
		}

		r.cache.putImmutable(cacheKey, raw)

		return pages, nil
	}

	rendered, err := r.renderCalendarAlbum(ctx, month, year, normalizeCalendarTheme(cacheKey.theme), entries)
	if err != nil {
		return nil, fmt.Errorf("render calendar album: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("after calendar album render: %w", err)
	}

	r.storeRenderedAlbum(ctx, cacheKey, rendered)

	return rendered.pages, nil
}

func (r *CalendarCardRenderer) storeRenderedAlbum(ctx context.Context, cacheKey calendarCacheKey, rendered renderedCalendarAlbum) {
	hash := sha256.Sum256([]byte(cacheKey.entriesHash))
	record := encodeSchedulePages(hash, rendered.pages)

	if record == nil {
		return
	}

	if rendered.cachePolicy.memoryCacheable {
		r.cache.putImmutable(cacheKey, record[len(hash):])
	}

	if rendered.cachePolicy.diskCacheable {
		r.storeSharedCachedAlbum(ctx, cacheKey, rendered.pages)
	}
}

func (r *CalendarCardRenderer) renderCalendarAlbum(ctx context.Context, month, year int, themeID string, entries []domain.CalendarEntry) (renderedCalendarAlbum, error) {
	photoResult, err := fetchMemberPhotos(ctx, entries)
	if err != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("fetch member photos: %w", err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("after member photo fetch: %w", ctxErr)
	}

	fs, err := loadCalendarFontSet()
	if err != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("load calendar fonts: %w", err)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("after calendar font load: %w", ctxErr)
	}

	specs := r.buildPageSpecs(ctx, month, year, themeID, entries, photoResult)
	pages := make([][]byte, 0, len(specs))

	for _, spec := range specs {
		data, renderErr := renderCalendarPagePNG(fs, spec)
		if renderErr != nil {
			return renderedCalendarAlbum{}, fmt.Errorf("render page: %w", renderErr)
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return renderedCalendarAlbum{}, fmt.Errorf("after calendar draw: %w", ctxErr)
		}

		pages = append(pages, data)
	}

	return renderedCalendarAlbum{
		pages:       pages,
		cachePolicy: photoResult.cachePolicy,
	}, nil
}

func (r *CalendarCardRenderer) buildPageSpecs(ctx context.Context, month, year int, themeID string, entries []domain.CalendarEntry, photoResult calendarPhotoFetchResult) []calPageSpec {
	var stringsStore *messagestrings.Store

	if r != nil {
		stringsStore = r.strings
	}

	m := newCalendarMetrics(1)

	m.strings = stringsStore

	emptyLabel := cmp.Or(m.emptyText(ctx), "등록된 기념일이 없습니다.")

	all := buildCalEntries(ctx, stringsStore, entries, photoResult.photos)
	if len(all) == 0 {
		return []calPageSpec{{
			year:    year,
			month:   month,
			themeID: themeID,
			isBday:  false,
			empty:   emptyLabel,
		}}
	}

	var bdays, annivs []calEntry

	for _, e := range all {
		if e.kind == domain.CelebrationKindBirthday {
			bdays = append(bdays, e)
		} else {
			annivs = append(annivs, e)
		}
	}

	var specs []calPageSpec

	if len(bdays) > 0 {
		specs = append(specs, calPageSpec{
			year:    year,
			month:   month,
			themeID: themeID,
			isBday:  true,
			entries: bdays,
			empty:   emptyLabel,
		})
	}

	if len(annivs) > 0 {
		specs = append(specs, calPageSpec{
			year:    year,
			month:   month,
			themeID: themeID,
			isBday:  false,
			entries: annivs,
			empty:   emptyLabel,
		})
	}

	return specs
}

func entryDisplayName(ctx context.Context, m *calendarMetrics, member *domain.Member) string {
	if member == nil {
		return m.unknownName(ctx)
	}

	if member.ShortKoreanName != "" {
		return member.ShortKoreanName
	}

	if member.NameKo != "" {
		return member.NameKo
	}

	return member.Name
}

func (m *calendarMetrics) calStr(key messagestrings.Key) string {
	return m.strings.Text(key)
}

func (m *calendarMetrics) headerText(_ context.Context, year, month int) string {
	return fmt.Sprintf(m.calStr(messagestrings.CalendarHeaderMonth), year, month)
}

func (m *calendarMetrics) summaryText(_ context.Context, total, birthday, anniversary int) string {
	return fmt.Sprintf(m.calStr(messagestrings.CalendarSummary), total, birthday, anniversary)
}

func (m *calendarMetrics) emptyText(_ context.Context) string {
	return m.calStr(messagestrings.CalendarEmpty)
}

func (m *calendarMetrics) dayText(_ context.Context, month, day int) string {
	return fmt.Sprintf(m.calStr(messagestrings.CalendarDay), month, day)
}

func (m *calendarMetrics) badgeBirthday(_ context.Context) string {
	return m.calStr(messagestrings.CalendarBadgeBirthday)
}

func (m *calendarMetrics) anniversaryBadge(_ context.Context, ordinal int) string {
	return fmt.Sprintf(m.calStr(messagestrings.CalendarBadgeAnniversary), ordinal)
}

func (m *calendarMetrics) unknownName(_ context.Context) string {
	return m.calStr(messagestrings.CalendarUnknown)
}
