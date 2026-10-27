package collectorruntime

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

// helper 기동 실패 오류는 동작명을 한 번만 담아야 한다. 같은 접두가 leaf·중간·호출부에서 반복되면
// 기동 실패 로그가 원인보다 접두로 채워진다.
func TestStartYouTubeJSHelperFailureNamesActionOnce(t *testing.T) {
	// helper 경로를 주지 않으면 다른 후보를 찾지 않고 기동 전에 실패한다.
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
