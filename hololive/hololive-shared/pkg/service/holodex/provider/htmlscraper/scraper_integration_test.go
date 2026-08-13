//go:build integration

package htmlscraper

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/shared-go/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestOfficialScheduleAPILiveIntegration(t *testing.T) {
	config := settings.DefaultOfficialScheduleConfig()
	service := &Service{
		httpClient:           httputil.NewExternalAPIClient(config.Timeout),
		logger:               slog.New(slog.NewTextHandler(io.Discard, nil)),
		officialSchedule:     config,
		maxResponseBodyBytes: settings.DefaultMaxResponseBodyBytes,
		identityIndex:        officialScheduleIdentityIndex{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	streams, err := service.fetchOfficialScheduleAPI(ctx)
	if err != nil {
		t.Fatalf("fetchOfficialScheduleAPI() error = %v", err)
	}
	for index, stream := range streams {
		validateOfficialScheduleIntegrationStream(t, index, stream)
	}
}

func validateOfficialScheduleIntegrationStream(t *testing.T, index int, stream *domain.Stream) {
	t.Helper()
	if stream == nil || stream.ID == "" {
		t.Fatalf("stream %d has no identity: %#v", index, stream)
	}
	if stream.Status != domain.StreamStatusUpcoming || stream.StartActual != nil {
		t.Fatalf("stream %d violated live-truth ownership: %#v", index, stream)
	}
	if stream.StartScheduled == nil || stream.StartScheduled.Location().String() != "Asia/Tokyo" {
		t.Fatalf("stream %d has invalid schedule time: %#v", index, stream.StartScheduled)
	}
	if stream.Link == nil || *stream.Link == "" {
		t.Fatalf("stream %d has no canonical YouTube URL", index)
	}
}
