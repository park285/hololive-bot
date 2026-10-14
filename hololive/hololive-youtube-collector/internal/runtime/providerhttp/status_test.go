package providerhttp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func dialError(errno syscall.Errno) error {
	return &url.Error{
		Op:  "Get",
		URL: "https://holodex.net/api/v2/live?key=secret-key",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: errno}},
	}
}

func tlsVerifyError() error {
	return &url.Error{
		Op:  "Get",
		URL: "https://holodex.net/api/v2/live?key=secret-key",
		Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
	}
}

func TestMapRequestErrorKeepsUnclassifiedMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "dial permission denied", err: dialError(syscall.EACCES)},
		{name: "x509 unknown authority", err: tlsVerifyError()},
		{name: "tls handshake timeout", err: &url.Error{Op: "Get", URL: "https://holodex.net/live", Err: errors.New("net/http: TLS handshake timeout")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := MapRequestError("request holodex live", test.err, "secret-key")
			if !collecterr.IsUnclassified(err) {
				t.Fatalf("IsUnclassified(%v) = false, want true", err)
			}

			if collecterr.CodeOf(err) != contract.ErrorInternalInvariant || collecterr.ClassOf(err) != contract.ClassInternal {
				t.Fatalf("error = %s/%s, want %s/%s", collecterr.CodeOf(err), collecterr.ClassOf(err), contract.ErrorInternalInvariant, contract.ClassInternal)
			}

			if text := err.Error(); strings.Contains(text, "secret-key") || !strings.HasPrefix(text, "request holodex live: ") {
				t.Fatalf("error text = %q", text)
			}
		})
	}
}

func TestMapRequestErrorKeepsExplicitClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   error
		code  contract.CollectionErrorCode
		class contract.FailureClass
	}{
		{name: "timeout", err: context.DeadlineExceeded, code: contract.ErrorCollectionTimeout, class: contract.ClassTimeout},
		{name: "canceled", err: context.Canceled, code: contract.ErrorCollectionCanceled, class: contract.ClassCanceled},
		{name: "transient", err: dialError(syscall.ECONNRESET), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
		{name: "connection refused", err: dialError(syscall.ECONNREFUSED), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
		{name: "host unreachable", err: dialError(syscall.EHOSTUNREACH), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := MapRequestError("request", test.err)
			if collecterr.IsUnclassified(err) {
				t.Fatalf("IsUnclassified(%v) = true, want false", err)
			}

			if collecterr.CodeOf(err) != test.code || collecterr.ClassOf(err) != test.class {
				t.Fatalf("error = %s/%s, want %s/%s", collecterr.CodeOf(err), collecterr.ClassOf(err), test.code, test.class)
			}
		})
	}
}

func TestRedactErrorKeepsUnclassifiedMarker(t *testing.T) {
	t.Parallel()

	hint, err := collecterr.NewRetryAfterHint(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		err  error
	}{
		{name: "dial error", err: collecterr.FromContext(fmt.Errorf("read holodex: %w", dialError(syscall.EACCES)))},
		{name: "tls error", err: collecterr.FromContext(fmt.Errorf("read holodex: %w", tlsVerifyError()))},
		{name: "dial error with retry hint", err: collecterr.WithRetry(collecterr.FromContext(dialError(syscall.EACCES)), hint)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			redacted := RedactError(test.err, "secret-key")
			if !collecterr.IsUnclassified(redacted) {
				t.Fatalf("IsUnclassified(%v) = false, want true", redacted)
			}

			if collecterr.CodeOf(redacted) != contract.ErrorInternalInvariant || collecterr.ClassOf(redacted) != contract.ClassInternal {
				t.Fatalf("error = %s/%s", collecterr.CodeOf(redacted), collecterr.ClassOf(redacted))
			}

			if collecterr.RetryOf(redacted) != collecterr.RetryOf(test.err) {
				t.Fatalf("retry hint = %v, want %v", collecterr.RetryOf(redacted), collecterr.RetryOf(test.err))
			}

			if strings.Contains(redacted.Error(), "secret-key") {
				t.Fatalf("error text leaks secret: %q", redacted.Error())
			}
		})
	}
}

func TestRedactErrorKeepsExplicitClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   error
		code  contract.CollectionErrorCode
		class contract.FailureClass
	}{
		{name: "internal", err: collecterr.Wrap(contract.ErrorInternalInvariant, contract.ClassInternal, errors.New("invariant key=secret-key")), code: contract.ErrorInternalInvariant, class: contract.ClassInternal},
		{name: "protocol", err: collecterr.Wrap(contract.ErrorCollectionFailed, contract.ClassProtocol, errors.New("bad body key=secret-key")), code: contract.ErrorCollectionFailed, class: contract.ClassProtocol},
		{name: "configuration", err: collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "bad key secret-key"), code: contract.ErrorConfiguration, class: contract.ClassConfiguration},
		{name: "connection refused", err: collecterr.FromContext(fmt.Errorf("read holodex: %w", dialError(syscall.ECONNREFUSED))), code: contract.ErrorCollectionFailed, class: contract.ClassTransient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			redacted := RedactError(test.err, "secret-key")
			if collecterr.IsUnclassified(redacted) {
				t.Fatalf("IsUnclassified(%v) = true, want false", redacted)
			}

			if collecterr.CodeOf(redacted) != test.code || collecterr.ClassOf(redacted) != test.class {
				t.Fatalf("error = %s/%s, want %s/%s", collecterr.CodeOf(redacted), collecterr.ClassOf(redacted), test.code, test.class)
			}

			if strings.Contains(redacted.Error(), "secret-key") {
				t.Fatalf("error text leaks secret: %q", redacted.Error())
			}
		})
	}
}

