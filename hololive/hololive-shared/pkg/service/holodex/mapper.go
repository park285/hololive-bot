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
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type StreamMapper struct {
	logger *slog.Logger
}

func NewStreamMapper(logger *slog.Logger) *StreamMapper {
	return &StreamMapper{logger: logger}
}

func (m *StreamMapper) MapStreamsResponse(rawStreams []StreamRaw) []*domain.Stream {
	streams := make([]*domain.Stream, 0, len(rawStreams))
	for _, raw := range rawStreams {
		stream := m.MapStreamResponse(&raw)
		if stream != nil {
			streams = append(streams, stream)
		}
	}
	return streams
}

func (m *StreamMapper) MapStreamResponse(raw *StreamRaw) *domain.Stream {
	stream := &domain.Stream{
		ID:          raw.ID,
		Title:       raw.Title,
		Status:      raw.Status,
		Duration:    raw.Duration,
		Thumbnail:   raw.Thumbnail,
		Link:        raw.Link,
		TopicID:     raw.TopicID,
		ViewerCount: raw.LiveViewers,
	}

	m.applyDefaultURLs(raw, stream)

	channelID, ok := resolveStreamChannelID(raw)
	if !ok {
		m.logger.Warn("Stream missing ChannelID - skipping",
			slog.String("stream_id", raw.ID),
			slog.String("title", raw.Title))
		return nil
	}
	stream.ChannelID = channelID

	if raw.Channel != nil && raw.Channel.Name != "" {
		stream.ChannelName = raw.Channel.Name
	} else {
		m.logger.Debug("Stream missing ChannelName, will use ChannelID",
			slog.String("stream_id", raw.ID),
			slog.String("channel_id", stream.ChannelID))
	}

	stream.StartScheduled = parseRFC3339Ptr(raw.StartScheduled)
	stream.StartActual = parseRFC3339Ptr(raw.StartActual)

	if raw.Channel != nil {
		stream.Channel = m.MapChannelResponse(raw.Channel)
	}

	return stream
}

func (m *StreamMapper) MapChannelsResponse(rawChannels []ChannelRaw) []*domain.Channel {
	channels := make([]*domain.Channel, len(rawChannels))
	for i, raw := range rawChannels {
		channels[i] = m.MapChannelResponse(&raw)
	}
	return channels
}

func (m *StreamMapper) MapChannelResponse(raw *ChannelRaw) *domain.Channel {
	return &domain.Channel{
		ID:              raw.ID,
		Name:            raw.Name,
		EnglishName:     raw.EnglishName,
		Photo:           raw.Photo,
		Twitter:         raw.Twitter,
		VideoCount:      raw.VideoCount,
		SubscriberCount: raw.SubscriberCount,
		Org:             raw.Org,
		Suborg:          raw.Suborg,
		Group:           raw.Group,
	}
}

func (m *StreamMapper) applyDefaultURLs(raw *StreamRaw, stream *domain.Stream) {
	if stream.Thumbnail == nil || *stream.Thumbnail == "" {
		thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", raw.ID)
		stream.Thumbnail = &thumbURL
	}

	if stream.Link == nil || *stream.Link == "" {
		linkURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", raw.ID)
		stream.Link = &linkURL
	}
}

func resolveStreamChannelID(raw *StreamRaw) (string, bool) {
	if raw.ChannelID != nil && *raw.ChannelID != "" {
		return *raw.ChannelID, true
	}
	if raw.Channel != nil && raw.Channel.ID != "" {
		return raw.Channel.ID, true
	}
	return "", false
}

func parseRFC3339Ptr(value *string) *time.Time {
	if value == nil || *value == "" {
		return nil
	}

	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil
	}
	return &parsed
}
