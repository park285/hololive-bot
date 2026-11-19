package handlers

import (
	"context"
	"fmt"

	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func withCollabMembers(ctx context.Context, reader handlercore.CollabMemberReader, streams []*domain.Stream) ([]*domain.Stream, error) {
	videos := make([]collabmembers.Video, 0, len(streams))
	for _, stream := range streams {
		if stream != nil {
			videos = append(videos, collabmembers.Video{ID: stream.ID, HostChannelID: stream.ChannelID})
		}
	}

	names, err := reader.DisplayNames(ctx, videos)
	if err != nil {
		return nil, fmt.Errorf("resolve collab members: %w", err)
	}

	out := make([]*domain.Stream, len(streams))
	for i, stream := range streams {
		out[i] = stream
		if stream == nil {
			continue
		}

		collab := names[stream.ID]
		if len(collab) == 0 && len(stream.CollaboTalentNames) == 0 {
			continue
		}

		copied := *stream

		copied.CollaboTalentNames = collab
		out[i] = &copied
	}

	return out, nil
}
