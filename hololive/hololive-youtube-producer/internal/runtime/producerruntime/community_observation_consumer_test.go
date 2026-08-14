package producerruntime

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

func TestCommunityObservationLeaseOwner(t *testing.T) {
	t.Parallel()
	if got := communityObservationLeaseOwner(nil); got != youtubeProducerRuntimeName {
		t.Fatalf("lease owner = %q, want %q", got, youtubeProducerRuntimeName)
	}
	cfg := &settings.Config{}
	cfg.Scraper.ActiveActive.InstanceID = " youtube-producer-c "
	if got := communityObservationLeaseOwner(cfg); got != "youtube-producer-c" {
		t.Fatalf("lease owner = %q, want instance id", got)
	}
}

func TestPostgresPoolNilSafe(t *testing.T) {
	t.Parallel()
	if postgresPool(nil) != nil {
		t.Fatal("nil infra must not yield a pool")
	}
	if postgresPool(&youtubeProducerInfrastructure{}) != nil {
		t.Fatal("missing postgres service must not yield a pool")
	}
	if communityObservationRunnerFrom(nil) != nil {
		t.Fatal("nil consumer must not become a typed interface")
	}
}

func TestYouTubeProducerRuntimeStartsCommunityObservation(t *testing.T) {
	started := make(chan struct{})
	runtime := &YouTubeProducerRuntime{
		Logger: testLogger(),
		CommunityObservation: communityObservationStartFunc(func(ctx context.Context) {
			close(started)
			<-ctx.Done()
		}),
		Managed: lifecycle.NewManaged(func() {}),
	}

	runCtx, cancel := context.WithCancel(context.Background())
	runtime.startBackgroundServices(runCtx, make(chan error, 1))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("community observation consumer was not started")
	}
	cancel()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	if err := runtime.shutdownRuntime(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

type communityObservationStartFunc func(context.Context)

func (f communityObservationStartFunc) Start(ctx context.Context) {
	f(ctx)
}
