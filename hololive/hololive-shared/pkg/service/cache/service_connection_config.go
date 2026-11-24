package cache

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

func cacheTLSConfig(caFile string) (*tls.Config, error) {
	certificates, err := os.ReadFile(caFile) // #nosec G304 -- 요청 입력이 아닌 운영자 지정 CA 경로이며 읽기 전용 인증서 mount를 소비한다.
	if err != nil {
		return nil, fmt.Errorf("read cache CA file: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificates) {
		return nil, errors.New("cache CA file contains no certificates")
	}

	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, nil
}
