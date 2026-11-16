package canonicalwrite

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func normalizeContentID(kind domain.OutboxKind, id string) string {
	normalized, err := ytcontentid.ForOutboxKind(kind, id)
	if err != nil {
		return ""
	}

	return normalized
}

func normalizeShortVideoResourceID(id string) string {
	normalized, err := ytcontentid.NormalizeShortVideoID(id)
	if err != nil {
		return ""
	}

	return normalized
}
