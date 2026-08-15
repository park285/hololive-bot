package collectorruntime

import (
	"context"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

func TestNewRegistryRejectsDuplicateJob(t *testing.T) {
	t.Parallel()
	_, err := NewRegistry(
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
	)
	if err == nil {
		t.Fatal("duplicate runner must fail closed")
	}
}

func TestNewRegistryRejectsEmissionMismatch(t *testing.T) {
	t.Parallel()
	_, err := NewRegistry(stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindVideoList))
	if err == nil {
		t.Fatal("emission mismatch must fail closed")
	}
}

func TestNewRegistryRequiresInitialJobCoverage(t *testing.T) {
	t.Parallel()
	_, err := NewRegistry(stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage))
	if err == nil {
		t.Fatal("incomplete InitialJobContracts coverage must fail closed")
	}
}

func TestNewRegistryAcceptsCompleteAdapterSet(t *testing.T) {
	t.Parallel()
	registry, err := NewRegistry(completeStubRunners()...)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Runners()) != 9 {
		t.Fatalf("runners = %d", len(registry.Runners()))
	}
}

func completeStubRunners() []JobRunner {
	return []JobRunner{
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_content", contract.KindVideoList, contract.KindShortsList),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_live", contract.KindLiveSnapshot),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_metadata",
			contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_viewer", contract.KindViewerSample),
		stubJob(contract.ProviderHolodex, "holodex_live", contract.KindLiveSnapshot, contract.KindViewerSample),
		stubJob(contract.ProviderHolodex, "holodex_metadata", contract.KindChannelStats, contract.KindChannelPhoto),
		stubJob(contract.ProviderHolodex, "holodex_schedule", contract.KindSchedule),
		stubJob(contract.ProviderHololiveOfficial, "official_schedule", contract.KindSchedule),
	}
}

type stubRunner struct {
	provider  contract.Provider
	jobKind   string
	emissions []contract.ObservationKind
	collect   func(context.Context, collectutil.RunInput) (collectutil.RunOutput, error)
}

func stubJob(provider contract.Provider, jobKind string, kinds ...contract.ObservationKind) *stubRunner {
	return &stubRunner{provider: provider, jobKind: jobKind, emissions: kinds}
}

func (s *stubRunner) Provider() contract.Provider           { return s.provider }
func (s *stubRunner) JobKind() string                       { return s.jobKind }
func (s *stubRunner) Emissions() []contract.ObservationKind { return s.emissions }
func (s *stubRunner) TargetKinds() []contract.ObservationKind {
	return s.emissions
}
func (s *stubRunner) Collect(ctx context.Context, input collectutil.RunInput) (collectutil.RunOutput, error) {
	if s.collect != nil {
		return s.collect(ctx, input)
	}
	return collectutil.RunOutput{}, nil
}
