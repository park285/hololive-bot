package bootstrap

import (
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync"
)

type tlsCertificateFingerprint struct {
	certPEM [sha256.Size]byte
	keyPEM  [sha256.Size]byte
}

type reloadingTLSCertificate struct {
	certFile string
	keyFile  string
	logger   *slog.Logger

	mu                sync.Mutex
	cert              *tls.Certificate
	fingerprint       tlsCertificateFingerprint
	lastReloadFailure string
}

func newReloadingTLSCertificate(certFile, keyFile string, logger *slog.Logger) (*reloadingTLSCertificate, error) {
	cert, fingerprint, err := loadTLSCertificatePair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load h3 certificate pair: %w", err)
	}

	return &reloadingTLSCertificate{
		certFile:    certFile,
		keyFile:     keyFile,
		logger:      logger,
		cert:        cert,
		fingerprint: fingerprint,
	}, nil
}

func (r *reloadingTLSCertificate) GetCertificate(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cert, fingerprint, err := loadTLSCertificatePair(r.certFile, r.keyFile)
	if err != nil {
		r.recordReloadFailure(err)

		return r.cert, nil
	}

	if fingerprint == r.fingerprint {
		return r.cert, nil
	}

	r.cert = cert
	r.fingerprint = fingerprint
	r.lastReloadFailure = ""

	return r.cert, nil
}

func (r *reloadingTLSCertificate) recordReloadFailure(err error) {
	if r.logger == nil {
		return
	}

	message := err.Error()
	if message == r.lastReloadFailure {
		return
	}

	r.lastReloadFailure = message

	r.logger.Warn(
		"h3 certificate reload failed; using previous certificate",
		"error", err,
		"cert_file", r.certFile,
		"key_file", r.keyFile,
	)
}

func loadTLSCertificatePair(certFile, keyFile string) (*tls.Certificate, tlsCertificateFingerprint, error) {
	var fingerprint tlsCertificateFingerprint

	// #nosec G304 -- H3 certificate path is operator-owned config, not user input.
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return nil, fingerprint, fmt.Errorf("read h3 certificate file: %w", err)
	}

	// #nosec G304 -- H3 private key path is operator-owned config, not user input.
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fingerprint, fmt.Errorf("read h3 key file: %w", err)
	}

	fingerprint = tlsCertificateFingerprint{
		certPEM: sha256.Sum256(certPEM),
		keyPEM:  sha256.Sum256(keyPEM),
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fingerprint, fmt.Errorf("parse h3 certificate pair: %w", err)
	}

	return &cert, fingerprint, nil
}
