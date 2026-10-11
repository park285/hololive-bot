package trigger

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

func TestClientPreservesEnqueueResultsWithoutRetry(t *testing.T) {
	for _, method := range []struct {
		name string
		send func(*Client, context.Context) (triggercontracts.Result, error)
	}{
		{"weekly", (*Client).SendWeeklyNotification},
		{"monthly", (*Client).SendMonthlyNotification},
		{"membernews", (*Client).SendMemberNewsWeekly},
	} {
		for _, tc := range []struct {
			name string
			body string
			want triggercontracts.Result
		}{
			{"queued", `{"status":"queued","queued":2,"skipped":0,"failed":0}`, triggercontracts.NewResult(2, 0, 0)},
			{"skipped", `{"status":"skipped","queued":0,"skipped":3,"failed":0}`, triggercontracts.NewResult(0, 3, 0)},
			{"partial", `{"status":"partial","queued":2,"skipped":1,"failed":1}`, triggercontracts.NewResult(2, 1, 1)},
		} {
			t.Run(method.name+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32

				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)

					if _, err := io.WriteString(w, tc.body); err != nil {
						t.Error(err)
					}
				}))
				t.Cleanup(srv.Close)

				client := newTestClient(t, srv.URL, "")
				got, err := method.send(client, t.Context())

				if err != nil || got != tc.want || calls.Load() != 1 {
					t.Fatalf("result=%+v error=%v requests=%d; want %+v and one request", got, err, calls.Load(), tc.want)
				}
			})
		}
	}
}

func TestClientRejectsMissingOrContradictoryEnqueueResult(t *testing.T) {
	for _, body := range []string{
		"", `{}`, `{"status":"weekly notification sent"}`,
		`{"status":"queued","queued":0,"skipped":0,"failed":1}`,
		`{"status":"partial","queued":1,"skipped":0,"failed":0}`,
		`{"status":"skipped","queued":0,"skipped":-1,"failed":0}`,
	} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if _, err := io.WriteString(w, body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(srv.Close)

			if _, err := newTestClient(t, srv.URL, "").SendWeeklyNotification(t.Context()); err == nil {
				t.Fatal("invalid response must not become a successful enqueue result")
			}
		})
	}
}
