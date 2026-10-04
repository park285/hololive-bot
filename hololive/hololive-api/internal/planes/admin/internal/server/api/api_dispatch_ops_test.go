package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/dispatchops"
)

type dispatchOpsStub struct {
	calls    int
	err      error
	filter   dispatchops.Filter
	request  dispatchops.RequeueRequest
	deadline bool
}

func (s *dispatchOpsStub) record(ctx context.Context) { s.calls++; _, s.deadline = ctx.Deadline() }
func (s *dispatchOpsStub) Summary(ctx context.Context) (dispatchops.Summary, error) {
	s.record(ctx)

	return dispatchops.Summary{Counts: []dispatchops.StatusCount{}}, s.err
}

func (s *dispatchOpsStub) Failures(ctx context.Context) (dispatchops.FailureBreakdown, error) {
	s.record(ctx)

	return dispatchops.FailureBreakdown{
		Total: "0", ErrorCodes: []dispatchops.FailureCount{}, AlarmTypes: []dispatchops.FailureCount{},
		Channels: []dispatchops.FailureCount{}, Rooms: []dispatchops.FailureCount{},
	}, s.err
}

func (s *dispatchOpsStub) List(ctx context.Context, f dispatchops.Filter) (dispatchops.Page, error) {
	s.record(ctx)

	s.filter = f

	return dispatchops.Page{Items: []dispatchops.Delivery{}}, s.err
}

func (s *dispatchOpsStub) Detail(ctx context.Context, _ string) (dispatchops.Detail, error) {
	s.record(ctx)

	return dispatchops.Detail{ReplayTargets: []dispatchops.Revision{}, Group: []dispatchops.Delivery{}}, s.err
}

func (s *dispatchOpsStub) Actions(ctx context.Context, _, _ string) (dispatchops.ActionPage, error) {
	s.record(ctx)

	return dispatchops.ActionPage{Items: []dispatchops.Action{}}, s.err
}

func (s *dispatchOpsStub) Requeue(ctx context.Context, _ string, r dispatchops.RequeueRequest) (dispatchops.RequeueResult, error) {
	s.record(ctx)

	s.request = r

	return dispatchops.RequeueResult{IDs: []string{"9007199254740993"}}, s.err
}

func dispatchTestRouter(ops DispatchOperations) *gin.Engine {
	h := (&Handler{}).DomainHandlers().Alarm
	h.SetDispatchOperations(ops)

	router := gin.New()
	router.GET("/dispatch/summary", h.GetDispatchSummary)
	router.GET("/dispatch/failures", h.GetDispatchFailures)
	router.GET("/dispatch/deliveries", h.GetDispatchDeliveries)
	router.GET("/dispatch/deliveries/:id", h.GetDispatchDelivery)
	router.GET("/dispatch/deliveries/:id/actions", h.GetDispatchActions)
	router.POST("/dispatch/deliveries/:id/requeue", h.RequeueDispatchDelivery)
	router.POST("/dispatch/deliveries/:id/settle", h.SettleDispatchDelivery)

	return router
}

const (
	dispatchTestBody        = `{"operatorId":"operator-1","reason":"verified repair","duplicateRiskAck":true,"targets":[{"id":"9007199254740993","updatedAt":"2026-09-18T01:02:03.456789Z"}]}`
	dispatchTestContentType = "application/json"
	dispatchTestReplayPath  = "/dispatch/deliveries/9007199254740993/requeue"
)

func dispatchRequest(t *testing.T, router *gin.Engine, method, path, body, contentType string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))

	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	return response
}

