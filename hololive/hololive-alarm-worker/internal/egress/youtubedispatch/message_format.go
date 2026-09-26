package youtubedispatch

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/format"
)

func FormatYouTubeOutboxPayload(ctx context.Context, renderer *template.Renderer, messageStrings *messagestrings.Store, seeMoreFold bool, payload *domain.YouTubeOutboxDispatchPayload) (string, error) {
	msg, err := format.FormatYouTubeOutboxPayload(ctx, renderer, messageStrings, seeMoreFold, payload)
	if err != nil {
		return "", fmt.Errorf("format youtube outbox payload: %w", err)
	}

	return msg, nil
}
