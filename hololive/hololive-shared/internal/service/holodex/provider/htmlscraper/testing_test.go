package htmlscraper

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func newTestServiceWithHTTPClient(
	t *testing.T,
	httpClient *http.Client,
	logger *slog.Logger,
	baseURL string,
) *Service {
	t.Helper()

	if logger == nil {
		logger = slog.Default()
	}

	config := settings.DefaultOfficialScheduleConfig()

	config.BaseURL = baseURL

	service, err := NewService(
		t.Context(),
		nil,
		httpClient,
		logger,
		settings.OfficialScheduleRuntimeConfig{
			OfficialSchedule:     config,
			MaxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	return service
}
