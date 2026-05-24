package batchrepo

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func normalizeContentID(kind domain.OutboxKind, id string) string {
	trimmed := strings.TrimSpace(id)
	switch kind {
	case domain.OutboxKindNewShort, domain.OutboxKindCommunityPost:
		normalized, err := ytcontentid.ForOutboxKind(kind, trimmed)
		if err != nil {
			return trimmed
		}
		return normalized
	default:
		return trimmed
	}
}

func normalizeShortVideoResourceID(id string) string {
	normalized, err := ytcontentid.NormalizeShortVideoID(id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return normalized
}
