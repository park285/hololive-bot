package format

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func TestFormatMessagesPreservesMemberNameLookupError(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("member name store unavailable")
	formatter := NewMessageFormatter(nil, failingMemberNameSource{err: lookupErr}, nil)

	results, err := formatter.FormatMessages(t.Context(), []domain.YouTubeNotificationOutbox{{
		Kind: domain.OutboxKindNewVideo, ChannelID: "channel", Payload: `{"video_id":"video","title":"title"}`,
	}})
	if err != nil || len(results) != 1 {
		t.Fatalf("FormatMessages() = %+v, %v; want one item result", results, err)
	}

	if !errors.Is(results[0].Err, lookupErr) || results[0].Message != (delivery.MessageVariants{}) {
		t.Fatalf("FormatMessages()[0] = %+v; want lookup failure without a replacement message", results[0])
	}
}

func TestFormatMessagesFailsWholeBatchWithoutRenderer(t *testing.T) {
	t.Parallel()

	formatter := NewMessageFormatter(nil, staticMemberNameSource{name: "멤버"}, nil)

	results, err := formatter.FormatMessages(t.Context(), []domain.YouTubeNotificationOutbox{
		{Kind: domain.OutboxKindNewVideo, ChannelID: "a", Payload: `{"video_id":"v1","title":"t1"}`},
		{Kind: domain.OutboxKindNewVideo, ChannelID: "b", Payload: `{"video_id":"v2","title":"t2"}`},
	})
	if err == nil || results != nil {
		t.Fatalf("FormatMessages() = %+v, %v; want batch error without results", results, err)
	}
}
