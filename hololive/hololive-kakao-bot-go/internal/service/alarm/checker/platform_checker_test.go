package checker

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

func TestChzzkCheckerCheck_EmptyMappings(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	chzzkChecker, err := NewChzzkChecker(
		cacheSvc,
		chzzk.NewClient(&http.Client{Timeout: time.Second}, chzzk.DefaultBaseURL, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewChzzkChecker() error = %v", err)
	}

	notifications, checkErr := chzzkChecker.Check(context.Background())
	if checkErr != nil {
		t.Fatalf("Check() error = %v", checkErr)
	}
	if len(notifications) != 0 {
		t.Fatalf("expected empty notifications, got %d", len(notifications))
	}
}

func TestTwitchCheckerCheck_EmptyMappings(t *testing.T) {
	t.Parallel()

	cacheSvc := newCheckerTestCacheClient(t)
	twitchChecker, err := NewTwitchChecker(
		cacheSvc,
		twitch.NewClient(twitch.ClientConfig{}, newCheckerTestLogger()),
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewTwitchChecker() error = %v", err)
	}

	notifications, checkErr := twitchChecker.Check(context.Background())
	if checkErr != nil {
		t.Fatalf("Check() error = %v", checkErr)
	}
	if len(notifications) != 0 {
		t.Fatalf("expected empty notifications, got %d", len(notifications))
	}
}
