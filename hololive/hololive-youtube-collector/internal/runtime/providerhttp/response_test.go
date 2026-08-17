package providerhttp

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestProviderStatusClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		status int
		code   collecterr.ErrorCode
		class  collecterr.FailureClass
	}{
		{name: "redirect", status: http.StatusFound, code: collecterr.Failed, class: collecterr.ClassProtocol},
		{name: "bad request", status: http.StatusBadRequest, code: collecterr.Failed, class: collecterr.ClassProtocol},
		{name: "unauthorized", status: http.StatusUnauthorized, code: collecterr.Configuration, class: collecterr.ClassConfiguration},
		{name: "request timeout", status: http.StatusRequestTimeout, code: collecterr.Failed, class: collecterr.ClassTransient},
		{name: "too early", status: http.StatusTooEarly, code: collecterr.Failed, class: collecterr.ClassTransient},
		{name: "rate limited", status: http.StatusTooManyRequests, code: collecterr.Cooldown, class: collecterr.ClassCooldown},
		{name: "server error", status: http.StatusInternalServerError, code: collecterr.Failed, class: collecterr.ClassTransient},
		{name: "not implemented", status: http.StatusNotImplemented, code: collecterr.Failed, class: collecterr.ClassProtocol},
		{name: "unknown client error", status: 499, code: collecterr.Failed, class: collecterr.ClassProtocol},
		{name: "unknown server error", status: 599, code: collecterr.Failed, class: collecterr.ClassTransient},
		{name: "unexpected success", status: http.StatusCreated, code: collecterr.Failed, class: collecterr.ClassProtocol},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := mapProviderStatus(contract.ProviderHolodex, test.status, "", "diagnostic")
			if collecterr.CodeOf(err) != test.code || collecterr.ClassOf(err) != test.class {
				t.Fatalf("status %d = %s/%s, want %s/%s", test.status, collecterr.CodeOf(err), collecterr.ClassOf(err), test.code, test.class)
			}
		})
	}
}

func TestMapRequestErrorPreservesKnownFailureClass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		err   error
		code  collecterr.ErrorCode
		class collecterr.FailureClass
	}{
		{name: "timeout", err: context.DeadlineExceeded, code: collecterr.Timeout, class: collecterr.ClassTimeout},
		{name: "canceled", err: context.Canceled, code: collecterr.Canceled, class: collecterr.ClassCanceled},
		{name: "transient", err: syscall.ECONNRESET, code: collecterr.Failed, class: collecterr.ClassTransient},
		{name: "unknown", err: errors.New("unknown"), code: collecterr.Internal, class: collecterr.ClassInternal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := MapRequestError("request", test.err)
			if collecterr.CodeOf(err) != test.code || collecterr.ClassOf(err) != test.class {
				t.Fatalf("error = %s/%s, want %s/%s", collecterr.CodeOf(err), collecterr.ClassOf(err), test.code, test.class)
			}
		})
	}
}

func TestHTTP007UnauthorizedAndForbiddenAreConfiguration(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		err := readStatus(t, status, "", `{"error":"no"}`)
		if collecterr.CodeOf(err) != collecterr.Configuration || collecterr.ClassOf(err) != collecterr.ClassConfiguration {
			t.Fatalf("status %d = %s/%s", status, collecterr.CodeOf(err), collecterr.ClassOf(err))
		}
	}
}

func TestHTTP008RetryAfterCooldownHints(t *testing.T) {
	t.Parallel()
	tooMany := readStatus(t, http.StatusTooManyRequests, "12", `{"error":"slow"}`)
	if collecterr.CodeOf(tooMany) != collecterr.Cooldown || collecterr.RetryOf(tooMany).After() != 12*time.Second {
		t.Fatalf("429 = %s/%v", collecterr.CodeOf(tooMany), collecterr.RetryOf(tooMany))
	}
	svc := readStatus(t, http.StatusServiceUnavailable, "5", `{"error":"wait"}`)
	if collecterr.CodeOf(svc) != collecterr.Cooldown || collecterr.RetryOf(svc).After() != 5*time.Second {
		t.Fatalf("503+Retry-After = %s/%v", collecterr.CodeOf(svc), collecterr.RetryOf(svc))
	}
	bare := readStatus(t, http.StatusServiceUnavailable, "", `{"error":"down"}`)
	if collecterr.CodeOf(bare) != collecterr.Failed || collecterr.ClassOf(bare) != collecterr.ClassTransient {
		t.Fatalf("bare 503 = %s/%s", collecterr.CodeOf(bare), collecterr.ClassOf(bare))
	}
}

