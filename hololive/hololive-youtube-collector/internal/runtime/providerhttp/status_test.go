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
		{name: "connection refused", err: dialError(syscall.ECONNREFUSED)},
		{name: "host unreachable", err: dialError(syscall.EHOSTUNREACH)},
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
			if collecterr.CodeOf(err) != collecterr.Internal || collecterr.ClassOf(err) != collecterr.ClassInternal {
				t.Fatalf("error = %s/%s, want %s/%s", collecterr.CodeOf(err), collecterr.ClassOf(err), collecterr.Internal, collecterr.ClassInternal)
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
		code  collecterr.ErrorCode
		class collecterr.FailureClass
	}{
		{name: "timeout", err: context.DeadlineExceeded, code: collecterr.Timeout, class: collecterr.ClassTimeout},
		{name: "canceled", err: context.Canceled, code: collecterr.Canceled, class: collecterr.ClassCanceled},
		{name: "transient", err: dialError(syscall.ECONNRESET), code: collecterr.Failed, class: collecterr.ClassTransient},
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
		{name: "dial error", err: collecterr.FromContext(fmt.Errorf("read holodex: %w", dialError(syscall.ECONNREFUSED)))},
		{name: "tls error", err: collecterr.FromContext(fmt.Errorf("read holodex: %w", tlsVerifyError()))},
		{name: "dial error with retry hint", err: collecterr.WithRetry(collecterr.FromContext(dialError(syscall.ECONNREFUSED)), hint)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			redacted := RedactError(test.err, "secret-key")
			if !collecterr.IsUnclassified(redacted) {
				t.Fatalf("IsUnclassified(%v) = false, want true", redacted)
			}
			if collecterr.CodeOf(redacted) != collecterr.Internal || collecterr.ClassOf(redacted) != collecterr.ClassInternal {
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
		code  collecterr.ErrorCode
		class collecterr.FailureClass
	}{
		{name: "internal", err: collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, errors.New("invariant key=secret-key")), code: collecterr.Internal, class: collecterr.ClassInternal},
		{name: "protocol", err: collecterr.Wrap(collecterr.Failed, collecterr.ClassProtocol, errors.New("bad body key=secret-key")), code: collecterr.Failed, class: collecterr.ClassProtocol},
		{name: "configuration", err: collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "bad key secret-key"), code: collecterr.Configuration, class: collecterr.ClassConfiguration},
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
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
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
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+addr+"/live?key=secret-key", http.NoBody)
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
	mapped := MapRequestError("request holodex live", doErr, "secret-key")
	if !collecterr.IsUnclassified(mapped) {
		t.Fatalf("IsUnclassified(%v) = false, want true", mapped)
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
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/live?key=secret-key", http.NoBody)
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
	var verifyErr *tls.CertificateVerificationError
	if !errors.As(doErr, &verifyErr) {
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
