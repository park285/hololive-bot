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
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	scriptPath, err := filepath.Abs(filepath.Join("..", "..", "..", "youtubejs", "src", "server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	helper, rpc, err := Start(ctx, &Config{
		NodePath: nodePath, ScriptPath: scriptPath,
		RuntimeBaseDir: t.TempDir(), RequestTimeout: 45 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := helper.Close(context.Background()); err != nil {
			t.Errorf("close helper: %v", err)
		}
	})

	channel, err := rpc.FetchChannel(ctx, ChannelRequest{
		ChannelID: channelID, MaxPages: 1, MaxSuccessResponseBytes: 1 << 20,
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
	viewer, err := rpc.FetchViewer(ctx, ViewerRequest{
		VideoID: content.Items[0].VideoID, MaxSuccessResponseBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("fetch real viewer: %v", err)
	}
	if viewer.VideoID != content.Items[0].VideoID || viewer.Availability == "" {
		t.Fatalf("invalid viewer result: %#v", viewer)
	}
}
