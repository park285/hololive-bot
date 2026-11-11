package canonicalwrite

import (
	"strconv"
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

func writeRowPlaceholders(sb *strings.Builder, rowIndex, columns int) {
	base := rowIndex * columns

	sb.WriteByte('(')

	for j := range columns {
		if j > 0 {
			sb.WriteString(", ")
		}

		sb.WriteByte('$')
		sb.WriteString(strconv.Itoa(base + j + 1))
	}

	sb.WriteByte(')')
}
