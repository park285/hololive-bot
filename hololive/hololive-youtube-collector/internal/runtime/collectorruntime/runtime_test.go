package collectorruntime

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

func readWalkedSource(root, path string) ([]byte, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	return fs.ReadFile(os.DirFS(root), filepath.ToSlash(relative))
}

func TestCollectionReadyPayloadFailsClosedWhenPendingQueueIsUnknown(t *testing.T) {
	t.Parallel()
	snapshot := collectionReady{firstSuccess: true, pendingQueueOK: false}
	payload := snapshot.payload("collector-a", "postgres_queue")
	if payload["status"] != "not_ready" || payload["dependency"] != "postgres_queue" || payload["pending_queue"] != nil {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestCollectionReadyRequiresFirstSuccessfulPublish(t *testing.T) {
	t.Parallel()
	snapshot := collectionReady{pendingQueueOK: true, dueJobs: 0}
	if dependency := snapshot.dependency(); dependency != "first_success" {
		t.Fatalf("dependency() = %q, want first_success", dependency)
	}
	snapshot.firstSuccess = true
	if dependency := snapshot.dependency(); dependency != "observation_handoff" {
		t.Fatalf("dependency() = %q, want observation_handoff", dependency)
	}
	snapshot.handoffComplete = true
	if dependency := snapshot.dependency(); dependency != "" {
		t.Fatalf("dependency() = %q, want ready", dependency)
	}
}

func TestBuildRequiresRuntimeAllowEnv(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", "")

	runtime, err := Build(context.Background(), &settings.Config{
		Ingestion: settings.IngestionConfig{YouTubeEnabled: true},
	}, testLogger())
	if err == nil || runtime != nil {
		t.Fatalf("Build() = %#v, %v, want runtime disabled error", runtime, err)
	}
	if err.Error() != "youtube collector runtime disabled: set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true on the owning host" {
		t.Fatalf("Build() error = %q", err)
	}
}

func TestBuildRequiresYouTubeIngestion(t *testing.T) {
	t.Setenv("YOUTUBE_COLLECTOR_RUNTIME_ALLOWED", "true")

	runtime, err := Build(context.Background(), &settings.Config{
		Ingestion: settings.IngestionConfig{YouTubeEnabled: false},
	}, testLogger())
	if err == nil || runtime != nil {
		t.Fatalf("Build() = %#v, %v, want youtube ingestion error", runtime, err)
	}
	if err.Error() != "youtube collector requires YOUTUBE_INGESTION_ENABLED=true" {
		t.Fatalf("Build() error = %q", err)
	}
}

func TestCollectorProductionSourceDoesNotClaimProducerLease(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	forbidden := []string{
		"ingestionlease",
		"WithSchedulerJobClaimer",
		"JobRunGuard",
		"AcquireIngestionLease",
		"YOUTUBE_PRODUCER_ACTIVE_ACTIVE_ENABLED",
		"ProvideYouTubeProducerRateLimiter",
		"sourceobservation.NewRepository(",
		".ClaimBatch(",
		"ProcessNextReplay",
		"RunRetentionTick",
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := readWalkedSource(root, path)
		if readErr != nil {
			return readErr
		}
		body := string(src)
		for _, token := range forbidden {
			if strings.Contains(body, token) {
				t.Errorf("%s must not contain %q", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk collector source: %v", err)
	}
}

func TestCollectorProductionSourceDoesNotUseHolodex(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	forbidden := []string{
		"holodex/provider",
		"ProvideHolodexService",
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := readWalkedSource(root, path)
		if readErr != nil {
			return readErr
		}
		body := string(src)
		for _, token := range forbidden {
			if strings.Contains(body, token) {
				t.Errorf("%s must not contain %q", path, token)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk collector source: %v", err)
	}
}

func TestCollectorProductionSourceDoesNotUseHTMLGetCommunityPosts(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	wiredYouTubeJS := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := readWalkedSource(root, path)
		if readErr != nil {
			return readErr
		}
		body := string(src)
		if strings.Contains(body, "GetCommunityPosts") {
			t.Errorf("%s must not call HTML GetCommunityPosts", path)
		}
		if strings.Contains(body, "hololive-youtube-collector/internal/runtime/youtubejs") {
			wiredYouTubeJS = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk collector source: %v", err)
	}
	if !wiredYouTubeJS {
		t.Fatal("collector production source must import the YouTube.js helper")
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
