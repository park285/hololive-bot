package handlers

import (
	"context"
	"fmt"

	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/collabmembers"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// withCollabMembers는 공식 스케줄의 콜라보 출연자 표시명을 CollaboTalentNames에 넣은 사본을 돌려준다.
// 명령 응답에서 이 필드는 공식 표기명이 아니라 호스트를 뺀 표시명이다. Holodex 응답은 캐시와 공유될 수 있어 원본을 고치지 않는다.
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

		// 공식 일정 fallback 스트림은 공식 표기명을 이미 담고 있으므로 조회 결과로 덮어 표시명만 남긴다.
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
