package streamcommon

import "github.com/kapu/hololive-shared/pkg/domain"

func FindByChannelAndScheduledMinute(streams []*domain.Stream, candidate *domain.Stream) *domain.Stream {
	if candidate == nil || candidate.StartScheduled == nil {
		return nil
	}

	candidateMinute := scheduledMinute(candidate)
	for _, stream := range streams {
		if matchesChannelAndScheduledMinute(stream, candidate.ChannelID, candidateMinute) {
			return stream
		}
	}

	return nil
}

func matchesChannelAndScheduledMinute(stream *domain.Stream, channelID string, minute int64) bool {
	if stream == nil || stream.StartScheduled == nil {
		return false
	}
	if stream.ChannelID != channelID {
		return false
	}
	return scheduledMinute(stream) == minute
}

func scheduledMinute(stream *domain.Stream) int64 {
	return stream.StartScheduled.UTC().Unix() / 60
}
