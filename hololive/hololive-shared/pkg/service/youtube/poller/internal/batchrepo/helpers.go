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
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return trimmed
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

func normalizeCommunityPostResourceID(id string) string {
	normalized, err := ytcontentid.NormalizeCommunityPostID(id)
	if err != nil {
		return strings.TrimSpace(id)
	}
	return normalized
}

func appendValuesPlaceholders(sb *strings.Builder, rowCount, columnCount int) {
	for i := range rowCount {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteByte('(')
		for j := range columnCount {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteByte('?')
		}
		sb.WriteByte(')')
	}
}
