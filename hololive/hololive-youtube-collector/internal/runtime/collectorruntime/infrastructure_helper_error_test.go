package collectorruntime

import (
	"strings"
	"testing"

	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

// helper 기동 실패 오류는 동작명을 한 번만 담아야 한다. 같은 접두가 leaf·중간·호출부에서 반복되면
// 기동 실패 로그가 원인보다 접두로 채워진다.
func TestStartYouTubeJSHelperFailureNamesActionOnce(t *testing.T) {
	// 실행 파일이 없는 경로만 주어 helper 경로 해석이 기동 전에 실패하게 한다.
	t.Setenv("YOUTUBEJS_NODE", "")
	t.Setenv("YOUTUBEJS_SCRIPT", "")
	t.Chdir(t.TempDir())

	collector := collectorconfig.DefaultConfig()

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
