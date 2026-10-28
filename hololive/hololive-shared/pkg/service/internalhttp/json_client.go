package internalhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/park285/shared-go/v2/pkg/httputil"
)

// NewClientForURLStrict은 https 내부 URL에는 H3 client를, 그 외에는 internal HTTP client를 반환합니다.
// H3 client 구성 실패를 fallback으로 숨기지 않습니다.
func NewClientForURLStrict(rawURL string, timeout time.Duration, options sharedh3.ClientOptions) (*http.Client, error) {
	if !internalURLUsesHTTPS(rawURL) {
		return httputil.NewInternalServiceClient(timeout), nil
	}

	// sharedh3의 closeFn은 transport.Close() 래퍼이고 그 transport는 반환된 client에 실려 있다.
	// 소유자가 CloseClient로 회수하므로 핸들을 호출 경로마다 들고 다니지 않는다.
	if err := validateInternalH3Options(options); err != nil {
		return nil, fmt.Errorf("load internal H3 client options for %s: %w", rawURL, err)
	}

	client, _, err := sharedh3.NewClient(timeout, options)
	if err != nil {
		return nil, fmt.Errorf("configure internal H3 client for %s: %w", rawURL, err)
	}

	return client, nil
}

// 기동 때 읽은 전용 설정만 사용하고 client 생성 시 환경을 다시 읽지 않는다.
func validateInternalH3Options(options sharedh3.ClientOptions) error {
	if strings.TrimSpace(options.CACertFile) == "" {
		return errors.New("HOLOLIVE_INTERNAL_H3_CA_CERT_FILE is required")
	}

	if strings.TrimSpace(options.ServerName) == "" {
		return errors.New("HOLOLIVE_INTERNAL_H3_SERVER_NAME is required")
	}

	return nil
}

func internalURLUsesHTTPS(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed != nil && parsed.Scheme == "https"
}
