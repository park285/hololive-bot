package internalhttp

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"
	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// NewJSONClient는 내부 서비스 URL scheme에 맞는 JSON client를 생성합니다.
// 내부 URL이 https일 때 H3 client를 구성하지 못하면(HOLOLIVE_INTERNAL_H3_* 누락 포함) TCP client로 바꾸지 않고
// 오류를 돌려줍니다. 호출자는 이 오류로 기동을 실패시킵니다. 경고 뒤 기본 client로 내려가던 폴백은 H3 전용 서버에 대한
// 요청을 런타임 실패로 미뤘기 때문에 지웠습니다(stack audit 2026-09-26).
func NewJSONClient(baseURL, apiKey string, timeout time.Duration) (*httputil.JSONClient, error) {
	client, err := NewClientForURLStrict(baseURL, timeout, nil)
	if err != nil {
		return nil, fmt.Errorf("new internal JSON client: %w", err)
	}

	return httputil.NewJSONClientWithHTTPClient(baseURL, apiKey, client), nil
}

// NewClientForURLStrict은 https 내부 URL에는 H3 client를, 그 외에는 internal HTTP client를 반환합니다.
// H3 client 구성 실패를 fallback으로 숨기지 않습니다.
func NewClientForURLStrict(rawURL string, timeout time.Duration, _ *slog.Logger) (*http.Client, error) {
	if !internalURLUsesHTTPS(rawURL) {
		return httputil.NewInternalServiceClient(timeout), nil
	}

	// sharedh3의 closeFn은 transport.Close() 래퍼이고 그 transport는 반환된 client에 실려 있다.
	// 소유자가 CloseClient로 회수하므로 핸들을 호출 경로마다 들고 다니지 않는다.
	options, err := settings.LoadInternalH3ClientOptions()
	if err != nil {
		return nil, fmt.Errorf("load internal H3 client options for %s: %w", rawURL, err)
	}

	client, _, err := sharedh3.NewClient(timeout, options)
	if err != nil {
		return nil, fmt.Errorf("configure internal H3 client for %s: %w", rawURL, err)
	}

	return client, nil
}

func internalURLUsesHTTPS(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && parsed != nil && parsed.Scheme == "https"
}
