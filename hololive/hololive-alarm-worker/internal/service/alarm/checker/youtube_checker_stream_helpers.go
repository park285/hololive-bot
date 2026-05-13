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

package checker

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func resolveLiveStart(stream *domain.Stream) *time.Time {
	if stream == nil {
		return nil
	}

	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		start := stream.StartActual.UTC()
		return &start
	}

	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		start := stream.StartScheduled.UTC()
		return &start
	}

	return nil
}

func groupStreamsByChannel(streams []*domain.Stream) map[string][]*domain.Stream {
	grouped := make(map[string][]*domain.Stream)

	for _, stream := range streams {
		if stream == nil {
			continue
		}

		channelID := stream.ChannelID
		if channelID == "" && stream.Channel != nil {
			channelID = stream.Channel.ID
		}

		if channelID == "" {
			continue
		}

		grouped[channelID] = append(grouped[channelID], stream)
	}

	return grouped
}

func youtubeStreamChannelID(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}
	if stream.ChannelID != "" {
		return stream.ChannelID
	}
	if stream.Channel != nil {
		return stream.Channel.ID
	}
	return ""
}
