package alarmhttp

import (
	"log/slog"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

func newTestClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL:         strings.TrimRight(baseURL, "/"),
		apiKey:          strings.TrimSpace(apiKey),
		httpClient:      httputil.NewInternalServiceClient(10 * time.Second),
		logger:          slog.New(slog.DiscardHandler),
		advanceRequests: make(chan struct{}, 1),
	}
}
