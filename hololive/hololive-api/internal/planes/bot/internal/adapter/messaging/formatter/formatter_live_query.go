package formatter

import (
	"context"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// LiveQuery는 확정 방송만 표시하고 채널별 조회 진단과 조회 시각은 응답에 붙이지 않는다.
// 전체 scope가 확인되지 않은 빈 결과만 '확인할 수 없음'으로 남겨 방송 없음과 구분한다.
func (f *ResponseFormatter) LiveQuery(ctx context.Context, result livequery.Result, memberName string) string {
	if len(result.Items) == 0 {
		if result.Status != livequery.Complete {
			return "현재 방송 상태를 확인할 수 없습니다."
		}

		if memberName != "" {
			return f.FormatMemberNotLive(ctx, memberName)
		}
	}

	streams := make([]*domain.Stream, len(result.Items))
	for i, item := range result.Items {
		streams[i] = &domain.Stream{
			ID: item.VideoID, ChannelID: item.ChannelID, ChannelName: item.ChannelName,
			Title: item.Title, Status: domain.StreamStatusLive, StartActual: item.StartedAt,
			Channel: &domain.Channel{ID: item.ChannelID, Name: item.ChannelName, Org: new(item.Org)},
		}
	}

	rendered, ok := f.renderLiveStreams(ctx, streams)
	if !ok {
		return messagestrings.FallbackSentinel
	}

	// 표시 한도 안내는 머리 문단에 두어야 '전체보기'로 접힌 화면에서도 보인다.
	if result.Truncated {
		rendered = insertHeadNotice(rendered, "표시 한도를 초과한 방송이 있습니다.")
	}

	return f.foldSeeMore(rendered)
}

func insertHeadNotice(text, notice string) string {
	head, rest, found := strings.Cut(text, "\n")
	if !found {
		return text + "\n" + notice
	}

	return head + "\n" + notice + "\n" + rest
}
