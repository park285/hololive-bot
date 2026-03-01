package server

import "strings"

// SplitChannelIDs는 쉼표로 구분된 channelIds 쿼리를 정규화하여 반환한다.
func SplitChannelIDs(raw string) []string {
	if raw == "" {
		return []string{}
	}

	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		channelID := strings.TrimSpace(part)
		if channelID == "" {
			continue
		}
		ids = append(ids, channelID)
	}

	return ids
}
