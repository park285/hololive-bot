package api

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/alarmtestkit"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

type orderedSettingsApplier struct {
	testSettingsApplier

	mu      sync.Mutex
	targets []int
	entered chan struct{}
	release chan struct{}
}

func (a *orderedSettingsApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) (sharedsettings.AlarmAdvanceMinutesApplyResult, error) {
	if minutes == 7 {
		close(a.entered)

		select {
		case <-a.release:

		case <-ctx.Done():
			return sharedsettings.AlarmAdvanceMinutesApplyResult{AlarmRequestedAdvanceMinutes: minutes}, ctx.Err()
		}
	}

	targets := targetpolicy.BuildRuntimeTargetMinutes(minutes)

	a.mu.Lock()

	a.targets = targets

	a.mu.Unlock()

	return sharedsettings.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes, AlarmApplied: true, AlarmTargetMinutes: slices.Clone(targets),
	}, nil
}

func (a *orderedSettingsApplier) SettingsRuntimeState() sharedsettings.SettingsRuntimeStateResult {
	a.mu.Lock()

	defer a.mu.Unlock()

	return sharedsettings.SettingsRuntimeStateResult{AlarmTargetMinutes: slices.Clone(a.targets)}
}

func newOrderedSettingsRequest(ctx context.Context, body string) *http.Request {
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/holo/settings", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	return request
}

func settingsRequestDone(h *SettingsAPIHandler, request *http.Request) (<-chan struct{}, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		defer close(done)

		c, _ := gin.CreateTestContext(rec)

		c.Request = request
		h.UpdateSettings(c)
	}()

	return done, rec
}

func requireSettingsRequestWaiting(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
		t.Error("settings request completed before the current operation finished")

	default:
	}
}

func TestSettingsAPIHandlerSerializesSaveApplyAndGetSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())

		applier := &orderedSettingsApplier{targets: []int{5, 3, 1}, entered: make(chan struct{}), release: make(chan struct{})}

		release := sync.OnceFunc(func() { close(applier.release) })

		defer release()

		h := &SettingsAPIHandler{Handler: &Handler{logger: newDiscardLogger(), activity: newActivityLoggerForTest(t), settings: store, settingsApplier: applier}}

		firstDone, firstRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":7}`))

		<-applier.entered

		secondDone, secondRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":10}`))

		getCtx, getRec := newAPITestContext(http.MethodGet, "/api/holo/settings", nil)

		getDone := make(chan struct{})

		go func() {
			defer close(getDone)

			h.GetSettings(getCtx)
		}()

		synctest.Wait()

		requireSettingsRequestWaiting(t, secondDone)

		requireSettingsRequestWaiting(t, getDone)

		assert.Equal(t, 7, store.Get().AlarmAdvanceMinutes)

		release()

		<-firstDone

		<-secondDone

		<-getDone

		assert.Equal(t, true, decodeUpdateSettingsRuntime(t, firstRec)["alarm_applied"])

		assert.Equal(t, true, decodeUpdateSettingsRuntime(t, secondRec)["alarm_applied"])

		assertCoherentSettingsResponse(t, getRec)

		assert.Equal(t, 10, store.Get().AlarmAdvanceMinutes)

		assert.Equal(t, []int{10, 3, 1}, applier.SettingsRuntimeState().AlarmTargetMinutes)
	})
}

func assertCoherentSettingsResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	var response settingsResponse

	require.Equal(t, http.StatusOK, rec.Code)

	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &response))

	targets, ok := response.Runtime["alarm_target_minutes"].([]any)

	require.True(t, ok)

	require.NotEmpty(t, targets)

	assert.InDelta(t, float64(response.Settings.AlarmAdvanceMinutes), targets[0], 0)
}

type gatedSettingsReadWriter struct {
	settingssvc.ReadWriter

	readOnce atomic.Bool
	entered  chan struct{}
	release  chan struct{}
	updates  atomic.Int32
}

func (w *gatedSettingsReadWriter) Get() settingssvc.Settings {
	current := w.ReadWriter.Get()

	if w.readOnce.CompareAndSwap(true, false) {
		close(w.entered)

		<-w.release
	}

	return current
}

func (w *gatedSettingsReadWriter) Update(next settingssvc.Settings) error {
	w.updates.Add(1)

	if err := w.ReadWriter.Update(next); err != nil {
		return fmt.Errorf("update settings: %w", err)
	}

	return nil
}

