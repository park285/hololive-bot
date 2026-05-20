package producerruntime

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

func TestYouTubeProducerRuntimeStartBackgroundServices_DoesNotStartPublishedAtResolverDirectly(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	runtime := &YouTubeProducerRuntime{
		Logger:              slog.New(slog.NewTextHandler(&buf, nil)),
		PublishedAtResolver: &poller.PendingPublishedAtResolver{},
	}

	runtime.startBackgroundServices(context.Background(), make(chan error, 1))

	if strings.Contains(buf.String(), "Pending published_at resolver started") {
		t.Fatalf("startBackgroundServices() unexpectedly started resolver directly, logs=%s", buf.String())
	}
}
