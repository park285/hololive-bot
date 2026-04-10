package streamcommon

import "github.com/kapu/hololive-shared/pkg/domain"

func FindByChannelAndScheduledMinute(streams []*domain.Stream, candidate *domain.Stream) *domain.Stream {
	if candidate == nil || candidate.StartScheduled == nil {
		return nil
	}

	candidateMinute := candidate.StartScheduled.UTC().Unix() / 60
	for _, stream := range streams {
		if stream == nil || stream.StartScheduled == nil {
			continue
		}
		if stream.ChannelID != candidate.ChannelID {
			continue
		}
		if stream.StartScheduled.UTC().Unix()/60 == candidateMinute {
			return stream
		}
	}

	return nil
}
