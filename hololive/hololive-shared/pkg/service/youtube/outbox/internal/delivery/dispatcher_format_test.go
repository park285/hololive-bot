package delivery

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFormatVideoMessageLiveStreamUsesBroadcastHeader(t *testing.T) {
	formatter := &MessageFormatter{}

	message, err := formatter.formatVideoMessage(
		"Test Member",
		`{"video_id":"live-1","title":"Live One"}`,
		domain.OutboxKindLiveStream,
	)

	if err != nil {
		t.Fatalf("formatVideoMessage() error = %v", err)
	}
	if !strings.Contains(message, "📺 Test Member 방송 알림") {
		t.Fatalf("formatVideoMessage() = %q, want broadcast header", message)
	}
	if !strings.Contains(message, "https://youtu.be/live-1") {
		t.Fatalf("formatVideoMessage() = %q, want watch URL", message)
	}
}
