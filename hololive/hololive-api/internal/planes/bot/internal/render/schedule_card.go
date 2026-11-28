package render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/render/schedulecard"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

const (
	scheduleCardFormat       = schedulecard.Format3x4
	scheduleCardCacheLimit   = 32
	scheduleCardCacheVersion = "v4"
	scheduleCardCacheTTL     = 7 * 24 * time.Hour
)

type scheduleCardKey struct {
	posterID      int64
	extractedAtNS int64
	changedAtNS   int64
	editedAtNS    int64
	changes       int
	format        schedulecard.Format
	template      cardtemplate.ID
	variant       string
	memberName    string
	photo         string
	highlight     string
}

func (k scheduleCardKey) String() string {
	key := fmt.Sprintf("%d:%d:%d:%d:%d:%s:%s:%q:%q:%q", k.posterID, k.extractedAtNS, k.changedAtNS, k.editedAtNS, k.changes, k.format, k.template, k.variant, k.memberName, k.photo)
	if k.highlight != "" {
		key += ":highlight=" + k.highlight
	}

	return key
}

func (k scheduleCardKey) shared() (string, [sha256.Size]byte) {
	return fmt.Sprintf("bot:schedule:card:%s:%d:%s:%s:%s", scheduleCardCacheVersion, k.posterID, k.format, k.template, k.variant), sha256.Sum256([]byte(k.String()))
}

func newScheduleCardKey(member *domain.Member, schedule *scheduleposter.PublishedSchedule, format schedulecard.Format, template cardtemplate.ID, variant string) scheduleCardKey {
	key := scheduleCardKey{
		posterID: schedule.PosterID, extractedAtNS: schedule.ExtractedAt.UnixNano(), changes: len(schedule.Changes), format: format,
		template: template, variant: variant, memberName: member.DisplayName(), photo: member.Photo,
	}

	for i := range schedule.Rows {
		if edited := schedule.Rows[i].EditedAt; !edited.IsZero() {
			key.editedAtNS = max(key.editedAtNS, edited.UnixNano())
		}
	}

	for i := range schedule.Changes {
		key.changedAtNS = max(key.changedAtNS, schedule.Changes[i].RecordedAt.UnixNano())

		if edited := schedule.Changes[i].EditedAt; !edited.IsZero() {
			key.editedAtNS = max(key.editedAtNS, edited.UnixNano())
		}
	}

	return key
}

type ScheduleCardRenderer struct {
	format   schedulecard.Format
	cache    *imageCache[scheduleCardKey]
	shared   sharedRenderCache
	flights  singleflight.Group
	variants *variantPicker
	draw     func(context.Context, schedulecard.Card, schedulecard.Format, cardtemplate.ID) ([][]byte, error)
}

type variantPicker struct {
	mu   sync.Mutex
	last map[string]string
	intN func(int) int
}

func newVariantPicker() *variantPicker {
	return &variantPicker{last: make(map[string]string), intN: rand.IntN}
}

