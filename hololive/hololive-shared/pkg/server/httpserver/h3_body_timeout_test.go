package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestRuntimeH3BodyReadTimeoutPreservesLongResponse(t *testing.T) {
	certFile, keyFile := writeH3LocalhostCertificate(t)

	for _, slowBody := range []bool{true, false} {
		name := "complete-body-long-response"

		if slowBody {
			name = "missing-body-fin"
		}

		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) { assertRuntimeBodyTimeout(t, certFile, keyFile, slowBody) })
		})
	}
}

func serveLongBodyTimeoutResponse(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	_, err := io.ReadAll(r.Body)
	if errors.Is(err, context.DeadlineExceeded) {
		w.WriteHeader(http.StatusRequestTimeout)

		return
	}

	if err != nil {
		t.Errorf("read body: %v", err)

		return
	}

	// 본문을 다 읽은 뒤의 긴 응답은 본문 예산과 독립적이어야 한다.
	time.Sleep(time.Minute)

	if contextErr := r.Context().Err(); contextErr != nil {
		t.Errorf("response context canceled: %v", contextErr)
	}

	w.WriteHeader(http.StatusOK)

	if _, writeErr := io.WriteString(w, "data: complete\n\n"); writeErr != nil {
		t.Error(writeErr)
	}
}

func assertRuntimeBodyTimeout(t *testing.T, certFile, keyFile string, slowBody bool) {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { serveLongBodyTimeoutResponse(t, w, r) })

	server, err := newH3Server(testLoopbackAddr, handler, certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}

	var body io.Reader = strings.NewReader("complete")

	reader, writer := io.Pipe()

	defer closeRuntimeBodyResource(t, reader)
	defer closeRuntimeBodyResource(t, writer)

	if slowBody {
		body = reader
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", body)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})

	go func() {
		defer close(done)

		server.Handler.ServeHTTP(recorder, req)
	}()

	synctest.Wait()
	time.Sleep(2 * time.Minute)
	synctest.Wait()

	select {
	case <-done:
	default:
		closeRuntimeBodyResource(t, writer)

		<-done
		t.Fatal("runtime H3 handler remained blocked waiting for body FIN")
	}

	wantStatus := http.StatusOK

	if slowBody {
		wantStatus = http.StatusRequestTimeout
	}

	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d", recorder.Code, wantStatus)
	}

	if !slowBody && recorder.Body.String() != "data: complete\n\n" {
		t.Fatalf("long response = %q", recorder.Body.String())
	}
}

func closeRuntimeBodyResource(t *testing.T, resource io.Closer) {
	t.Helper()

	if err := resource.Close(); err != nil {
		t.Error(err)
	}
}
