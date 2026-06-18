package workerapp

import "strings"

func cleanCommunityOutboxTitle(value string) string {
	value = strings.NewReplacer(`\r\n`, "\n", `\n`, "\n", `\r`, "\n").Replace(value)
	lines := make([]string, 0, 4)
	for line := range strings.SplitSeq(value, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isCommunityOutboxDecorationLine(trimmed) {
			continue
		}
		lines = append(lines, trimmed)
		if len(lines) == 4 {
			break
		}
	}
	return strings.Join(lines, " ")
}

func isCommunityOutboxDecorationLine(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}
	for _, r := range trimmed {
		switch r {
		case '/', '\\', '／', '＼', '|', '｜', '-', '─', '━', 'ー', '―', '＝', '=', '*', '＊', '·', '・', ' ':
			continue
		default:
			return false
		}
	}
	return true
}

func communityOutboxThumbnailURL(data *alarmDispatchKaringCommunityPayload) string {
	if data == nil {
		return ""
	}
	if thumbnail := bestKaringImageURL(data.Images); thumbnail != "" {
		return thumbnail
	}
	return bestKaringImageURL(data.AuthorPhoto)
}

func bestKaringImageURL(images []alarmDispatchKaringImagePayload) string {
	bestURL := ""
	bestArea := -1
	for _, image := range images {
		url := normalizeKaringImageURL(image.URL)
		if url == "" {
			continue
		}
		area := image.Width * image.Height
		if area > bestArea {
			bestURL = url
			bestArea = area
		}
	}
	return bestURL
}

func normalizeKaringImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return "https:" + trimmed
	}
	return trimmed
}
