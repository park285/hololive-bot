// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package holodex

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
		if stream.Channel == nil {
			f.logger.Debug("Filtered out stream without channel info", slog.String("id", stream.ID))
			continue
		}

		channel := stream.Channel

		if channel.Org == nil || !isAllowedOrg(*channel.Org) {
			org := ""
			if channel.Org != nil {
				org = *channel.Org
			}
			f.logger.Debug("Filtered out stream from non-allowed org",
				slog.String("channel", stream.ChannelName),
				slog.String("org", org),
			)
			continue
		}

		if f.IsHolostarsChannel(channel) {
			f.logger.Debug("Filtered out HOLOSTARS stream", slog.String("channel", stream.ChannelName))
			continue
		}

		filtered = append(filtered, stream)
	}

	return filtered
}

func (f *StreamFilter) FilterUpcomingStreams(streams []*domain.Stream) []*domain.Stream {
	now := time.Now()
	filtered := make([]*domain.Stream, 0, len(streams))

	for _, stream := range streams {
		if stream.StartActual != nil {
			continue
		}

		if stream.StartScheduled != nil && stream.StartScheduled.After(now) {
			filtered = append(filtered, stream)
		} else if stream.StartScheduled == nil {
			filtered = append(filtered, stream)
		}
	}

	return filtered
}

// IsHolostarsChannel: 채널이 HOLOSTARS 소속인지 확인합니다.
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
