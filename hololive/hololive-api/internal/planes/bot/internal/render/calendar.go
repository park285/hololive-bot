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

	plans := r.planPages(month, year, entries, func(kind calendarPageKind) string {
		return r.variants.pick(fmt.Sprintf("%04d-%02d:%s", year, month, kind), calendarThemePool)
	})

	return r.renderPlannedPages(ctx, month, year, plans)
}

func (r *CalendarCardRenderer) RenderCalendarImagesWithTheme(ctx context.Context, month, year int, themeID string, entries []domain.CalendarEntry) ([][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar album render request: %w", err)
	}

	normalized := normalizeCalendarTheme(themeID)
	plans := r.planPages(month, year, entries, func(calendarPageKind) string {
		return normalized
	})

	return r.renderPlannedPages(ctx, month, year, plans)
}

func (r *CalendarCardRenderer) planPages(_, _ int, entries []domain.CalendarEntry, pickTheme func(calendarPageKind) string) []calendarPagePlan {
	_, globalTruncated := uniqueCalendarPhotoURLs(entries, calendarPhotoMaxFetches)

	if len(entries) == 0 {
		return []calendarPagePlan{{
			kind:            calendarPageEmpty,
			themeID:         normalizeCalendarTheme(pickTheme(calendarPageEmpty)),
			globalTruncated: globalTruncated,
		}}
	}

	var bdays, annivs []domain.CalendarEntry

	for _, e := range entries {
		if e.Kind == domain.CelebrationKindBirthday {
			bdays = append(bdays, e)
		} else {
			annivs = append(annivs, e)
		}
	}

	plans := make([]calendarPagePlan, 0, 2)

	if len(bdays) > 0 {
		plans = append(plans, calendarPagePlan{
			kind:            calendarPageBirthday,
			isBday:          true,
			themeID:         normalizeCalendarTheme(pickTheme(calendarPageBirthday)),
			entries:         bdays,
			globalTruncated: globalTruncated,
		})
	}

	if len(annivs) > 0 {
		plans = append(plans, calendarPagePlan{
			kind:            calendarPageAnniversary,
			isBday:          false,
			themeID:         normalizeCalendarTheme(pickTheme(calendarPageAnniversary)),
			entries:         annivs,
			globalTruncated: globalTruncated,
		})
	}

	return plans
}

func (r *CalendarCardRenderer) renderPlannedPages(ctx context.Context, month, year int, plans []calendarPagePlan) ([][]byte, error) {
	out := make([][]byte, 0, len(plans))

	for _, plan := range plans {
		cacheKey := newCalendarPageCacheKey(month, year, plan.kind, plan.themeID, plan.entries)

		if cached, ok := r.cachedAlbum(cacheKey); ok && len(cached) > 0 {
			out = append(out, cached[0])

			continue
		}

		rendered, err := r.renderCoalesced(ctx, cacheKey, month, year, plan)
		if err != nil {
			return nil, fmt.Errorf("render coalesced: %w", err)
		}

		out = append(out, rendered...)
	}

	return out, nil
}

func normalizeCalendarTheme(themeID string) string {
	if slices.Contains(calendarThemePool, themeID) {
		return themeID
	}

	return CalendarThemePrismDaylight
}

func (r *CalendarCardRenderer) renderCalendarPageOnce(ctx context.Context, cacheKey calendarCacheKey, month, year int, plan calendarPagePlan) ([][]byte, error) {
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

	rendered, err := r.renderSingleCalendarPage(ctx, month, year, plan)
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

func (r *CalendarCardRenderer) renderSingleCalendarPage(ctx context.Context, month, year int, plan calendarPagePlan) (renderedCalendarAlbum, error) {
	photoResult, err := fetchMemberPhotos(ctx, plan.entries)
	if err != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("fetch member photos: %w", err)
	}

	if plan.globalTruncated {
		photoResult.cachePolicy.markUncacheable()
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

	spec := r.buildSinglePageSpec(ctx, month, year, plan, photoResult)

	data, renderErr := renderCalendarPagePNG(fs, spec)
	if renderErr != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("render page: %w", renderErr)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return renderedCalendarAlbum{}, fmt.Errorf("after calendar draw: %w", ctxErr)
	}

	return renderedCalendarAlbum{
		pages:       [][]byte{data},
		cachePolicy: photoResult.cachePolicy,
	}, nil
}

func (r *CalendarCardRenderer) buildSinglePageSpec(ctx context.Context, month, year int, plan calendarPagePlan, photoResult calendarPhotoFetchResult) calPageSpec {
	var stringsStore *messagestrings.Store

	if r != nil {
		stringsStore = r.strings
	}

	m := newCalendarMetrics(1)

	m.strings = stringsStore

	return calPageSpec{
		year:    year,
		month:   month,
		themeID: normalizeCalendarTheme(plan.themeID),
		isBday:  plan.isBday,
		entries: buildCalEntries(ctx, stringsStore, plan.entries, photoResult.photos),
		empty:   cmp.Or(m.emptyText(ctx), "등록된 기념일이 없습니다."),
	}
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