func TestSettingsAPIHandlerEmptyRequestDoesNotOverwriteConcurrentUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		writer := &gatedSettingsReadWriter{
			ReadWriter: mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger()),
			entered:    make(chan struct{}), release: make(chan struct{}),
		}

		writer.readOnce.Store(true)

		release := sync.OnceFunc(func() { close(writer.release) })

		defer release()

		h := &SettingsAPIHandler{Handler: &Handler{logger: newDiscardLogger(), activity: newActivityLoggerForTest(t), settings: writer, settingsApplier: testSettingsApplier{}}}

		emptyDone, emptyRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{}`))

		<-writer.entered

		updateDone, updateRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":10}`))

		synctest.Wait()

		requireSettingsRequestWaiting(t, updateDone)

		release()

		<-emptyDone

		<-updateDone

		assert.Empty(t, decodeUpdateSettingsRuntime(t, emptyRec))

		assert.Equal(t, true, decodeUpdateSettingsRuntime(t, updateRec)["alarm_applied"])

		assert.Equal(t, int32(1), writer.updates.Load())

		assert.Equal(t, 10, writer.Get().AlarmAdvanceMinutes)
	})
}

func TestSettingsAPIHandlerCanceledQueuedGetAndUpdateDoNotReadOrWrite(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		writer := &countingSettingsWriter{ReadWriter: mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())}

		applier := &orderedSettingsApplier{targets: []int{5, 3, 1}, entered: make(chan struct{}), release: make(chan struct{})}

		release := sync.OnceFunc(func() { close(applier.release) })

		defer release()

		h := &SettingsAPIHandler{Handler: &Handler{logger: newDiscardLogger(), activity: newActivityLoggerForTest(t), settings: writer, settingsApplier: applier}}

		firstDone, _ := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":7}`))

		<-applier.entered

		ctx, cancel := context.WithCancel(t.Context())

		defer cancel()

		updateDone, updateRec := settingsRequestDone(h, newOrderedSettingsRequest(ctx, `{"alarmAdvanceMinutes":10}`))

		getCtx, getRec := newAPITestContext(http.MethodGet, "/api/holo/settings", nil)

		getCtx.Request = getCtx.Request.WithContext(ctx)

		getDone := make(chan struct{})

		go func() {
			defer close(getDone)

			h.GetSettings(getCtx)
		}()

		synctest.Wait()

		cancel()

		synctest.Wait()

		<-getDone

		<-updateDone

		assertErrorResponse(t, getRec, http.StatusInternalServerError, "Failed to get settings")

		assertErrorResponse(t, updateRec, http.StatusInternalServerError, "Failed to update settings")

		assert.Equal(t, int32(1), writer.updates.Load())

		release()

		<-firstDone
	})
}

func TestSettingsAPIHandlerSaveApplyOrderingWithActualWorker(t *testing.T) {
	worker, targets, err := alarmtestkit.NewWorker([]int{5, 3, 1})

	require.NoError(t, err)

	server := httptest.NewServer(worker)

	t.Cleanup(server.Close)

	store := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())

	writer := &savedSettingsGate{ReadWriter: store, entered: make(chan struct{}), release: make(chan struct{})}

	release := sync.OnceFunc(func() { close(writer.release) })

	defer release()

	h := &SettingsAPIHandler{Handler: &Handler{logger: newDiscardLogger(), activity: newActivityLoggerForTest(t), settings: writer, settingsApplier: sharedsettings.NewLocalSettingsApplier(alarm.NewClient(server.URL, newDiscardLogger()))}}

	firstDone, firstRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":7}`))

	<-writer.entered

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)

	defer cancel()

	queuedDone, queuedRec := settingsRequestDone(h, newOrderedSettingsRequest(ctx, `{"alarmAdvanceMinutes":10}`))

	<-queuedDone

	assertErrorResponse(t, queuedRec, http.StatusInternalServerError, "Failed to update settings")

	assert.Equal(t, 7, store.Get().AlarmAdvanceMinutes)

	assert.Equal(t, []int{5, 3, 1}, targets())

	release()

	<-firstDone

	assert.Equal(t, true, decodeUpdateSettingsRuntime(t, firstRec)["alarm_applied"])

	lastDone, lastRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":10}`))

	<-lastDone

	assert.Equal(t, true, decodeUpdateSettingsRuntime(t, lastRec)["alarm_applied"])

	assert.Equal(t, 10, store.Get().AlarmAdvanceMinutes)

	assert.Equal(t, []int{10, 3, 1}, targets())
}

