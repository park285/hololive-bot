package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/admin-dashboard/internal/httpx"
)

// InFlight는 종료 시점의 처리 중 요청이며 requestId는 업무 receipt가 아닙니다.
type InFlight struct {
	RequestID string    `json:"requestId"`
	Operation string    `json:"operation"`
	Mutation  bool      `json:"mutation"`
	StartedAt time.Time `json:"startedAt"`
	Outcome   string    `json:"outcome"`
}

type admittedRequest struct {
	info     InFlight
	dispatch contract.Dispatch
}

type admission struct {
	mu      sync.Mutex
	closed  bool
	active  map[string]*admittedRequest
	drained chan struct{}
}

func (a *admission) enter(info InFlight) (*admittedRequest, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return nil, false
	}

	if len(a.active) == 0 {
		a.active = make(map[string]*admittedRequest)
		a.drained = make(chan struct{})
	}

	request := &admittedRequest{info: info}

	a.active[info.RequestID] = request

	return request, true
}

func (a *admission) leave(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.active, id)

	if len(a.active) == 0 {
		close(a.drained)
	}
}

func (r *API) admit(operation string, mutation bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		request, ok := r.admission.enter(InFlight{RequestID: httpx.RequestID(c), Operation: operation, Mutation: mutation, StartedAt: time.Now().UTC()})
		if !ok {
			httpx.Abort(c, &contract.AppError{Status: http.StatusServiceUnavailable, Body: contract.ErrorResponse{Code: "ADMISSION_CLOSED", Error: "Administrator service is stopping"}})

			return
		}

		defer r.admission.leave(request.info.RequestID)

		c.Request = c.Request.WithContext(contract.WithDispatch(c.Request.Context(), &request.dispatch))
		c.Set("admin-dispatch", &request.dispatch)
		c.Next()
	}
}

// BeginDrain는 새 HTTP 진입을 막고 현재 요청을 분류합니다. 전송 시도는 outcome_unknown입니다.
func (r *API) BeginDrain() []InFlight {
	r.admission.mu.Lock()
	defer r.admission.mu.Unlock()

	r.admission.closed = true

	out := make([]InFlight, 0, len(r.admission.active))

	for _, request := range r.admission.active {
		info := request.info

		info.Outcome = "in_progress"

		if info.Mutation {
			info.Outcome = "not_dispatched"

			if request.dispatch.Attempted() {
				info.Outcome = "outcome_unknown"
			}
		}

		out = append(out, info)
	}

	slices.SortFunc(out, func(a, b InFlight) int { return strings.Compare(a.RequestID, b.RequestID) })

	return out
}

// WaitDrained는 허용된 HTTP handler 종료를 기다리며 upstream 효과 완료를 주장하지 않습니다.
func (r *API) WaitDrained(ctx context.Context) error {
	r.admission.mu.Lock()

	if len(r.admission.active) == 0 {
		r.admission.mu.Unlock()

		return nil
	}

	done := r.admission.drained
	r.admission.mu.Unlock()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for administrator requests: %w", ctx.Err())
	}
}

// CloseStreams는 새 WS 연결을 차단하고 연결·감시·구독을 회수합니다.
func (r *API) CloseStreams() { r.streams.Close() }