func TestHTTP009GzipBombHitsDecompressedCap(t *testing.T) {
	t.Parallel()
	payload := append(bytes.Repeat([]byte(" "), 8192), []byte("[]")...)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if compressed.Len() >= 1024 {
		t.Fatalf("fixture must be small compressed, got %d", compressed.Len())
	}
	resp := &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewReader(payload)),
		Uncompressed:  true,
		ContentLength: int64(compressed.Len()),
	}
	resp.Header.Set("Content-Type", "application/json")
	_, err := ReadProviderJSONDocument(context.Background(), resp, DefaultJSONPolicy(1024), contract.ProviderHolodex)
	if collecterr.CodeOf(err) != collecterr.ResponseTooLarge || collecterr.ClassOf(err) != collecterr.ClassResourceLimit {
		t.Fatalf("gzip bomb = %v", err)
	}
}

func TestHTTP010NonSuccessHugeBodyIsCappedAndSanitized(t *testing.T) {
	t.Parallel()
	const secret = "query-secret"
	payload := bytes.Repeat([]byte("x"), 64<<10)
	payload = append([]byte(`https://example.test/path?token=`+secret+" "), payload...)
	var reads atomic.Int64
	var closes atomic.Int32
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Header:     make(http.Header),
		Body:       &countingReadCloser{r: bytes.NewReader(payload), reads: &reads, closes: &closes},
	}
	err := readResponse(t, resp, DefaultJSONPolicy(1024))
	if err == nil {
		t.Fatal("500 response returned nil error")
	}
	if collecterr.ClassOf(err) != collecterr.ClassTransient {
		t.Fatalf("500 = %v", err)
	}
	policy := DefaultJSONPolicy(1024)
	if reads.Load() > policy.MaxErrorBodyBytes+1+policy.MaxDrainBytes {
		t.Fatalf("read %d bytes of error body", reads.Load())
	}
	if closes.Load() != 1 {
		t.Fatalf("closes = %d", closes.Load())
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("leaked query secret: %v", err)
	}
}

func TestProviderResponseDrainsBoundedRemainder(t *testing.T) {
	t.Parallel()
	policy := DefaultJSONPolicy(8)
	payload := bytes.Repeat([]byte("x"), int(policy.MaxSuccessBodyBytes+1+policy.MaxDrainBytes+1024))
	var reads atomic.Int64
	var closes atomic.Int32
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       &countingReadCloser{r: bytes.NewReader(payload), reads: &reads, closes: &closes},
	}
	resp.Header.Set("Content-Type", "application/json")
	err := readResponse(t, resp, policy)
	if collecterr.CodeOf(err) != collecterr.ResponseTooLarge {
		t.Fatalf("oversized response = %v", err)
	}
	wantReads := policy.MaxSuccessBodyBytes + 1 + policy.MaxDrainBytes
	if reads.Load() != wantReads {
		t.Fatalf("reads = %d, want bounded %d", reads.Load(), wantReads)
	}
	if closes.Load() != 1 {
		t.Fatalf("closes = %d", closes.Load())
	}
}

