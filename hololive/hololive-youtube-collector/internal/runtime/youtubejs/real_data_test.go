package youtubejs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRealYouTubeDataRoundTrip(t *testing.T) {
	channelID := os.Getenv("YOUTUBEJS_REAL_DATA_CHANNEL_ID")
	if channelID == "" {
		t.Skip("set YOUTUBEJS_REAL_DATA_CHANNEL_ID to run the public YouTube smoke test")
	}

	ctx, rpc := startRealDataHelper(t)

	channel, err := rpc.FetchChannel(ctx, ChannelRequest{
		ChannelID: channelID, Kind: "live", MaxPages: 1, MaxSuccessResponseBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("fetch real channel: %v", err)
	}

	if channel.PageCount < 1 || channel.Continuity == "" {
		t.Fatalf("invalid channel pagination: %#v", channel.Pagination)
	}

	content, err := rpc.FetchContent(ctx, ContentRequest{
		ChannelID: channelID, Kind: "videos", MaxResults: 3, MaxPages: 1, MaxSuccessResponseBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("fetch real content: %v", err)
	}

	if content.PageCount < 1 || content.Continuity == "" {
		t.Fatalf("invalid content pagination: %#v", content.Pagination)
	}

	if len(content.Items) == 0 {
		t.Fatal("real channel returned no video items")
	}

	// videos 탭 항목은 시각을 싣지 않고, 공개 시각은 영상별 확인 응답에서만 옵니다. 응답에 시각이 없을 수도 있으므로 존재는 요구하지 않습니다.
	first := content.Items[0]
	if first.PublishedAt != nil || first.ScheduledFor != nil {
		t.Fatalf("videos item carried list-derived times: %#v", first)
	}

	check, err := rpc.FetchVideoLiveCheck(ctx, VideoLiveCheckRequest{VideoID: first.VideoID, MaxSuccessResponseBytes: MaxLiveCheckResponseBytes})
	if err != nil {
		t.Fatalf("fetch real video live check: %v", err)
	}

	if check.VideoID != first.VideoID || (check.PublishedAt != nil && (!check.IdentityConfirmed || check.PublishedAt.After(time.Now()))) {
		t.Fatalf("real video live check publication facts = %#v", check)
	}
}

func startRealDataHelper(t *testing.T) (context.Context, *RPC) {
	t.Helper()

	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}

	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "..", "youtubejs", "src", "server.mjs"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)

	helper, rpc, err := Start(ctx, &Config{
		NodePath: nodePath, ScriptPath: scriptPath,
		RuntimeBaseDir: t.TempDir(), RequestTimeout: 45 * time.Second,
		MaxInflight: DefaultMaxInflight,
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		//nolint:usetesting // t.Context()는 Cleanup 직전에 취소되므로 여기에 쓰면 helper 종료가 즉시 실패한다.
		if closeErr := helper.Close(context.Background()); closeErr != nil {
			t.Errorf("close helper: %v", closeErr)
		}
	})
	t.Cleanup(cancel)

	return ctx, rpc
}
