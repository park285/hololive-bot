package render

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
)

type calendarRenderCall struct {
	done    chan struct{}
	pages   [][]byte
	err     error
	cancel  context.CancelFunc
	waiters int
}

type calendarPagePlan struct {
	kind            calendarPageKind
	isBday          bool
	themeID         string
	entries         []domain.CalendarEntry
	globalTruncated bool
}

func (r *CalendarCardRenderer) renderCoalesced(ctx context.Context, cacheKey calendarCacheKey, month, year int, plan calendarPagePlan) ([][]byte, error) {
	call := r.acquireCalendarRenderCall(ctx, cacheKey, month, year, plan)

	out, err := r.awaitCalendarRenderCall(ctx, cacheKey, call)
	if err != nil {
		return nil, fmt.Errorf("await calendar render call: %w", err)
	}

	return out, nil
}

func (r *CalendarCardRenderer) acquireCalendarRenderCall(ctx context.Context, cacheKey calendarCacheKey, month, year int, plan calendarPagePlan) *calendarRenderCall {
	r.renderMu.Lock()
	defer r.renderMu.Unlock()

	call := r.inflight[cacheKey]
	if call == nil {
		call = r.startCalendarRenderCall(ctx, cacheKey, month, year, plan)
	}

	call.waiters++

	return call
}

func (r *CalendarCardRenderer) awaitCalendarRenderCall(ctx context.Context, cacheKey calendarCacheKey, call *calendarRenderCall) ([][]byte, error) {
	select {
	case <-call.done:
		out, err := completedCalendarRenderPages(call)
		if err != nil {
			return nil, fmt.Errorf("completed calendar render pages: %w", err)
		}

		return out, nil
	case <-ctx.Done():
		r.cancelCalendarRenderWaiter(cacheKey, call)

		return nil, fmt.Errorf("await calendar render: %w", ctx.Err())
	}
}

func completedCalendarRenderPages(call *calendarRenderCall) ([][]byte, error) {
	if call.err != nil {
		return nil, call.err
	}

	if len(call.pages) == 0 {
		return nil, errors.New("await calendar render: missing image pages")
	}

	return cloneSchedulePages(call.pages), nil
}

func (r *CalendarCardRenderer) startCalendarRenderCall(ctx context.Context, cacheKey calendarCacheKey, month, year int, plan calendarPagePlan) *calendarRenderCall {
	if r.inflight == nil {
		r.inflight = make(map[calendarCacheKey]*calendarRenderCall)
	}

	workCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	call := &calendarRenderCall{done: make(chan struct{}), cancel: cancel}

	r.inflight[cacheKey] = call

	planSnapshot := plan

	planSnapshot.entries = append([]domain.CalendarEntry(nil), plan.entries...)

	go panicguard.Run(slog.Default(), panicscope.BackgroundTask, "calendar-render", func() {
		r.runCalendarRenderCall(workCtx, call, cacheKey, month, year, planSnapshot)
	})

	return call
}

func (r *CalendarCardRenderer) runCalendarRenderCall(ctx context.Context, call *calendarRenderCall, cacheKey calendarCacheKey, month, year int, plan calendarPagePlan) {
	var (
		pages [][]byte
		err   error
	)

	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("render calendar album: panic type: %T", recovered)
			slog.Default().Error("Calendar render panicked", slog.Group("panic", slog.String("type", fmt.Sprintf("%T", recovered))), slog.String("stack", string(debug.Stack())))
		}

		call.cancel()
		r.renderMu.Lock()

		if r.inflight[cacheKey] == call {
			delete(r.inflight, cacheKey)
		}

		call.pages = cloneSchedulePages(pages)
		call.err = err
		close(call.done)
		r.renderMu.Unlock()
	}()

	pages, err = r.renderCalendarPageOnce(ctx, cacheKey, month, year, plan)
}

func (r *CalendarCardRenderer) cancelCalendarRenderWaiter(cacheKey calendarCacheKey, call *calendarRenderCall) {
	r.renderMu.Lock()
	defer r.renderMu.Unlock()

	if r.inflight[cacheKey] != call {
		return
	}

	call.waiters--
	if call.waiters == 0 {
		delete(r.inflight, cacheKey)
		call.cancel()
	}
}