func TestDispatchHandlerRequeueValidation(t *testing.T) {
	tests := []struct {
		name, path, body, contentType string
		status                        int
	}{
		{"valid", dispatchTestReplayPath, dispatchTestBody, dispatchTestContentType, 200},
		{"charset", dispatchTestReplayPath, dispatchTestBody, "application/json; charset=utf-8", 200},
		{"unsupported_media", dispatchTestReplayPath, dispatchTestBody, "text/plain", 415},
		{"empty", dispatchTestReplayPath, "", dispatchTestContentType, 400},
		{"null", dispatchTestReplayPath, "null", dispatchTestContentType, 400},
		{"no_ack", dispatchTestReplayPath, strings.Replace(dispatchTestBody, "true", "false", 1), dispatchTestContentType, 400},
		{"blank_reason", dispatchTestReplayPath, strings.Replace(dispatchTestBody, "verified repair", "   ", 1), dispatchTestContentType, 400},
		{"unknown_member", dispatchTestReplayPath, strings.Replace(dispatchTestBody, "{", "{\"unexpected\":true,", 1), dispatchTestContentType, 400},
		{"duplicate_member", dispatchTestReplayPath, strings.Replace(dispatchTestBody, "{", "{\"duplicateRiskAck\":true,", 1), dispatchTestContentType, 400},
		{"trailing_document", dispatchTestReplayPath, dispatchTestBody + "{}", dispatchTestContentType, 400},
		{"wrong_path_id", "/dispatch/deliveries/01/requeue", dispatchTestBody, dispatchTestContentType, 400},
		{"unexpected_query", dispatchTestReplayPath + "?force=true", dispatchTestBody, dispatchTestContentType, 400},
		{"body_limit", dispatchTestReplayPath, strings.Replace(dispatchTestBody, "verified repair", strings.Repeat("a", dispatchOpsMaxBody), 1), dispatchTestContentType, 413},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := &dispatchOpsStub{}
			response := dispatchRequest(t, dispatchTestRouter(stub), "POST", test.path, test.body, test.contentType)

			if response.Code != test.status {
				t.Fatalf("got %d want %d: %s", response.Code, test.status, response.Body.String())
			}

			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("missing no-store")
			}

			if test.status == 200 {
				if stub.calls != 1 || !stub.deadline || stub.request.Targets[0].ID != "9007199254740993" {
					t.Fatalf("dispatch: %+v", stub)
				}

				if !strings.Contains(response.Body.String(), `"9007199254740993"`) {
					t.Fatal("lost string ID")
				}
			} else if stub.calls != 0 {
				t.Fatal("invalid request reached repository")
			}
		})
	}
}

func TestDispatchHandlerQueryValidation(t *testing.T) {
	for _, path := range []string{
		"/dispatch/deliveries?status=DLQ", "/dispatch/deliveries?status=dlq&status=dlq",
		"/dispatch/deliveries?status=", "/dispatch/deliveries?beforeId=01", "/dispatch/deliveries?limit=99999",
		"/dispatch/deliveries?roomId=a%0Ab", "/dispatch/deliveries?status=dlq;status=sent",
		"/dispatch/summary?status=dlq", "/dispatch/deliveries/01", "/dispatch/deliveries/1?beforeId=2",
		"/dispatch/deliveries/1/actions?beforeId=0", "/dispatch/deliveries/1/actions?beforeId=1&beforeId=2",
		"/dispatch/deliveries?errorCode=Timeout", "/dispatch/deliveries?errorCode=",
		"/dispatch/deliveries?errorCode=timeout&errorCode=timeout",
		"/dispatch/deliveries?alarmType=%20live", "/dispatch/deliveries?alarmType=",
		"/dispatch/deliveries?alarmType=" + strings.Repeat("a", 65),
		"/dispatch/deliveries?alarmType=live&alarmType=live", "/dispatch/failures?x=1",
	} {
		t.Run(path, func(t *testing.T) {
			stub := &dispatchOpsStub{}
			response := dispatchRequest(t, dispatchTestRouter(stub), "GET", path, "", "")

			if response.Code != 400 || stub.calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, stub.calls, response.Body.String())
			}
		})
	}

	stub := &dispatchOpsStub{}
	response := dispatchRequest(t, dispatchTestRouter(stub), "GET", "/dispatch/deliveries?status=dlq&roomId=9007199254740993&beforeId=9223372036854775807&alarmType=LIVE&errorCode=unclassified", "", "")

	if response.Code != 200 || stub.filter.RoomID != "9007199254740993" || stub.filter.BeforeID != "9223372036854775807" || stub.filter.AlarmType != "LIVE" || stub.filter.ErrorCode != "unclassified" || !stub.deadline {
		t.Fatalf("response=%s stub=%+v", response.Body.String(), stub)
	}
}

