package polling

import (
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func TestResolveYouTubeProducerFleetActiveAPCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		configured          int
		activeActiveEnabled bool
		want                int
		wantErr             bool
	}{
		{name: "production four AP fleet", configured: 4, activeActiveEnabled: true, want: 4},
		{name: "single AP ignores stale count", configured: 4, activeActiveEnabled: false, want: 1},
		{name: "active-active requires explicit count", configured: 0, activeActiveEnabled: true, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveYouTubeProducerFleetActiveAPCount(test.configured, test.activeActiveEnabled)
			if (err != nil) != test.wantErr {
				t.Fatalf("resolveYouTubeProducerFleetActiveAPCount() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("resolveYouTubeProducerFleetActiveAPCount() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestValidateYouTubeProducerRuntimeBudgetProductionActiveActiveWithoutLimiter(t *testing.T) {
	t.Parallel()

	registrations := buildBackfillTestRegistrations(settings.ScraperBackfillConfig{}, []string{"UC_A"})
	config := &settings.ScraperConfig{
		ActiveActive: settings.ScraperActiveActiveConfig{Enabled: true},
	}
	wiring := &GlobalBudgetWiring{
		ActiveInstanceCount: 4,
		BudgetRPM:           0,
	}

	if err := validateYouTubeProducerRegistrationsAndBudgets(
		registrations,
		config,
		wiring,
		false,
		slog.Default(),
	); err != nil {
		t.Fatalf("validateYouTubeProducerRegistrationsAndBudgets() error = %v", err)
	}
}

func TestSummarizeYouTubeProducerBudgetDoesNotClampInvalidAPCount(t *testing.T) {
	t.Parallel()

	summary := summarizeYouTubeProducerBudgetForFleet(nil, 30, 0)
	if summary.ActiveAPCount != 0 || summary.FleetBudgetRPM != 0 {
		t.Fatalf("summary = %#v, want invalid count to remain visible to the validating caller", summary)
	}
}
