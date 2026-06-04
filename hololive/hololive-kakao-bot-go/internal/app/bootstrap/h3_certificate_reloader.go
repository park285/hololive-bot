package bootstrap

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

const defaultH3CertificateReloadInterval = 45 * time.Second

type tlsCertificateFileReader func(string) ([]byte, error)

type tlsCertificatePairParser func([]byte, []byte) (tls.Certificate, error)

type tlsCertificateFingerprint struct {
	certPEM [sha256.Size]byte
	keyPEM  [sha256.Size]byte
}

type tlsCertificatePairPEM struct {
	certPEM     []byte
	keyPEM      []byte
	fingerprint tlsCertificateFingerprint
}

type tlsCertificateSnapshot struct {
	cert        *tls.Certificate
	fingerprint tlsCertificateFingerprint
}

type reloadingTLSCertificateOptions struct {
	reloadInterval       time.Duration
	readFile             tlsCertificateFileReader
	parseCertificatePair tlsCertificatePairParser
}

type reloadingTLSCertificate struct {
	certFile             string
	keyFile              string
	logger               *slog.Logger
	readFile             tlsCertificateFileReader
	parseCertificatePair tlsCertificatePairParser

	reloadInterval time.Duration
	current        atomic.Value
	startOnce      sync.Once
	failureMu      sync.Mutex

	lastReloadFailure string
}

func newReloadingTLSCertificate(certFile, keyFile string, logger *slog.Logger) (*reloadingTLSCertificate, error) {
	return newReloadingTLSCertificateWithOptions(certFile, keyFile, logger, reloadingTLSCertificateOptions{})
}

func newReloadingTLSCertificateWithOptions(
	certFile, keyFile string,
	logger *slog.Logger,
	options reloadingTLSCertificateOptions,
) (*reloadingTLSCertificate, error) {
	readFile := options.readFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	parseCertificatePair := options.parseCertificatePair
	if parseCertificatePair == nil {
		parseCertificatePair = tls.X509KeyPair
	}
	reloadInterval := options.reloadInterval
	if reloadInterval <= 0 {
		reloadInterval = defaultH3CertificateReloadInterval
	}

	cert, fingerprint, err := loadTLSCertificatePairWithReader(certFile, keyFile, readFile, parseCertificatePair)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate pair: %w", err)
	}

	reloader := &reloadingTLSCertificate{
		certFile:             certFile,
		keyFile:              keyFile,
		logger:               logger,
		readFile:             readFile,
		parseCertificatePair: parseCertificatePair,
		reloadInterval:       reloadInterval,
	}
	reloader.current.Store(&tlsCertificateSnapshot{cert: cert, fingerprint: fingerprint})

	return reloader, nil
}

func (r *reloadingTLSCertificate) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	r.startOnce.Do(func() {
		go r.runReloadLoop(ctx)
	})
}

func (r *reloadingTLSCertificate) cachedCertificate() *tls.Certificate {
	snapshot := r.cachedSnapshot()
	if snapshot == nil {
		return nil
	}

	return snapshot.cert
}

func (r *reloadingTLSCertificate) runReloadLoop(ctx context.Context) {
	ticker := time.NewTicker(r.reloadInterval)
	defer ticker.Stop()

	for waitH3CertificateReloadTick(ctx, ticker.C) {
		r.reloadOnce()
	}
}

func waitH3CertificateReloadTick(ctx context.Context, ticks <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticks:
		return true
	}
}

func (r *reloadingTLSCertificate) reloadOnce() {
	pairPEM, err := readTLSCertificatePairPEM(
		r.certFile,
		r.keyFile,
		r.readFile,
	)
	if err != nil {
		r.recordReloadFailure(err)
		return
	}

	snapshot := r.cachedSnapshot()
	if snapshot != nil && pairPEM.fingerprint == snapshot.fingerprint {
		r.clearReloadFailure()
		return
	}

	cert, err := parseTLSCertificatePair(pairPEM, r.parseCertificatePair)
	if err != nil {
		r.recordReloadFailure(err)
		return
	}

	r.current.Store(&tlsCertificateSnapshot{cert: cert, fingerprint: pairPEM.fingerprint})
	r.clearReloadFailure()
}

func (r *reloadingTLSCertificate) cachedSnapshot() *tlsCertificateSnapshot {
	snapshot, _ := r.current.Load().(*tlsCertificateSnapshot)
	return snapshot
}

func (r *reloadingTLSCertificate) recordReloadFailure(err error) {
	if r.logger == nil {
		return
	}

	r.failureMu.Lock()
	defer r.failureMu.Unlock()

	message := err.Error()
	if message == r.lastReloadFailure {
		return
	}

	r.lastReloadFailure = message

	r.logger.Warn(
		"h3 certificate reload failed; using previous certificate",
		"error", err,
	)
}

func (r *reloadingTLSCertificate) clearReloadFailure() {
	r.failureMu.Lock()
	defer r.failureMu.Unlock()

	r.lastReloadFailure = ""
}

func loadTLSCertificatePair(certFile, keyFile string) (*tls.Certificate, tlsCertificateFingerprint, error) {
	return loadTLSCertificatePairWithReader(certFile, keyFile, os.ReadFile, tls.X509KeyPair)
}

func loadTLSCertificatePairWithReader(
	certFile, keyFile string,
	readFile tlsCertificateFileReader,
	parseCertificatePair tlsCertificatePairParser,
) (*tls.Certificate, tlsCertificateFingerprint, error) {
	pairPEM, err := readTLSCertificatePairPEM(certFile, keyFile, readFile)
	if err != nil {
		return nil, pairPEM.fingerprint, err
	}

	cert, err := parseTLSCertificatePair(pairPEM, parseCertificatePair)
	if err != nil {
		return nil, pairPEM.fingerprint, err
	}

	return cert, pairPEM.fingerprint, nil
}

func readTLSCertificatePairPEM(
	certFile, keyFile string,
	readFile tlsCertificateFileReader,
) (tlsCertificatePairPEM, error) {
	var pairPEM tlsCertificatePairPEM
	// #nosec G304 -- H3 certificate path is operator-owned config, not user input.
	certPEM, err := readFile(certFile)
	if err != nil {
		return pairPEM, fmt.Errorf("read h3 certificate file: %w", err)
	}

	// #nosec G304 -- H3 private key path is operator-owned config, not user input.
	keyPEM, err := readFile(keyFile)
	if err != nil {
		return pairPEM, fmt.Errorf("read h3 key file: %w", err)
	}

	pairPEM = tlsCertificatePairPEM{
		certPEM: certPEM,
		keyPEM:  keyPEM,
		fingerprint: tlsCertificateFingerprint{
			certPEM: sha256.Sum256(certPEM),
			keyPEM:  sha256.Sum256(keyPEM),
		},
	}

	return pairPEM, nil
}

func parseTLSCertificatePair(
	pairPEM tlsCertificatePairPEM,
	parseCertificatePair tlsCertificatePairParser,
) (*tls.Certificate, error) {
	cert, err := parseCertificatePair(pairPEM.certPEM, pairPEM.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse h3 certificate pair: %w", err)
	}

	return &cert, nil
}

func (r *reloadingTLSCertificate) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := r.cachedCertificate()
	if cert == nil {
		return nil, fmt.Errorf("load h3 certificate: no cached certificate")
	}

	return cert, nil
}