func TestDispatchHandlerErrorsAreSanitized(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{dispatchops.ErrInvalidInput, 400},
		{dispatchops.ErrNotFound, 404},
		{dispatchops.ErrConflict, 409},
		{dispatchops.ErrUnavailable, 503},
		{errors.New("password=secret; SELECT private_payload"), 500},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			stub := &dispatchOpsStub{err: test.err}
			response := dispatchRequest(t, dispatchTestRouter(stub), "POST", dispatchTestReplayPath, dispatchTestBody, dispatchTestContentType)

			if response.Code != test.status {
				t.Fatalf("got %d want %d", response.Code, test.status)
			}

			if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "SELECT") {
				t.Fatal("database detail leaked")
			}

			if stub.calls != 1 {
				t.Fatal("mutation retried")
			}
		})
	}
}

func TestDispatchHandlerMissingDependencyFailsClosed(t *testing.T) {
	for _, path := range []string{"/dispatch/summary", "/dispatch/failures", "/dispatch/deliveries", "/dispatch/deliveries/1", "/dispatch/deliveries/1/actions"} {
		response := dispatchRequest(t, dispatchTestRouter(nil), "GET", path, "", "")
		if response.Code != 503 {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}

	response := dispatchRequest(t, dispatchTestRouter(nil), "POST", dispatchTestReplayPath, dispatchTestBody, dispatchTestContentType)
	if response.Code != 503 {
		t.Fatalf("mutation: %d", response.Code)
	}
}

func TestDispatchFailuresHTTP(t *testing.T) {
	stub := &dispatchOpsStub{}
	response := dispatchRequest(t, dispatchTestRouter(stub), "GET", "/dispatch/failures", "", "")

	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || !stub.deadline {
		t.Fatalf("status=%d headers=%v deadline=%v", response.Code, response.Header(), stub.deadline)
	}

	for _, field := range []string{`"total":"0"`, `"errorCodes":[]`, `"alarmTypes":[]`, `"channels":[]`, `"rooms":[]`} {
		if !strings.Contains(response.Body.String(), field) {
			t.Fatalf("missing %s: %s", field, response.Body.String())
		}
	}

	stub.err = errors.New("SELECT private_payload")
	response = dispatchRequest(t, dispatchTestRouter(stub), "GET", "/dispatch/failures", "", "")

	if response.Code != 500 || strings.Contains(response.Body.String(), "private_payload") {
		t.Fatalf("unsanitized failure: %d %s", response.Code, response.Body.String())
	}
}

func (s *dispatchOpsStub) Settle(ctx context.Context, _ string, _ dispatchops.SettleRequest) (dispatchops.RequeueResult, error) {
	s.record(ctx)

	return dispatchops.RequeueResult{IDs: []string{"9007199254740993"}}, s.err
}

func TestDispatchHandlerSettlementValidation(t *testing.T) {
	body := `{"operatorId":"operator-1","reason":"발송 보류","action":"cancel","targets":[{"id":"9007199254740993","updatedAt":"2026-09-18T01:02:03.456789Z"}]}`
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"cancel", body, 200},
		{"quarantine", strings.Replace(body, "cancel", "quarantine", 1), 200},
		{"unknown_action", strings.Replace(body, "cancel", "retry", 1), 400},
		{"missing_reason", strings.Replace(body, "발송 보류", "", 1), 400},
		{"unknown_field", strings.Replace(body, `"action":`, `"duplicateRiskAck":true,"action":`, 1), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &dispatchOpsStub{}
			response := dispatchRequest(t, dispatchTestRouter(stub), http.MethodPost, "/dispatch/deliveries/9007199254740993/settle", tc.body, dispatchTestContentType)

			if response.Code != tc.status {
				t.Fatalf("status %d: %s", response.Code, response.Body.String())
			}

			if tc.status == 200 && (stub.calls != 1 || !stub.deadline) {
				t.Fatal("missing single bounded call")
			}

			if tc.status != 200 && stub.calls != 0 {
				t.Fatal("invalid input dispatched")
			}
		})
	}
}