func TestRedactErrorReturnsOriginalWhenNothingToRedact(t *testing.T) {
	t.Parallel()

	original := collecterr.FromContext(errors.New("read body"))
	if got := RedactError(original, "secret-key"); !errors.Is(got, original) {
		t.Fatalf("RedactError = %v, want the original error", got)
	}
}

func TestMapRequestErrorFromRealClientDialRefused(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := listener.Addr().String()
	if closeErr2 := listener.Close(); closeErr2 != nil {
		t.Fatal(closeErr2)
	}

	client, err := NewProviderHTTPClient(testTransportConfig())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/live?key=secret-key", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, doErr := client.Do(req)
	if doErr == nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}

		t.Fatal("expected dial failure against closed port")
	}

	if !errors.Is(doErr, syscall.ECONNREFUSED) {
		t.Skipf("closed port did not yield ECONNREFUSED: %v", doErr)
	}

	// 상대가 연결을 거부하면 일시 장애다. 미분류 INTERNAL로 두면 외부 장애가 코드 결함으로 집계된다.
	mapped := MapRequestError("request holodex live", doErr, "secret-key")
	if collecterr.IsUnclassified(mapped) ||
		collecterr.CodeOf(mapped) != contract.ErrorCollectionFailed || collecterr.ClassOf(mapped) != contract.ClassTransient {
		t.Fatalf("mapped = %v (%s/%s), want classified %s/%s", mapped, collecterr.CodeOf(mapped), collecterr.ClassOf(mapped), contract.ErrorCollectionFailed, contract.ClassTransient)
	}

	if strings.Contains(mapped.Error(), "secret-key") {
		t.Fatalf("error text leaks secret: %q", mapped.Error())
	}
}

func TestMapRequestErrorFromRealClientTLSVerifyFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client, err := NewProviderHTTPClient(testTransportConfig())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/live?key=secret-key", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, doErr := client.Do(req)
	if doErr == nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}

		t.Fatal("expected TLS verification failure against untrusted test certificate")
	}

	if _, ok := errors.AsType[*tls.CertificateVerificationError](doErr); !ok {
		t.Fatalf("Do() error = %v, want *tls.CertificateVerificationError", doErr)
	}

	mapped := MapRequestError("request holodex live", doErr, "secret-key")
	if !collecterr.IsUnclassified(mapped) {
		t.Fatalf("IsUnclassified(%v) = false, want true", mapped)
	}

	if strings.Contains(mapped.Error(), "secret-key") {
		t.Fatalf("error text leaks secret: %q", mapped.Error())
	}
}

// Holodex·official 응답이 ResponseHeaderTimeout 안에 header를 보내지 않으면 net/http는 Timeout()이 참인 *url.Error를
// 돌려준다. 운영 로그에서 가장 흔한 provider 실패라 INTERNAL 불변식이 아니라 TIMEOUT으로 기록돼야 한다.
func TestMapRequestErrorFromRealClientResponseHeaderTimeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	cfg := testTransportConfig()

	cfg.ResponseHeaderTimeout = 50 * time.Millisecond
	cfg.RequestTimeout = 5 * time.Second

	client, err := NewProviderHTTPClient(cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := client.Close(); closeErr != nil {
			t.Errorf("close client: %v", closeErr)
		}
	})

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/live?key=secret-key", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}

	resp, doErr := client.Do(req)
	if doErr == nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close body: %v", closeErr)
		}

		t.Fatal("expected response header timeout")
	}

	mapped := MapRequestError("request holodex live", doErr, "secret-key")

	if collecterr.CodeOf(mapped) != contract.ErrorCollectionTimeout || collecterr.ClassOf(mapped) != contract.ClassTimeout {
		t.Fatalf("CodeOf/ClassOf(%v) = %s/%s, want %s/%s", mapped, collecterr.CodeOf(mapped), collecterr.ClassOf(mapped),
			contract.ErrorCollectionTimeout, contract.ClassTimeout)
	}

	if collecterr.IsUnclassified(mapped) {
		t.Fatalf("IsUnclassified(%v) = true, want explicit timeout", mapped)
	}

	if strings.Contains(mapped.Error(), "secret-key") {
		t.Fatalf("error text leaks secret: %q", mapped.Error())
	}
}
