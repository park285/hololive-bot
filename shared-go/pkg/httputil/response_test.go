package httputil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCheckStatus(t *testing.T) {
	t.Parallel()

	t.Run("2xx는 nil", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}
		if err := CheckStatus(resp); err != nil {
			t.Fatalf("CheckStatus() error = %v", err)
		}
	})

	t.Run("비2xx는 status/body를 포함", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("upstream failed")),
		}
		err := CheckStatus(resp)
		if err == nil {
			t.Fatal("CheckStatus() expected error")
		}
		if !strings.Contains(err.Error(), "status 502") {
			t.Fatalf("error = %q, expected status 502", err.Error())
		}
		if !strings.Contains(err.Error(), "upstream failed") {
			t.Fatalf("error = %q, expected body text", err.Error())
		}
	})

	t.Run("body read 실패는 wrap", func(t *testing.T) {
		t.Parallel()

		resp := &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       &errorReadCloser{err: fmt.Errorf("read fail")},
		}
		err := CheckStatus(resp)
		if err == nil {
			t.Fatal("CheckStatus() expected error")
		}
		if !strings.Contains(err.Error(), "read body") {
			t.Fatalf("error = %q, expected read body message", err.Error())
		}
	})
}

func TestCheckStatusReturnsTypedAPIError(t *testing.T) {
	t.Parallel()

	resp := &http.Response{
		StatusCode: http.StatusConflict,
		Body: io.NopCloser(strings.NewReader(`{
			"error":"notification_in_progress",
			"message":"notification is already running",
			"request_id":"req-123",
			"details":{"trigger":"weekly"}
		}`)),
	}

	err := CheckStatus(resp)
	if err == nil {
		t.Fatal("CheckStatus() expected error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("CheckStatus() error type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusConflict)
	}
	if apiErr.Code != "notification_in_progress" {
		t.Fatalf("Code = %q, want notification_in_progress", apiErr.Code)
	}
	if apiErr.Message != "notification is already running" {
		t.Fatalf("Message = %q, want notification is already running", apiErr.Message)
	}
	if apiErr.RequestID != "req-123" {
		t.Fatalf("RequestID = %q, want req-123", apiErr.RequestID)
	}
	if apiErr.Details["trigger"] != "weekly" {
		t.Fatalf("Details[trigger] = %v, want weekly", apiErr.Details["trigger"])
	}
}

func TestAPIErrorHelpersMatchWrappedErrors(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("wrapped: %w", &APIError{
		StatusCode: http.StatusNotFound,
		Code:       "no_subscribed_members",
		Message:    "no subscribed members",
	})

	if !IsStatus(err, http.StatusNotFound) {
		t.Fatal("IsStatus() = false, want true")
	}
	if IsStatus(err, http.StatusConflict) {
		t.Fatal("IsStatus() = true for wrong status")
	}
	if !IsCode(err, "no_subscribed_members") {
		t.Fatal("IsCode() = false, want true")
	}
	if IsCode(err, "notification_in_progress") {
		t.Fatal("IsCode() = true for wrong code")
	}

	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatal("AsAPIError() ok = false, want true")
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("AsAPIError().StatusCode = %d, want %d", apiErr.StatusCode, http.StatusNotFound)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	rc := &trackCloseReadCloser{Reader: strings.NewReader(`{"name":"test"}`)}
	resp := &http.Response{Body: rc}

	var out struct {
		Name string `json:"name"`
	}
	if err := DecodeJSON(resp, &out); err != nil {
		t.Fatalf("DecodeJSON() error = %v", err)
	}
	if out.Name != "test" {
		t.Fatalf("DecodeJSON() name = %q, want test", out.Name)
	}
	if !rc.closed {
		t.Fatal("DecodeJSON() expected body close")
	}
}

type errorReadCloser struct {
	err error
}

func (e *errorReadCloser) Read(_ []byte) (int, error) {
	return 0, e.err
}

func (e *errorReadCloser) Close() error {
	return nil
}

type trackCloseReadCloser struct {
	*strings.Reader
	closed bool
}

func (t *trackCloseReadCloser) Close() error {
	t.closed = true
	return nil
}
