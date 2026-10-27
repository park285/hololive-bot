package api

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

// alarmSettingsWorker는 worker 알림 HTTP API의 PUT settings route만 흉내 낸다. 응답은 worker handler와 같은
// {"success":..,"error":..,"message":..,"data":{"target_minutes":[..]}} envelope이다. 실제 worker handler와
// alarmhttp client의 왕복은 alarm-worker httpapi·subscriptions 테스트가 검증하고, 여기서는 API의 설정 저장·적용 순서만 본다.
type alarmSettingsWorker struct {
	mu      sync.Mutex
	targets []int
}

type alarmSettingsWorkerEnvelope struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

func newAlarmSettingsWorker(t *testing.T, targets []int) (http.Handler, func() []int) {
	t.Helper()

	worker := &alarmSettingsWorker{targets: slices.Clone(targets)}
	mux := http.NewServeMux()
	mux.HandleFunc(http.MethodPut+" "+contractsalarm.SettingsPath, func(w http.ResponseWriter, r *http.Request) {
		worker.updateAdvanceMinutes(t, w, r)
	})

	return mux, worker.targetMinutes
}

func (w *alarmSettingsWorker) updateAdvanceMinutes(t *testing.T, rw http.ResponseWriter, r *http.Request) {
	t.Helper()

	var req struct {
		Minutes int `json:"minutes"`
	}

	if err := jsonv2.UnmarshalRead(r.Body, &req); err != nil || req.Minutes < 1 {
		writeAlarmSettingsWorkerResponse(t, rw, http.StatusBadRequest, alarmSettingsWorkerEnvelope{Error: "invalid_request_body", Message: "invalid request body"})

		return
	}

	if r.Context().Err() != nil {
		writeAlarmSettingsWorkerResponse(t, rw, http.StatusInternalServerError, alarmSettingsWorkerEnvelope{Error: "alarm_advance_update_failed", Message: "alarm advance update failed"})

		return
	}

	policy := targetpolicy.NewTargetMinutePolicyFromRuntimeAdvance(req.Minutes)

	w.mu.Lock()

	w.targets = policy.Clone()
	w.mu.Unlock()

	writeAlarmSettingsWorkerResponse(t, rw, http.StatusOK, alarmSettingsWorkerEnvelope{Success: true, Data: map[string][]int{"target_minutes": policy.Clone()}})
}

func writeAlarmSettingsWorkerResponse(t *testing.T, w http.ResponseWriter, status int, body alarmSettingsWorkerEnvelope) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	if err := jsonv2.MarshalWrite(w, body); err != nil {
		t.Errorf("write alarm settings worker response: %v", err)
	}
}

func (w *alarmSettingsWorker) targetMinutes() []int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.targets)
}
