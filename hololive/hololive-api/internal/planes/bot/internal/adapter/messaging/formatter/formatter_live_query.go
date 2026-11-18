package formatter

import (
	"context"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (f *ResponseFormatter) LiveQuery(ctx context.Context, result livequery.Result, memberName string, collabMembers map[string][]string) string {
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
			Channel:            &domain.Channel{ID: item.ChannelID, Name: item.ChannelName, Org: new(item.Org)},
			CollaboTalentNames: collabMembers[item.VideoID],
		}
	}

	rendered, ok := f.renderLiveStreams(ctx, streams, result.AsOf)
	if !ok {
		return f.renderFailureText()
	}

	if result.Truncated {
		rendered = insertHeadNotice(rendered, "표시 한도를 초과한 방송이 있습니다.")
	}

	return f.foldStreamList(rendered, streams)
}

func insertHeadNotice(text, notice string) string {
	head, rest, found := strings.Cut(text, "\n")
	if !found {
		return text + "\n" + notice
	}

	return head + "\n" + notice + "\n" + rest
}
