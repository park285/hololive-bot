package format

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type failingMemberNameSource struct{ err error }

func (s failingMemberNameSource) GetMemberName(context.Context, string) (string, error) {
	return "", s.err
}

type staticMemberNameSource struct{ name string }

func (s staticMemberNameSource) GetMemberName(context.Context, string) (string, error) {
	return s.name, nil
}

func TestFormatMessagesReturnsMemberNameLookupFailure(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("postgres unavailable")
	items := []domain.YouTubeNotificationOutbox{{Kind: domain.OutboxKindNewVideo, ChannelID: "UC-test", Payload: `{"video_id":"v1","title":"t"}`}}

	results, err := NewMessageFormatter(nil, failingMemberNameSource{err: lookupErr}, nil).FormatMessages(t.Context(), items)
	require.NoError(t, err)
	require.ErrorIs(t, results[0].Err, lookupErr)

	results, err = NewMessageFormatter(nil, nil, nil).FormatMessages(t.Context(), items)
	require.NoError(t, err)
	require.ErrorContains(t, results[0].Err, "member name source is nil")
}
