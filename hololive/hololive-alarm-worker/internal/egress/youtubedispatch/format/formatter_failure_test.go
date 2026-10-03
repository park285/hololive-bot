package format

import (
	"context"
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestFormatMessagePreservesMemberNameLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("member name store unavailable")
	cacheClient := cachemocks.NewStrictClient()

	cacheClient.HGetFunc = func(context.Context, string, string) (string, error) {
		return "", lookupErr
	}

	formatter := NewMessageFormatter(nil, cacheClient, nil, nil)

	message, err := formatter.FormatMessage(t.Context(), &domain.YouTubeNotificationOutbox{
		Kind: domain.OutboxKindNewVideo, ChannelID: "channel", Payload: `{"video_id":"video","title":"title"}`,
	})
	if !errors.Is(err, lookupErr) || message != "" {
		t.Fatalf("FormatMessage() = %q, %v; want lookup failure without a replacement message", message, err)
	}
}
