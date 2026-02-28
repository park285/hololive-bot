package poller

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// 패키지 레벨에서 한 번만 컴파일
var numericRe = regexp.MustCompile(`[\d.]+`)

// isLiveReplayVideo: 라이브 다시보기 영상인지 판별
// published_text에 "streamed", "premiered" 등의 키워드가 포함되면 true
func isLiveReplayVideo(publishedText string) bool {
	lower := strings.ToLower(publishedText)
	return strings.Contains(lower, "streamed") || strings.Contains(lower, "premiered")
}

// convertThumbnails: scraper.Thumbnail을 domain.ThumbnailsJSON으로 변환
func convertThumbnails(thumbnails []scraper.Thumbnail) domain.ThumbnailsJSON {
	if len(thumbnails) == 0 {
		return nil
	}

	result := make(domain.ThumbnailsJSON, len(thumbnails))
	for i, t := range thumbnails {
		result[i] = domain.ThumbnailEntry{
			URL:    t.URL,
			Width:  t.Width,
			Height: t.Height,
		}
	}
	return result
}

// mustMarshalJSON: JSON 마샬링 (에러 시 빈 객체)
func mustMarshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// parseViewerCount: 시청자 수 텍스트 파싱
// "12,345 watching" -> 12345
// "1.2K watching" -> 1200
func parseViewerCount(text string) int {
	if text == "" {
		return 0
	}

	// "watching", "viewers" 등 제거
	text = strings.ToLower(text)
	text = strings.ReplaceAll(text, ",", "")
	text = strings.ReplaceAll(text, " watching", "")
	text = strings.ReplaceAll(text, " viewers", "")
	text = strings.ReplaceAll(text, " waiting", "")
	text = strings.TrimSpace(text)

	// K, M 처리
	multiplier := 1.0
	if strings.HasSuffix(text, "k") {
		multiplier = 1000
		text = strings.TrimSuffix(text, "k")
	} else if strings.HasSuffix(text, "m") {
		multiplier = 1000000
		text = strings.TrimSuffix(text, "m")
	}

	// 숫자 추출
	match := numericRe.FindString(text)
	if match == "" {
		return 0
	}

	val, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0
	}

	return int(val * multiplier)
}
