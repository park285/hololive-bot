package streammapping

import (
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type StreamFilter struct {
	logger *slog.Logger
}

func NewStreamFilter(logger *slog.Logger) *StreamFilter {
	return &StreamFilter{logger: logger}
}

func (f *StreamFilter) FilterHololiveStreams(streams []*domain.Stream) []*domain.Stream {
	filtered := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if f.isHololiveStream(stream) {
			filtered = append(filtered, stream)
		}
	}

	return filtered
}

func (f *StreamFilter) isHololiveStream(stream *domain.Stream) bool {
	if stream.Channel == nil {
		f.logger.Debug("Filtered out stream without channel info", slog.String("id", stream.ID))

		return false
	}

	if !f.isAllowedOrgStream(stream) {
		return false
	}

	if f.IsHolostarsChannel(stream.Channel) {
		f.logger.Debug("Filtered out HOLOSTARS stream", slog.String("channel", stream.ChannelName))

		return false
	}

	return true
}

func (f *StreamFilter) isAllowedOrgStream(stream *domain.Stream) bool {
	channel := stream.Channel
	if channel.Org != nil && isAllowedOrg(*channel.Org) {
		return true
	}

	org := ""

	if channel.Org != nil {
		org = *channel.Org
	}

	f.logger.Debug("Filtered out stream from non-allowed org",
		slog.String("channel", stream.ChannelName),
		slog.String("org", org),
	)

	return false
}

func (f *StreamFilter) FilterUpcomingStreams(streams []*domain.Stream) []*domain.Stream {
	return f.FilterUpcomingStreamsInWindow(streams, time.Now(), time.Time{})
}

func isUpcomingStream(stream *domain.Stream, now time.Time) bool {
	if stream.StartActual != nil {
		return false
	}

	return stream.StartScheduled == nil || stream.StartScheduled.After(now)
}

// FilterUpcomingStreamsInWindow는 (now, until]의 예정 방송과 일정 미정 방송을 유지합니다.
// 호출자가 기준 시각을 한 번 전달하여 목록 전체의 하한과 상한에 같은 시각을 적용합니다.
// Until이 영 값이면 기존 일정 조회처럼 상한을 적용하지 않습니다.
func (f *StreamFilter) FilterUpcomingStreamsInWindow(streams []*domain.Stream, now, until time.Time) []*domain.Stream {
	filtered := make([]*domain.Stream, 0, len(streams))

	for _, stream := range streams {
		if isUpcomingStream(stream, now) && (until.IsZero() || stream.StartScheduled == nil || !stream.StartScheduled.After(until)) {
			filtered = append(filtered, stream)
		}
	}

	return filtered
}

func (f *StreamFilter) IsHolostarsChannel(channel *domain.Channel) bool {
	if channel == nil {
		return false
	}

	upper := func(s *string) string {
		if s == nil {
			return ""
		}

		return strings.ToUpper(*s)
	}

	return strings.Contains(upper(channel.Suborg), "HOLOSTARS") ||
		strings.Contains(strings.ToUpper(channel.Name), "HOLOSTARS") ||
		strings.Contains(upper(channel.EnglishName), "HOLOSTARS")
}

func isAllowedOrg(org string) bool {
	return slices.Contains(constants.HolodexAPIParams.AllowedFilterOrgs, org)
}