func TestProviderResponseDrainsForConnectionReuse(t *testing.T) {
	t.Parallel()
	var remoteAddresses [2]string
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := int(calls.Add(1) - 1)
		if index < len(remoteAddresses) {
			remoteAddresses[index] = r.RemoteAddr
		}
		if index == 0 {
			w.Header().Set("Content-Type", "text/plain")
			_, writeErr := w.Write(bytes.Repeat([]byte("x"), 1024))
			if writeErr != nil {
				t.Errorf("write first response: %v", writeErr)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, writeErr := w.Write([]byte(`[]`))
		if writeErr != nil {
			t.Errorf("write second response: %v", writeErr)
		}
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	firstErr := readTestProviderResponse(t, client, server.URL)
	if collecterr.ClassOf(firstErr) != collecterr.ClassProtocol {
		t.Fatalf("first response = %v", firstErr)
	}
	if secondErr := readTestProviderResponse(t, client, server.URL); secondErr != nil {
		t.Fatalf("second response = %v", secondErr)
	}
	if remoteAddresses[0] == "" || remoteAddresses[0] != remoteAddresses[1] {
		t.Fatalf("connection was not reused: %#v", remoteAddresses)
	}
}

func readTestProviderResponse(t *testing.T, client *http.Client, endpoint string) error {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req) //nolint:bodyclose // 검증 대상 reader가 응답 본문을 닫는 계약을 함께 검사한다.
	if err != nil {
		t.Fatal(err)
	}
	_, err = ReadProviderJSONDocument(context.Background(), resp, DefaultJSONPolicy(2048), contract.ProviderHolodex)
	return err
}

func TestHTTP011CanceledBodyDoesNotDrainAndClosesOnce(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	var reads atomic.Int64
	var closes atomic.Int32
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: &countingReadCloser{
			r:      &cancelOnRead{cancel: cancel, rest: bytes.NewReader(bytes.Repeat([]byte("x"), 1<<20))},
			reads:  &reads,
			closes: &closes,
		},
	}
	resp.Header.Set("Content-Type", "application/json")
	_, err := ReadProviderJSONDocument(ctx, resp, DefaultJSONPolicy(1<<20), contract.ProviderHolodex)
	if collecterr.CodeOf(err) != collecterr.Canceled {
		t.Fatalf("canceled = %v", err)
	}
	if reads.Load() > 4096 {
		t.Fatalf("unbounded drain read %d", reads.Load())
	}
	if closes.Load() != 1 {
		t.Fatalf("closes = %d", closes.Load())
	}
}

func TestHTTP015InvalidContentTypeAndTrailingJSON(t *testing.T) {
	t.Parallel()
	plain := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`[]`)),
	}
	plain.Header.Set("Content-Type", "text/plain")
	err := readResponse(t, plain, DefaultJSONPolicy(1024))
	if collecterr.ClassOf(err) != collecterr.ClassProtocol {
		t.Fatalf("content-type = %v", err)
	}
	trailing := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`[]{}`)),
	}
	trailing.Header.Set("Content-Type", "application/json")
	err = readResponse(t, trailing, DefaultJSONPolicy(1024))
	if collecterr.ClassOf(err) != collecterr.ClassProtocol {
		t.Fatalf("trailing JSON = %v", err)
	}
}

func readStatus(t *testing.T, status int, retryAfter, body string) error {
	t.Helper()
	resp := &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	if retryAfter != "" {
		resp.Header.Set("Retry-After", retryAfter)
	}
	return readResponse(t, resp, DefaultJSONPolicy(1024))
}

func readResponse(t *testing.T, resp *http.Response, policy ProviderResponsePolicy) error {
	t.Helper()
	_, err := ReadProviderJSONDocument(context.Background(), resp, policy, contract.ProviderHolodex)
	return err
}

type countingReadCloser struct {
	r      io.Reader
	reads  *atomic.Int64
	closes *atomic.Int32
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.reads.Add(int64(n))
	return n, err
}

func (c *countingReadCloser) Close() error {
	c.closes.Add(1)
	if closer, ok := c.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type cancelOnRead struct {
	cancel context.CancelFunc
	rest   io.Reader
	done   bool
}

func (r *cancelOnRead) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		r.cancel()
		if len(p) == 0 {
			return 0, nil
		}
		p[0] = 'x'
		return 1, nil
	}
	return r.rest.Read(p)
}
