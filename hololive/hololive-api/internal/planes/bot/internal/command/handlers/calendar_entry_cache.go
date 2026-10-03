package handlers

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/singleflight"

	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	calendarEntryCacheVersion  = 1
	calendarEntryCacheMaxBytes = 2 << 20
)

type cachedCelebrationCalendarFinder struct {
	base handlercore.CelebrationCalendarFinder
	dir  string
	ttl  time.Duration
	now  func() time.Time

	flights singleflight.Group
}

type calendarEntriesSnapshot struct {
	Version  int                    `json:"version"`
	CachedAt time.Time              `json:"cachedAt"`
	Entries  []domain.CalendarEntry `json:"entries"`
}

func NewCachedCelebrationCalendarFinder(base handlercore.CelebrationCalendarFinder, dir string, ttl time.Duration) handlercore.CelebrationCalendarFinder {
	return newCachedCelebrationCalendarFinder(base, dir, ttl, time.Now)
}

func newCachedCelebrationCalendarFinder(
	base handlercore.CelebrationCalendarFinder,
	dir string,
	ttl time.Duration,
	now func() time.Time,
) handlercore.CelebrationCalendarFinder {
	if isNilCelebrationCalendarFinder(base) {
		return nil
	}

	dir = strings.TrimSpace(dir)
	if dir == "" || ttl <= 0 {
		return base
	}

	if now == nil {
		now = time.Now
	}

	return &cachedCelebrationCalendarFinder{
		base: base,
		dir:  dir,
		ttl:  ttl,
		now:  now,
	}
}

func (f *cachedCelebrationCalendarFinder) FindMembersWithCelebrationsInMonth(
	ctx context.Context,
	month, referenceYear int,
) ([]domain.CalendarEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("calendar entries request: %w", err)
	}

	path := f.snapshotPath(month, referenceYear)
	if entries, ok := f.readSnapshot(path); ok {
		return entries, nil
	}

	resultCh := f.flights.DoChan(fmt.Sprintf("%04d-%02d", referenceYear, month), func() (any, error) {
		var entries []domain.CalendarEntry

		// DoChan의 goroutine panic은 호출자의 복구 경계를 벗어나므로 공유 조회 안에서 오류로 돌려준다.
		err := panicguard.RunE(slog.Default(), panicguard.BackgroundTask, "calendar-entry-cache", func() error {
			var loadErr error

			entries, loadErr = f.loadEntriesSnapshot(ctx, path, month, referenceYear)
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

func (f *cachedCelebrationCalendarFinder) loadEntriesSnapshot(ctx context.Context, path string, month, referenceYear int) ([]domain.CalendarEntry, error) {
	if entries, ok := f.readSnapshot(path); ok {
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
	f.writeSnapshot(path, cloned)

	return cloned, nil
}

func (f *cachedCelebrationCalendarFinder) readSnapshot(path string) ([]domain.CalendarEntry, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > calendarEntryCacheMaxBytes {
		return nil, false
	}

	data, err := readCacheFile(path)
	if err != nil {
		return nil, false
	}

	var snapshot calendarEntriesSnapshot

	if err := jsonv2.Unmarshal(data, &snapshot, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, false
	}

	if snapshot.Version != calendarEntryCacheVersion || snapshot.CachedAt.IsZero() {
		return nil, false
	}

	if f.now().After(snapshot.CachedAt.Add(f.ttl)) {
		return nil, false
	}

	return cloneCalendarEntries(snapshot.Entries), true
}

func (f *cachedCelebrationCalendarFinder) writeSnapshot(path string, entries []domain.CalendarEntry) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return
	}

	snapshot := calendarEntriesSnapshot{
		Version:  calendarEntryCacheVersion,
		CachedAt: f.now().UTC(),
		Entries:  cloneCalendarEntries(entries),
	}
	data, err := jsonv2.Marshal(snapshot)

	if err != nil || len(data) > calendarEntryCacheMaxBytes {
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return
	}

	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()

	if writeErr != nil || closeErr != nil {
		removeFile(tmpName)

		return
	}

	if err := os.Rename(tmpName, path); err != nil {
		removeFile(tmpName)
	}
}

func removeFile(path string) {
	if err := os.Remove(path); err != nil {
		return
	}
}

func (f *cachedCelebrationCalendarFinder) snapshotPath(month, referenceYear int) string {
	filename := fmt.Sprintf("%04d-%02d.json", referenceYear, month)
	return filepath.Join(f.dir, "entries", "v1", filename)
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

func isNilCelebrationCalendarFinder(base handlercore.CelebrationCalendarFinder) bool {
	if base == nil {
		return true
	}

	value := reflect.ValueOf(base)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	case reflect.Invalid,
		reflect.Bool,
		reflect.Int,
		reflect.Int8,
		reflect.Int16,
		reflect.Int32,
		reflect.Int64,
		reflect.Uint,
		reflect.Uint8,
		reflect.Uint16,
		reflect.Uint32,
		reflect.Uint64,
		reflect.Uintptr,
		reflect.Float32,
		reflect.Float64,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.String,
		reflect.Struct,
		reflect.UnsafePointer:
		return false
	default:
		return false
	}
}

func readCacheFile(path string) ([]byte, error) {
	cleaned := filepath.Clean(path)
	dir, name := filepath.Split(cleaned)

	if dir == "" || name == "" || !fs.ValidPath(name) {
		return nil, errors.New("invalid cache path")
	}

	out, err := fs.ReadFile(os.DirFS(dir), name)
	if err != nil {
		return out, fmt.Errorf("read file: %w", err)
	}

	return out, nil
}
