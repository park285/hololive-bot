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

	if stream.Thumbnail == nil || *stream.Thumbnail == "" {
		thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/mqdefault.jpg", raw.ID)
		stream.Thumbnail = &thumbURL
	}

	if stream.Link == nil || *stream.Link == "" {
		linkURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", raw.ID)
		stream.Link = &linkURL
	}

	if raw.ChannelID != nil && *raw.ChannelID != "" {
		stream.ChannelID = *raw.ChannelID
	} else if raw.Channel != nil && raw.Channel.ID != "" {
		stream.ChannelID = raw.Channel.ID
	} else {
		m.logger.Warn("Stream missing ChannelID - skipping",
			slog.String("stream_id", raw.ID),
			slog.String("title", raw.Title))
		return nil
	}

	if raw.Channel != nil && raw.Channel.Name != "" {
		stream.ChannelName = raw.Channel.Name
	} else {
		m.logger.Debug("Stream missing ChannelName, will use ChannelID",
			slog.String("stream_id", raw.ID),
			slog.String("channel_id", stream.ChannelID))
	}

	if raw.StartScheduled != nil && *raw.StartScheduled != "" {
		if t, err := time.Parse(time.RFC3339, *raw.StartScheduled); err == nil {
			stream.StartScheduled = &t
		}
	}

	if raw.StartActual != nil && *raw.StartActual != "" {
		if t, err := time.Parse(time.RFC3339, *raw.StartActual); err == nil {
			stream.StartActual = &t
		}
	}

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