type savedSettingsGate struct {
	settingssvc.ReadWriter

	entered chan struct{}
	release chan struct{}
}

func (w *savedSettingsGate) Update(next settingssvc.Settings) error {
	if err := w.ReadWriter.Update(next); err != nil {
		return fmt.Errorf("update settings: %w", err)
	}

	if next.AlarmAdvanceMinutes == 7 {
		close(w.entered)

		<-w.release
	}

	return nil
}

type remainingBudgetApplier struct {
	testSettingsApplier

	remaining chan time.Duration
}

func (a remainingBudgetApplier) ApplyAlarmAdvanceMinutes(ctx context.Context, minutes int) (sharedsettings.AlarmAdvanceMinutesApplyResult, error) {
	if minutes != 10 {
		return a.testSettingsApplier.ApplyAlarmAdvanceMinutes(ctx, minutes)
	}

	deadline, _ := ctx.Deadline()

	a.remaining <- time.Until(deadline)

	<-ctx.Done()

	return sharedsettings.AlarmAdvanceMinutesApplyResult{
		AlarmRequestedAdvanceMinutes: minutes,
		AlarmReason:                  "alarm worker outcome_unknown: request deadline",
	}, ctx.Err()
}

func TestSettingsAPIHandlerOperationBudgetIncludesCoordinatorWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger())

		writer := &savedSettingsGate{ReadWriter: store, entered: make(chan struct{}), release: make(chan struct{})}

		release := sync.OnceFunc(func() { close(writer.release) })

		defer release()

		applier := remainingBudgetApplier{remaining: make(chan time.Duration, 1)}

		h := &SettingsAPIHandler{Handler: &Handler{logger: newDiscardLogger(), activity: newActivityLoggerForTest(t), settings: writer, settingsApplier: applier}}

		firstDone, _ := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":7}`))

		<-writer.entered

		started := time.Now()

		secondDone, secondRec := settingsRequestDone(h, newOrderedSettingsRequest(t.Context(), `{"alarmAdvanceMinutes":10}`))

		synctest.Wait()

		time.Sleep(3 * time.Second)

		release()

		<-firstDone

		synctest.Wait()

		remaining := <-applier.remaining

		assert.Equal(t, constants.RequestTimeout.AdminRequest-3*time.Second, remaining)

		time.Sleep(remaining)

		synctest.Wait()

		<-secondDone

		assert.Equal(t, constants.RequestTimeout.AdminRequest, time.Since(started))

		runtime := decodeUpdateSettingsRuntime(t, secondRec)

		assert.Equal(t, false, runtime["alarm_applied"])

		assert.NotContains(t, runtime, "alarm_target_minutes")

		assert.Equal(t, 10, store.Get().AlarmAdvanceMinutes, "deadline after persistence must not roll back the file")
	})
}

func TestSettingsAPIHandlerCanceledDuringReadDoesNotReturnPastSnapshotOrPersist(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				writer := &gatedSettingsReadWriter{
					ReadWriter: mustNewTestSettingsService(t, filepath.Join(t.TempDir(), "settings.json"), settingssvc.Settings{AlarmAdvanceMinutes: 5}, newDiscardLogger()),
					entered:    make(chan struct{}), release: make(chan struct{}),
				}

				writer.readOnce.Store(true)

				release := sync.OnceFunc(func() { close(writer.release) })

				defer release()

				h := &SettingsAPIHandler{Handler: &Handler{logger: newDiscardLogger(), activity: newActivityLoggerForTest(t), settings: writer, settingsApplier: testSettingsApplier{}}}

				ctx, cancel := context.WithCancel(t.Context())

				defer cancel()

				request, rec := newAPITestContext(method, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":10}`))

				request.Request = request.Request.WithContext(ctx)

				done := make(chan struct{})

				go func() {
					defer close(done)

					if method == http.MethodGet {
						h.GetSettings(request)
					} else {
						h.UpdateSettings(request)
					}
				}()

				<-writer.entered

				cancel()

				release()

				<-done

				assert.Equal(t, http.StatusInternalServerError, rec.Code)

				assert.NotContains(t, decodeSettingsResponse(t, rec), "settings")

				assert.Zero(t, writer.updates.Load())

				assert.Equal(t, 5, writer.Get().AlarmAdvanceMinutes)
			})
		})
	}
}
