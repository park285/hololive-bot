package parser

import (
	"github.com/tidwall/gjson"
)

func PickLockupMetadataTexts(parts *gjson.Result) (viewCount int64, publishedText string) {
	texts := CollectLockupTexts(parts)
	if viewCount, published, ok := PickViewCountAndPublished(texts); ok {
		return viewCount, published
	}

	return FallbackPickMetadata(texts)
}

func CollectLockupTexts(parts *gjson.Result) []string {
	var texts []string

	parts.ForEach(func(_, part gjson.Result) bool {
		text := part.Get("text.content").String()
		if text != "" {
			texts = append(texts, text)
		}

		return true
	})

	return texts
}

func PickViewCountAndPublished(texts []string) (viewCount int64, publishedText string, ok bool) {
	for i, t := range texts {
		parsed := ParseViewCount(t)
		if parsed <= 0 {
			continue
		}

		return parsed, firstOtherText(texts, i), true
	}

	return 0, "", false
}

func firstOtherText(texts []string, excludeIdx int) string {
	for i, t := range texts {
		if i == excludeIdx {
			continue
		}

		return t
	}

	return ""
}

func FallbackPickMetadata(texts []string) (result1 int64, result2 string) {
	var viewText, publishedText string

	if len(texts) > 0 {
		viewText = texts[0]
	}

	if len(texts) > 1 {
		publishedText = texts[1]
	}

	return ParseViewCount(viewText), publishedText
}
