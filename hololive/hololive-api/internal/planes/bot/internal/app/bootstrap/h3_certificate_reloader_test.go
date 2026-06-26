package bootstrap

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReloadingTLSCertificateGetCertificateUsesCachedCertWithoutReadingFiles(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	readCount := &atomic.Int64{}
	reloader := newTestReloadingTLSCertificate(
		t,
		certFile,
		keyFile,
		10*time.Millisecond,
		countingTLSCertificateFileReader(readCount),
	)

	readCount.Store(0)

	for range 100 {
		cert, err := reloader.GetCertificate(&tls.ClientHelloInfo{})
		if err != nil {
			t.Fatalf("GetCertificate() error = %v", err)
		}
		if cert == nil {
			t.Fatal("GetCertificate() certificate = nil")
		}
	}

	if got := readCount.Load(); got != 0 {
		t.Fatalf("reader calls after initial load = %d, want 0", got)
	}
}

func TestReloadingTLSCertificateReloadOnceSkipsParseWhenFingerprintUnchanged(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	readCount := &atomic.Int64{}
	parseCount := &atomic.Int64{}
	reloader := newTestReloadingTLSCertificateWithOptions(
		t,
		certFile,
		keyFile,
		10*time.Millisecond,
		reloadingTLSCertificateOptions{
			readFile:             countingTLSCertificateFileReader(readCount),
			parseCertificatePair: countingTLSCertificatePairParser(parseCount),
		},
	)

	readCount.Store(0)
	parseCount.Store(0)

	for range 5 {
		reloader.reloadOnce()
	}

	if got := readCount.Load(); got != 10 {
		t.Fatalf("reader calls after unchanged reloads = %d, want 10", got)
	}
	if got := parseCount.Load(); got != 0 {
		t.Fatalf("parser calls after unchanged reloads = %d, want 0", got)
	}
}

func TestReloadingTLSCertificateReloadLoopAppliesChangedCertificate(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	reloader := newTestReloadingTLSCertificate(t, certFile, keyFile, 10*time.Millisecond, nil)

	firstSerial := certificateSerial(t, reloader.cachedCertificate())

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reloader.Start(ctx)

	overwriteLocalhostCertificate(t, certFile, keyFile)

	waitForCachedCertificateSerial(t, reloader, func(serial string) bool {
		return serial != firstSerial
	})
}

func TestReloadingTLSCertificateReloadFailureKeepsPreviousCertificateConcurrently(t *testing.T) {
	t.Parallel()

	certFile, keyFile := writeLocalhostCertificate(t)
	reloader := newTestReloadingTLSCertificate(t, certFile, keyFile, 10*time.Millisecond, nil)

	first, err := reloader.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reloader.Start(ctx)

	if err := os.WriteFile(keyFile, []byte("not a private key"), 0o600); err != nil {
		t.Fatalf("write invalid key: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	errs := make(chan string, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				cert, getErr := reloader.GetCertificate(&tls.ClientHelloInfo{})
				if getErr != nil {
					errs <- "GetCertificate returned error"
					return
				}
				if cert != first {
					errs <- "GetCertificate returned replacement certificate after reload failure"
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)

	for msg := range errs {
		t.Fatal(msg)
	}
}

func newTestReloadingTLSCertificate(
	t *testing.T,
	certFile, keyFile string,
	reloadInterval time.Duration,
	readFile tlsCertificateFileReader,
) *reloadingTLSCertificate {
	t.Helper()

	return newTestReloadingTLSCertificateWithOptions(
		t,
		certFile,
		keyFile,
		reloadInterval,
		reloadingTLSCertificateOptions{
			readFile: readFile,
		},
	)
}

func newTestReloadingTLSCertificateWithOptions(
	t *testing.T,
	certFile, keyFile string,
	reloadInterval time.Duration,
	options reloadingTLSCertificateOptions,
) *reloadingTLSCertificate {
	t.Helper()

	options.reloadInterval = reloadInterval
	reloader, err := newReloadingTLSCertificateWithOptions(
		certFile,
		keyFile,
		nil,
		options,
	)
	if err != nil {
		t.Fatalf("newReloadingTLSCertificateWithOptions() error = %v", err)
	}

	return reloader
}

func countingTLSCertificateFileReader(readCount *atomic.Int64) tlsCertificateFileReader {
	root := ""
	return func(name string) ([]byte, error) {
		if root == "" {
			root = filepath.Dir(name)
		}
		rel, err := filepath.Rel(root, name)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("read certificate fixture outside temp dir")
		}
		readCount.Add(1)
		return fs.ReadFile(os.DirFS(root), filepath.ToSlash(rel))
	}
}

func countingTLSCertificatePairParser(parseCount *atomic.Int64) tlsCertificatePairParser {
	return func(certPEM, keyPEM []byte) (tls.Certificate, error) {
		parseCount.Add(1)
		return tls.X509KeyPair(certPEM, keyPEM)
	}
}

func waitForCachedCertificateSerial(
	t *testing.T,
	reloader *reloadingTLSCertificate,
	accept func(string) bool,
) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		serial := certificateSerial(t, reloader.cachedCertificate())
		if accept(serial) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("cached certificate serial did not change before deadline")
}
