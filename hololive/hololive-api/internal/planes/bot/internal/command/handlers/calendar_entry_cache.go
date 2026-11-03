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

	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/sharedcache"
	"github.com/kapu/hololive-shared/pkg/constants"
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

// NewCachedCelebrationCalendarFinder는 월별 기념일 항목을 공유 cache에 ttl 동안 둔다. 만료는 key TTL이 맡는다.
// 주입한 store가 없거나 ttl이 0 이하이면 cache 없이 base를 그대로 쓴다.
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

		// DoChan의 goroutine panic은 호출자의 복구 경계를 벗어나므로 공유 조회 안에서 오류로 돌려준다.
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

	// 공유 조회는 첫 요청의 취소와 분리하되 기존 봇 요청 예산(10초) 안에서 끝낸다.
	// 모든 호출자가 이탈해도 이 예산 안에서 캐시를 채울 수 있다.
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), constants.RequestTimeout.BotCommand)
	defer cancel()

	entries, err := f.base.FindMembersWithCelebrationsInMonth(loadCtx, month, referenceYear)
	if err != nil {
		return nil, fmt.Errorf("find members with celebrations in month: %w", err)
	}

	cloned := cloneCalendarEntries(entries)
	f.writeSnapshot(loadCtx, key, cloned)

	return cloned, nil
}

// readSnapshot의 cache 오류와 손상 값은 miss로 보고 PostgreSQL 조회로 진행한다. 공유 cache는 정본이 아니므로
// 달력 명령 실패로 올리지 않으며, 저장소 오류만 경고 로그로 드러낸다.
func (f *cachedCelebrationCalendarFinder) readSnapshot(ctx context.Context, key string) ([]domain.CalendarEntry, bool) {
	data, ok, err := f.store.Get(ctx, key)
	if err != nil {
		logCalendarEntryCacheFailure("read", key, err)

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
		logCalendarEntryCacheFailure("write", key, err)
	}
}

func logCalendarEntryCacheFailure(operation, key string, err error) {
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
