package handlers

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/reflectutil"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

const (
	calendarEntryCacheVersion  = 1
	calendarEntryCacheMaxBytes = 2 << 20
)

type cachedCelebrationCalendarFinder struct {
	base  handlercore.CelebrationCalendarFinder
	store sharedcache.Store
	ttl   time.Duration

	flights singleflight.Group
}

type calendarEntriesSnapshot struct {
	Version int                    `json:"version"`
	Entries []domain.CalendarEntry `json:"entries"`
}

func NewCachedCelebrationCalendarFinder(
	base handlercore.CelebrationCalendarFinder,
	store sharedcache.Store,
	ttl time.Duration,
) handlercore.CelebrationCalendarFinder {
	if reflectutil.IsNil(base) {
		return nil
	}

	if reflectutil.IsNil(store) || ttl <= 0 {
		return base
	}

	return &cachedCelebrationCalendarFinder{
		base:  base,
		store: store,
		ttl:   ttl,
	}
}

func (f *cachedCelebrationCalendarFinder) FindMembersWithCelebrationsInMonth(
	ctx context.Context,
	month, referenceYear int,
) ([]domain.CalendarEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar entries request: %w", err)
	}

	key := calendarEntriesCacheKey(month, referenceYear)
	if entries, ok := f.readSnapshot(ctx, key); ok {
		return entries, nil
	}

	resultCh := f.flights.DoChan(key, func() (any, error) {
		var entries []domain.CalendarEntry

		err := panicguard.RunE(slog.Default(), panicscope.BackgroundTask, "calendar-entry-cache", func() error {
			var loadErr error

			entries, loadErr = f.loadEntriesSnapshot(ctx, key, month, referenceYear)
			if loadErr != nil {
				return fmt.Errorf("load calendar entries snapshot: %w", loadErr)
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("run shared calendar entries lookup: %w", err)
		}

		return entries, nil
	})

	var result singleflight.Result

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for calendar entries: %w", ctx.Err())
	case result = <-resultCh:
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar entries request: %w", err)
	}

	if result.Err != nil {
		return nil, fmt.Errorf("find members with celebrations in month: %w", result.Err)
	}

	entries, ok := result.Val.([]domain.CalendarEntry)
	if !ok {
		return nil, fmt.Errorf("calendar entries cache returned %T", result.Val)
	}

	return cloneCalendarEntries(entries), nil
}

func (f *cachedCelebrationCalendarFinder) loadEntriesSnapshot(ctx context.Context, key string, month, referenceYear int) ([]domain.CalendarEntry, error) {
	if entries, ok := f.readSnapshot(ctx, key); ok {
		return entries, nil
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), transport.CommandTimeout)
	defer cancel()

	entries, err := f.base.FindMembersWithCelebrationsInMonth(loadCtx, month, referenceYear)
	if err != nil {
		return nil, fmt.Errorf("find members with celebrations in month: %w", err)
	}

	cloned := cloneCalendarEntries(entries)
	f.writeSnapshot(loadCtx, key, cloned)

	return cloned, nil
}

func (f *cachedCelebrationCalendarFinder) readSnapshot(ctx context.Context, key string) ([]domain.CalendarEntry, bool) {
	data, ok, err := f.store.Get(ctx, key)
	if err != nil {
		logCalendarEntryCacheFailure(ctx, "read", key, err)

		return nil, false
	}

	if !ok || len(data) == 0 || len(data) > calendarEntryCacheMaxBytes {
		return nil, false
	}

	var snapshot calendarEntriesSnapshot

	if err := jsonv2.Unmarshal(data, &snapshot, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, false
	}

	if snapshot.Version != calendarEntryCacheVersion {
		return nil, false
	}

	return cloneCalendarEntries(snapshot.Entries), true
}

func (f *cachedCelebrationCalendarFinder) writeSnapshot(ctx context.Context, key string, entries []domain.CalendarEntry) {
	data, err := jsonv2.Marshal(calendarEntriesSnapshot{
		Version: calendarEntryCacheVersion,
		Entries: cloneCalendarEntries(entries),
	})
	if err != nil || len(data) > calendarEntryCacheMaxBytes {
		return
	}

	if err := f.store.Set(ctx, key, data, f.ttl); err != nil {
		logCalendarEntryCacheFailure(ctx, "write", key, err)
	}
}

func logCalendarEntryCacheFailure(ctx context.Context, operation, key string, err error) {
	if ctx.Err() != nil {
		return
	}

	slog.Default().Warn("calendar_entry_cache_failed",
		slog.String("operation", operation),
		privacylog.CacheKeyAttr(key),
		slog.Any("error", err),
	)
}

func calendarEntriesCacheKey(month, referenceYear int) string {
	return fmt.Sprintf("bot:calendar:entries:v%d:%04d-%02d", calendarEntryCacheVersion, referenceYear, month)
}

func cloneCalendarEntries(entries []domain.CalendarEntry) []domain.CalendarEntry {
	if entries == nil {
		return nil
	}

	cloned := make([]domain.CalendarEntry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		cloned[i].Member = cloneCalendarMember(entry.Member)
	}

	return cloned
}

func cloneCalendarMember(member *domain.Member) *domain.Member {
	if member == nil {
		return nil
	}

	cloned := *member
	if member.Aliases != nil {
		cloned.Aliases = &domain.Aliases{
			Ko: append([]string(nil), member.Aliases.Ko...),
			Ja: append([]string(nil), member.Aliases.Ja...),
		}
	}

	if member.Birthday != nil {
		birthday := *member.Birthday

		cloned.Birthday = &birthday
	}

	if member.DebutDate != nil {
		debutDate := *member.DebutDate

		cloned.DebutDate = &debutDate
	}

	return &cloned
}