func (p *variantPicker) pick(channelID string, pool []string) string {
	if len(pool) == 0 {
		return ""
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	candidates := slices.DeleteFunc(slices.Clone(pool), func(v string) bool { return v == p.last[channelID] })
	if len(candidates) == 0 {
		candidates = pool
	}

	choice := candidates[p.intN(len(candidates))]

	p.last[channelID] = choice

	return choice
}

func NewScheduleCardRenderer(store sharedcache.Store) *ScheduleCardRenderer {
	r := newScheduleCardRenderer(scheduleCardFormat, scheduleCardCacheLimit, schedulecard.RenderPages)

	r.shared = newScheduleCardSharedCache(store)

	return r
}

func newScheduleCardSharedCache(store sharedcache.Store) sharedRenderCache {
	return sharedRenderCache{store: store, ttl: scheduleCardCacheTTL, event: "schedule_card_cache_failed"}
}

func newScheduleCardRenderer(format schedulecard.Format, cacheLimit int, draw func(context.Context, schedulecard.Card, schedulecard.Format, cardtemplate.ID) ([][]byte, error)) *ScheduleCardRenderer {
	return &ScheduleCardRenderer{format: format, cache: newImageCache[scheduleCardKey](cacheLimit), variants: newVariantPicker(), draw: draw}
}

func (r *ScheduleCardRenderer) RenderScheduleCards(ctx context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID) ([][]byte, error) {
	return r.render(ctx, member, schedule, template, nil)
}

func (r *ScheduleCardRenderer) RenderScheduleChangeCards(ctx context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID,
	postIDs []int64,
) ([][]byte, error) {
	if len(postIDs) == 0 || slices.ContainsFunc(postIDs, func(id int64) bool { return id <= 0 }) {
		return nil, fmt.Errorf("%w: highlight post ids must be positive, got %v", schedulecard.ErrInvalidCard, postIDs)
	}

	return r.render(ctx, member, schedule, template, slices.Sorted(slices.Values(postIDs)))
}

func (r *ScheduleCardRenderer) render(ctx context.Context, member *domain.Member, schedule scheduleposter.PublishedSchedule, template cardtemplate.ID,
	highlight []int64,
) ([][]byte, error) {
	if member == nil {
		return nil, errors.New("render schedule card: member is nil")
	}

	if !template.Valid() {
		return nil, fmt.Errorf("%w: unknown template %q", schedulecard.ErrInvalidCard, template)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	variant := r.variants.pick(member.ChannelID, schedulecard.Variants(member.ChannelID, r.format, template))
	key := newScheduleCardKey(member, &schedule, r.format, template, variant)

	key.highlight = highlightKey(highlight)

	if pages, ok := r.cachedPages(key); ok {
		return pages, nil
	}

	parent := context.WithoutCancel(ctx)

	resultCh := r.flights.DoChan(key.String(), func() (any, error) {
		workCtx, cancel := scheduleWorkContext(parent, template)
		defer cancel()

		var pages [][]byte

		err := panicguard.RunE(slog.Default(), panicscope.BackgroundTask, "schedule-card-render", func() error {
			var renderErr error

			pages, renderErr = r.renderAndStore(workCtx, key, member, schedule, highlight)

			if renderErr != nil && errors.Is(renderErr, context.DeadlineExceeded) && workCtx.Err() != nil {
				return context.Cause(workCtx)
			}

			return renderErr
		})
		if err != nil {
			return nil, fmt.Errorf("run shared schedule card render: %w", err)
		}

		return pages, nil
	})

	return waitSchedulePages(ctx, template, resultCh)
}

func waitSchedulePages(ctx context.Context, template cardtemplate.ID, resultCh <-chan singleflight.Result) ([][]byte, error) {
	waitCtx := ctx
	if deadline, ok := ctx.Deadline(); ok && template == cardtemplate.Orbit {
		var cancel context.CancelFunc

		waitCtx, cancel = context.WithTimeoutCause(ctx, time.Until(deadline)*3/4, schedulecard.ErrRenderBudget)

		defer cancel()
	}

	var result singleflight.Result

	select {
	case <-waitCtx.Done():
		return nil, fmt.Errorf("wait for schedule card: %w", context.Cause(waitCtx))
	case result = <-resultCh:
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if result.Err != nil {
		return nil, fmt.Errorf("render schedule card: %w", result.Err)
	}

	pages, ok := result.Val.([][]byte)
	if !ok || len(pages) == 0 {
		return nil, errors.New("render schedule card: missing pages")
	}

	if result.Shared {
		return cloneSchedulePages(pages), nil
	}

	return pages, nil
}

func scheduleWorkContext(parent context.Context, template cardtemplate.ID) (context.Context, context.CancelFunc) {
	const budget = 12 * time.Second

	if template == cardtemplate.Orbit {
		return context.WithTimeoutCause(parent, budget, schedulecard.ErrRenderBudget)
	}

	return context.WithTimeout(parent, photoFetchBudget+budget)
}

func (r *ScheduleCardRenderer) renderAndStore(ctx context.Context, key scheduleCardKey, member *domain.Member, schedule scheduleposter.PublishedSchedule,
	highlight []int64,
) ([][]byte, error) {
	if pages, ok := r.cachedPages(key); ok {
		return pages, nil
	}

	cacheable := len(highlight) == 0

	if cacheable {
		if pages, ok := r.sharedPages(ctx, key); ok {
			return pages, nil
		}
	}

	var photoURLs []string

	if member.Photo != "" {
		photoURLs = []string{member.Photo}
	}

	photos, err := fetchPhotos(ctx, photoURLs, false)
	if err != nil {
		return nil, fmt.Errorf("fetch schedule card photo: %w", err)
	}

	photo := photos.photos[member.Photo]

	card := newScheduleCard(member, schedule, photo, highlight)

	card.Variant = key.variant

	if key.template == cardtemplate.Orbit && len(card.Items) > schedulecard.MaxItems {
		return nil, schedulecard.ErrRenderBudget
	}

	pages, err := r.draw(ctx, card, r.format, key.template)
	if err != nil {
		return nil, fmt.Errorf("draw schedule card: %w", err)
	}

	if err := validateScheduleAlbum(pages); err != nil {
		return nil, err
	}

	if cacheable && photos.cachePolicy.memoryCacheable && (member.Photo == "" || photo != nil) {
		sharedKey, hash := key.shared()
		if record := encodeSchedulePages(hash, pages); record != nil {
			r.cache.putImmutable(key, record[len(hash):])
			r.shared.write(ctx, sharedKey, record)
		}
	}

	return pages, nil
}

func (r *ScheduleCardRenderer) sharedPages(ctx context.Context, key scheduleCardKey) ([][]byte, bool) {
	sharedKey, hash := key.shared()

	record, ok := r.shared.read(ctx, sharedKey)
	if !ok || len(record) <= len(hash) || !bytes.Equal(record[:len(hash)], hash[:]) {
		return nil, false
	}

	body := record[len(hash):]

	pages, valid := decodeSchedulePages(body)
	if !valid {
		return nil, false
	}

	r.cache.putImmutable(key, body)

	return cloneSchedulePages(pages), true
}

func validateScheduleAlbum(pages [][]byte) error {
	if len(pages) == 0 || slices.ContainsFunc(pages, func(page []byte) bool { return !isPNGData(page) }) {
		return errors.New("draw schedule card: missing PNG page")
	}

	if len(pages) > schedulecard.MaxPages {
		return schedulecard.ErrRenderBudget
	}

	remaining := schedulecard.MaxAlbumBytes

	for _, page := range pages {
		if len(page) > min(remaining, schedulecard.MaxPageBytes) {
			return schedulecard.ErrRenderBudget
		}

		remaining -= len(page)
	}

	return nil
}

func newScheduleCard(member *domain.Member, schedule scheduleposter.PublishedSchedule, photo image.Image, highlight []int64) schedulecard.Card {
	slots := schedule.Slots()
	items := make([]schedulecard.Item, len(slots))

	for i := range slots {
		slot := &slots[i]

		items[i] = schedulecard.Item{
			Day:       slot.Day,
			Start:     slot.Start,
			End:       slot.End,
			Converted: slot.SourceZone != "",
			FromPost:  fromPost(slot),
			Status:    schedulecard.Status(slot.Status),
			Title:     slot.TitleKo,
			With:      slot.WithKo,
			Place:     slot.PlaceKo,
			Tags:      slot.TagsKo,
			Note:      slot.NoteKo,
			Highlight: changedBy(slot, highlight),
		}

		if slot.TimeChanged {
			setTimeChange(&items[i], slot)
		}
	}

	return schedulecard.Card{
		ChannelID:   member.ChannelID,
		MemberName:  member.DisplayName(),
		WeekStart:   schedule.WeekStart.AddDate(0, 0, schedule.RowsDayShift),
		WeekEnd:     schedule.WeekEnd.AddDate(0, 0, schedule.RowsDayShift),
		ExtractedAt: schedule.UpdatedAt(),
		Items:       items,
		Photo:       photo,
	}
}

func setTimeChange(item *schedulecard.Item, slot *scheduleposter.Slot) {
	if slot.Day.Equal(slot.OrigDay) {
		item.PrevStart = slot.OrigStart
	} else {
		item.MovedFrom = slot.OrigDay
	}
}

func changedBy(slot *scheduleposter.Slot, postIDs []int64) bool {
	return slices.ContainsFunc(slot.Steps, func(step scheduleposter.Step) bool { return slices.Contains(postIDs, step.PostID) })
}

func highlightKey(postIDs []int64) string {
	parts := make([]string, len(postIDs))

	for i, id := range postIDs {
		parts[i] = strconv.FormatInt(id, 10)
	}

	return strings.Join(parts, ",")
}

func fromPost(slot *scheduleposter.Slot) bool {
	return slices.ContainsFunc(slot.Steps, func(step scheduleposter.Step) bool {
		return step.Kind == scheduleposter.ChangeAdded || step.Kind == scheduleposter.ChangeMoved || step.Kind == scheduleposter.ChangeOff
	})
}
