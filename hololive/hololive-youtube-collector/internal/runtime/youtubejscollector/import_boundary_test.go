package youtubejscollector

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYouTubeJSCollectorPackageDoesNotImportCanonicalPersist(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read collector package: %v", err)
	}
	forbidden := []string{
		"poller/runtime/batchrepo",
		"internal/runtime/pollers",
		"PersistCommunityPosts",
		"BuildPostArtifacts",
		"BuildBatch",
		"ArtifactsFromPayload",
		"CollectNewPosts",
		"youtube_community_posts",
		"youtube_notification_outbox",
		"youtube_content_watermarks",
		"youtube_content_alarm_tracking",
		"PollWithLease",
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, readErr := os.ReadFile(filepath.Join(".", name))
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		body := string(src)
		for _, token := range forbidden {
			if strings.Contains(body, token) {
				t.Fatalf("%s must not contain persist helper %q", name, token)
			}
		}
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			if strings.Contains(path, "batchrepo") || strings.Contains(path, "internal/runtime/pollers") {
				t.Fatalf("%s imports persist package %q", name, path)
			}
		}
	}
}
