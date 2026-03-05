package httputil

import (
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
