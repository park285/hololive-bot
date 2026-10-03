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

func TestFormatMessageReturnsMemberNameLookupFailure(t *testing.T) {
	t.Parallel()

	lookupErr := errors.New("postgres unavailable")
	item := &domain.YouTubeNotificationOutbox{Kind: domain.OutboxKindNewVideo, ChannelID: "UC-test", Payload: `{"video_id":"v1","title":"t"}`}

	// 조회 오류는 대체 문구로 바뀌지 않고 렌더링 전에 실패로 돌아와야 한다. renderer가 nil이어도 이 오류가 먼저 나온다.
	_, err := NewMessageFormatter(nil, failingMemberNameSource{err: lookupErr}, nil).FormatMessage(t.Context(), item)
	require.ErrorIs(t, err, lookupErr)

	_, err = NewMessageFormatter(nil, nil, nil).FormatMessage(t.Context(), item)
	require.ErrorContains(t, err, "member name source is nil")
}
