package format

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFormatMessagePreservesMemberNameLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("member name store unavailable")
	formatter := NewMessageFormatter(nil, failingMemberNameSource{err: lookupErr}, nil)

	message, err := formatter.FormatMessage(t.Context(), &domain.YouTubeNotificationOutbox{
		Kind: domain.OutboxKindNewVideo, ChannelID: "channel", Payload: `{"video_id":"video","title":"title"}`,
	})
	if !errors.Is(err, lookupErr) || message != "" {
		t.Fatalf("FormatMessage() = %q, %v; want lookup failure without a replacement message", message, err)
	}
}
