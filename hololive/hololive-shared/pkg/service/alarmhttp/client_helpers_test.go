package alarmhttp

import (
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

// newTestClient는 origin 검증 없이 평문 HTTP transport로 Client를 만든다. 요청 경로 조립과 오류 처리를
// 운영 생성자가 거부하는 주소로도 시험하기 위해 쓴다.
func newTestClient(baseURL, apiKey string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &Client{
		baseURL:         strings.TrimRight(baseURL, "/"),
		apiKey:          strings.TrimSpace(apiKey),
		httpClient:      httputil.NewInternalServiceClient(10 * time.Second),
		logger:          logger,
		advanceRequests: make(chan struct{}, 1),
	}
}
