package cache

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
)

func validateCacheSentinel(config Config) error {
	if config.Host != "" || config.SocketPath != "" || config.ForceSingleClient {
		return errors.New("sentinel and direct cache endpoints are mutually exclusive")
	}

	if config.SentinelMasterSet == "" || strings.ContainsAny(config.SentinelMasterSet, " \t\r\n") || len(config.SentinelAddrs) == 0 {
		return errors.New("cache Sentinel primary group and endpoints are required")
	}

	if err := validateCacheSentinelAddresses(config.SentinelAddrs); err != nil {
		return err
	}

	if config.Username == "" || config.Password == "" || config.SentinelUsername == "" || config.SentinelPassword == "" {
		return errors.New("cache and Sentinel each require an explicit username and password")
	}

	if config.TLSCAFile == "" {
		return errors.New("cache Sentinel requires a trusted CA file")
	}

	return nil
}

func validateCacheSentinelAddresses(addresses []string) error {
	for i, address := range addresses {
		host, port, err := net.SplitHostPort(address)
		number, portErr := strconv.Atoi(port)

		if err != nil || portErr != nil || host == "" || number < 1 || number > 65535 ||
			strings.ContainsAny(address, " /?#@\t\r\n") || slices.Contains(addresses[:i], address) {
			return errors.New("cache Sentinel endpoints must be distinct host:port addresses")
		}
	}

	return nil
}

func cacheTLSConfig(caFile string) (*tls.Config, error) {
	certificates, err := os.ReadFile(caFile) // #nosec G304 -- 요청 입력이 아닌 운영자 지정 CA 경로이며 읽기 전용 인증서 mount를 소비한다.
	if err != nil {
		return nil, fmt.Errorf("read cache CA file: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificates) {
		return nil, errors.New("cache CA file contains no certificates")
	}

	// ServerName은 연결할 때 주소에서 정한다. 전환된 primary도 고유 SAN 검증을 받는다.
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, nil
}
