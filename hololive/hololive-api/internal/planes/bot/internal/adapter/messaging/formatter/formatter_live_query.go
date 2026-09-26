package formatter

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/livequery"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// LiveQuery는 조회 계층의 확실성과 실제 표시 수를 그대로 전달한다.
func (f *ResponseFormatter) LiveQuery(ctx context.Context, result livequery.Result, memberName string) string {
	message := "현재 방송 상태를 확인할 수 없습니다."

	if len(result.Items) > 0 || result.Status == livequery.Complete {
		streams := make([]*domain.Stream, len(result.Items))
		for i, item := range result.Items {
			streams[i] = &domain.Stream{
				ID: item.VideoID, ChannelID: item.ChannelID, ChannelName: item.ChannelName,
				Title: item.Title, Status: domain.StreamStatusLive, StartActual: item.StartedAt,
				Channel: &domain.Channel{ID: item.ChannelID, Name: item.ChannelName, Org: new(item.Org)},
			}
		}

		message = f.FormatLiveStreams(ctx, streams)
		if len(streams) == 0 && memberName != "" {
			message = f.FormatMemberNotLive(ctx, memberName)
		}
	}

	if result.Status != livequery.Complete {
		message += "\n\n조회 미완료: " + liveQueryReasons(result.Channels)
	}

	if result.Truncated {
		message += "\n표시 한도를 초과한 방송이 있습니다."
	}

	if !result.AsOf.IsZero() {
		message += "\n기준: " + util.FormatKST(result.AsOf, "01/02 15:04")
	}

	return message
}

func liveQueryReasons(channels []livequery.Channel) string {
	counts := make(map[livequery.Reason]int)

	for _, channel := range channels {
		counts[channel.Reason]++
	}

	var parts []string

	for _, entry := range []struct {
		reason livequery.Reason
		text   string
	}{
		{livequery.InvalidTarget, "조회 대상 없음"},
		{livequery.InvalidProjection, "수집 대상 정보 만료"},
		{livequery.Uncollected, "미수집"},
		{livequery.Inconsistent, "저장 상태 불일치"},
		{livequery.ConfirmingEnd, "종료 확인 중"},
		{livequery.InvalidClock, "관측 시각 불일치"},
		{livequery.Stale, "방송 관측 만료"},
		{livequery.Incomplete, "수집 범위 미확인"},
	} {
		if count := counts[entry.reason]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d채널", entry.text, count))
		}
	}

	return strings.Join(parts, ", ")
}
