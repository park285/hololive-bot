package render

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicguard"
)

type calendarRenderCall struct {
	done    chan struct{}
	data    []byte
	err     error
	cancel  context.CancelFunc
	waiters int
}

func (r *CalendarCardRenderer) renderCoalesced(ctx context.Context, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) ([]byte, error) {
	call := r.acquireCalendarRenderCall(ctx, cacheKey, month, year, entries)
	return r.awaitCalendarRenderCall(ctx, cacheKey, call)
}

func (r *CalendarCardRenderer) acquireCalendarRenderCall(ctx context.Context, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) *calendarRenderCall {
	r.renderMu.Lock()
	defer r.renderMu.Unlock()
	call := r.inflight[cacheKey]
	if call == nil {
		call = r.startCalendarRenderCall(ctx, cacheKey, month, year, entries)
	}
	call.waiters++
	return call
}

func (r *CalendarCardRenderer) awaitCalendarRenderCall(ctx context.Context, cacheKey calendarCacheKey, call *calendarRenderCall) ([]byte, error) {
	select {
	case <-call.done:
		return completedCalendarRenderData(call)
	case <-ctx.Done():
		r.cancelCalendarRenderWaiter(cacheKey, call)
		return nil, ctx.Err()
	}
}

func completedCalendarRenderData(call *calendarRenderCall) ([]byte, error) {
	if call.err != nil {
		return nil, call.err
	}
	data := bytes.Clone(call.data)
	if data == nil {
		return nil, errors.New("await calendar render: missing image data")
	}
	return data, nil
}

func (r *CalendarCardRenderer) startCalendarRenderCall(ctx context.Context, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) *calendarRenderCall {
	if r.inflight == nil {
		r.inflight = make(map[calendarCacheKey]*calendarRenderCall)
	}
	workCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	call := &calendarRenderCall{done: make(chan struct{}), cancel: cancel}
	r.inflight[cacheKey] = call
	entrySnapshot := append([]domain.CalendarEntry(nil), entries...)
	panicguard.Go(slog.Default(), "calendar-render", func() {
		r.runCalendarRenderCall(workCtx, call, cacheKey, month, year, entrySnapshot)
	})
	return call
}

func (r *CalendarCardRenderer) runCalendarRenderCall(ctx context.Context, call *calendarRenderCall, cacheKey calendarCacheKey, month, year int, entries []domain.CalendarEntry) {
	var data []byte
	var err error
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("render calendar image: panic: %v", recovered)
			slog.Default().Error("Calendar render panicked", slog.Any("panic", recovered), slog.String("stack", string(debug.Stack())))
		}
		call.cancel()
		r.renderMu.Lock()
		if r.inflight[cacheKey] == call {
			delete(r.inflight, cacheKey)
		}
		call.data = bytes.Clone(data)
		call.err = err
		close(call.done)
		r.renderMu.Unlock()
	}()
	data, err = r.renderCalendarImageOnce(ctx, cacheKey, month, year, entries)
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
