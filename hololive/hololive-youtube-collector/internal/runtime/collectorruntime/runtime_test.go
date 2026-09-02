package collectorruntime

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	collectorconfig "github.com/kapu/hololive-shared/pkg/config/settings/collector"
)

func readWalkedSource(root, path string) ([]byte, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("rel: %w", err)
	}

	out, err := fs.ReadFile(os.DirFS(root), filepath.ToSlash(relative))
	if err != nil {
		return out, fmt.Errorf("read file: %w", err)
	}

	return out, nil
}

func TestBuildRequiresRuntimeAllowEnv(t *testing.T) {
	runtime, err := Build(t.Context(), &collectorconfig.RuntimeConfig{
		RuntimeOwnership: collectorconfig.RuntimeOwnershipConfig{},
	}, testLogger())
	if err == nil || runtime != nil {
		t.Fatalf("Build() = %#v, %v, want runtime disabled error", runtime, err)
	}

	if err.Error() != "youtube collector runtime disabled: set YOUTUBE_COLLECTOR_RUNTIME_ALLOWED=true on the owning host" {
		t.Fatalf("Build() error = %q", err)
	}
}

func TestBuildRequiresWorkerProfile(t *testing.T) {
	runtime, err := Build(t.Context(), &collectorconfig.RuntimeConfig{
		RuntimeOwnership: collectorconfig.RuntimeOwnershipConfig{
			RuntimeAllowed: true,
		},
	}, testLogger())
	if err == nil || runtime != nil {
		t.Fatalf("Build() = %#v, %v, want worker profile error", runtime, err)
	}

	if err.Error() != "youtube collector worker profile is required" {
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
			return fmt.Errorf("read walked source: %w", readErr)
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
			return fmt.Errorf("read walked source: %w", readErr)
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
			return fmt.Errorf("read walked source: %w", readErr)
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
	return slog.New(slog.DiscardHandler)
}
