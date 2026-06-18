// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package trigger

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
)

func TestClientSendWeeklyNotificationSuccess(t *testing.T) {
	t.Parallel()

	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL+"/", "", nil)
	if err := client.SendWeeklyNotification(t.Context()); err != nil {
		t.Fatalf("SendWeeklyNotification() error = %v", err)
	}

	if gotPath != triggercontracts.MajorEventWeeklyPath {
		t.Fatalf("path = %q, want %q", gotPath, triggercontracts.MajorEventWeeklyPath)
	}
}

func TestClientSendMonthlyNotificationConflict(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)

	err := client.SendMonthlyNotification(t.Context())
	if !errors.Is(err, triggercontracts.ErrNotificationInProgress) {
		t.Fatalf("SendMonthlyNotification() error = %v, want ErrNotificationInProgress", err)
	}
}

func TestClientSendMemberNewsWeeklyNon2xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)

		_, err := w.Write([]byte("upstream failed"))
		if err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)

	err := client.SendMemberNewsWeekly(t.Context())
	if err == nil {
		t.Fatal("SendMemberNewsWeekly() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %q, expected status 502", err.Error())
	}

	if !strings.Contains(err.Error(), "upstream failed") {
		t.Fatalf("error = %q, expected response body", err.Error())
	}
}

func TestClientSendMemberNewsWeeklySuccess(t *testing.T) {
	t.Parallel()

	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, "", nil)
	if err := client.SendMemberNewsWeekly(t.Context()); err != nil {
		t.Fatalf("SendMemberNewsWeekly() error = %v", err)
	}

	if gotPath != triggercontracts.MemberNewsWeeklyPath {
		t.Fatalf("path = %q, want %q", gotPath, triggercontracts.MemberNewsWeeklyPath)
	}
}

func TestClientSendWeeklyNotificationWithAPIKey(t *testing.T) {
	t.Parallel()

	const apiKey = "test-key"

	var gotHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(commoncontracts.APIKeyHeader)

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL, apiKey, nil)
	if err := client.SendWeeklyNotification(t.Context()); err != nil {
		t.Fatalf("SendWeeklyNotification() error = %v", err)
	}

	if gotHeader != apiKey {
		t.Fatalf("%s header = %q, want %q", commoncontracts.APIKeyHeader, gotHeader, apiKey)
	}
}
