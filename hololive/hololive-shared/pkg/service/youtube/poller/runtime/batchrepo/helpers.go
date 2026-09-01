package batchrepo

import (
	"strings"

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
