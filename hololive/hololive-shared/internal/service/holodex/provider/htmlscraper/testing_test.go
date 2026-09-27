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

	service, err := NewServiceWithDependencies(
		nil,
		ServiceDependencies{HTTP: httpClient},
		logger,
		settings.OfficialScheduleRuntimeConfig{
			OfficialSchedule:     config,
			MaxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		},
	)
	if err != nil {
		t.Fatalf("NewServiceWithDependencies() error = %v", err)
	}

	return service
}
