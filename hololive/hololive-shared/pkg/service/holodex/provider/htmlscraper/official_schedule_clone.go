package htmlscraper

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func cloneStreams(streams []*domain.Stream) []*domain.Stream {
	if streams == nil {
		return nil
	}
	cloned := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		cloned = append(cloned, cloneStream(stream))
	}
	return cloned
}

func cloneStream(stream *domain.Stream) *domain.Stream {
	if stream == nil {
		return nil
	}
	cloned := *stream
	cloned.StartScheduled = cloneTimePtr(stream.StartScheduled)
	cloned.StartActual = cloneTimePtr(stream.StartActual)
	cloned.Duration = cloneIntPtr(stream.Duration)
	cloned.Thumbnail = cloneStringPtr(stream.Thumbnail)
	cloned.Link = cloneStringPtr(stream.Link)
	cloned.TopicID = cloneStringPtr(stream.TopicID)
	cloned.Channel = cloneChannelPtr(stream.Channel)
	cloned.ViewerCount = cloneIntPtr(stream.ViewerCount)
	cloned.CollaboTalentNames = cloneStringSlice(stream.CollaboTalentNames)
	return &cloned
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelPtr(value *domain.Channel) *domain.Channel {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.EnglishName = cloneStringPtr(value.EnglishName)
	cloned.Photo = cloneStringPtr(value.Photo)
	cloned.Twitter = cloneStringPtr(value.Twitter)
	cloned.VideoCount = cloneIntPtr(value.VideoCount)
	cloned.SubscriberCount = cloneIntPtr(value.SubscriberCount)
	cloned.Org = cloneStringPtr(value.Org)
	cloned.Suborg = cloneStringPtr(value.Suborg)
	cloned.Group = cloneStringPtr(value.Group)
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
