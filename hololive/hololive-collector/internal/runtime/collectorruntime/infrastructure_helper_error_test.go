package collectorruntime

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-collector/internal/runtime/youtubejs"
)

func TestStartYouTubeJSHelperFailureNamesActionOnce(t *testing.T) {
	collector := fixtureCollectorConfig(t)

	collector.YouTubeJSNodePath = ""
	collector.YouTubeJSScriptPath = ""

	helper, _, err := startYouTubeJSHelper(t.Context(), &collector, youtubejs.NewRateLimiter(collector.RequestInterval))
	if err == nil {
		t.Fatalf("startYouTubeJSHelper() succeeded without a helper script (close: %v)", helper.Close(t.Context()))
	}

	message := err.Error()
	if !strings.HasPrefix(message, "start youtube.js helper: ") || strings.Count(strings.ToLower(message), "youtube") != 1 {
		t.Fatalf("startYouTubeJSHelper() error = %q, want a single start youtube.js helper prefix", message)
	}

	if !strings.Contains(message, "is not configured") {
		t.Fatalf("startYouTubeJSHelper() error = %q, want unresolved helper path cause", message)
	}
}
