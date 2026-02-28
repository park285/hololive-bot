package scraper

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/tidwall/gjson"
)

// ytInitialData 추출용 정규식 (패턴별 우선순위)
var ytInitialDataPatterns = []*regexp.Regexp{
	// 패턴 1: 기본 패턴 (가장 일반적)
	regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.+?\})\s*;\s*</script>`),
	// 패턴 2: 세미콜론 없이 닫는 경우
	regexp.MustCompile(`(?s)var\s+ytInitialData\s*=\s*(\{.+?\})\s*</script>`),
	// 패턴 3: window['ytInitialData'] 형식
	regexp.MustCompile(`(?s)window\["ytInitialData"\]\s*=\s*(\{.+?\})\s*;`),
	// 패턴 4: 레거시 호환 (원본 패턴)
	regexp.MustCompile(`(?s)var ytInitialData = (.+?);</script>`),
}

var ytInitialDataAnchors = []string{
	"var ytInitialData",
	`window["ytInitialData"]`,
	`window['ytInitialData']`,
}

// ErrYtInitialDataNotFound: HTML에서 ytInitialData를 찾을 수 없는 경우
var ErrYtInitialDataNotFound = errors.New("ytInitialData not found in HTML")

// extractYtInitialData: YouTube HTML에서 ytInitialData JSON을 추출
// 여러 패턴을 시도하여 YouTube 구조 변경에 유연하게 대응
func extractYtInitialData(html string) (string, error) {
	candidates := collectYtInitialDataCandidates(html)
	if best, ok := pickBestYtInitialDataCandidate(candidates); ok {
		return best, nil
	}

	return "", ErrYtInitialDataNotFound
}

func collectYtInitialDataCandidates(html string) []string {
	candidates := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)

	appendCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, exists := seen[candidate]; exists {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, anchor := range ytInitialDataAnchors {
		searchFrom := 0
		for searchFrom < len(html) {
			idx := strings.Index(html[searchFrom:], anchor)
			if idx < 0 {
				break
			}
			idx += searchFrom

			eqOffset := strings.Index(html[idx:], "=")
			if eqOffset < 0 {
				searchFrom = idx + len(anchor)
				continue
			}
			eqIdx := idx + eqOffset
			if eqIdx+1 >= len(html) {
				break
			}

			objOffset := strings.Index(html[eqIdx+1:], "{")
			if objOffset < 0 {
				searchFrom = eqIdx + 1
				continue
			}
			objStart := eqIdx + 1 + objOffset
			objEnd, ok := findJSONObjectEnd(html, objStart)
			if !ok {
				searchFrom = objStart + 1
				continue
			}

			appendCandidate(html[objStart : objEnd+1])
			searchFrom = objEnd + 1
		}
	}

	for _, pattern := range ytInitialDataPatterns {
		matches := pattern.FindAllStringSubmatch(html, -1)
		if len(matches) == 0 {
			continue
		}
		for _, match := range matches {
			if len(match) < 2 {
				continue
			}
			appendCandidate(match[1])
		}
	}

	return candidates
}

func findJSONObjectEnd(src string, start int) (int, bool) {
	if start < 0 || start >= len(src) || src[start] != '{' {
		return 0, false
	}

	depth := 0
	inString := false
	escaped := false
	var quote byte

	for i := start; i < len(src); i++ {
		ch := src[i]

		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				inString = false
			}
			continue
		}

		if ch == '"' || ch == '\'' {
			inString = true
			quote = ch
			continue
		}

		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}

	return 0, false
}

func pickBestYtInitialDataCandidate(candidates []string) (string, bool) {
	best := ""
	bestScore := -1_000_000_000

	for _, candidate := range candidates {
		score := scoreYtInitialDataCandidate(candidate)
		if score > bestScore || (score == bestScore && len(candidate) > len(best)) {
			bestScore = score
			best = candidate
		}
	}

	if best == "" {
		return "", false
	}
	return best, true
}

func scoreYtInitialDataCandidate(candidate string) int {
	if !json.Valid([]byte(candidate)) {
		return -1_000_000
	}

	data := gjson.Parse(candidate)
	score := 0

	if data.Get("contents").Exists() {
		score += 1000
	}
	if data.Get("contents.twoColumnBrowseResultsRenderer.tabs").Exists() {
		score += 4000
	}
	if data.Get("header").Exists() {
		score += 300
	}
	if data.Get("metadata").Exists() {
		score += 200
	}
	if data.Get("onResponseReceivedEndpoints").Exists() {
		score += 300
	}
	if data.Get("alerts").Exists() {
		score += 50
	}
	if data.Get("responseContext").Exists() {
		score += 10
	}

	score += min(len(candidate), 1_000_000) / 1000
	return score
}

// parseShortNumber: "2.76M", "1.5K", "500" 등을 정수로 변환
func parseShortNumber(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" || text == "No" {
		return 0
	}

	multiplier := int64(1)
	switch {
	case strings.HasSuffix(text, "K"):
		multiplier = 1_000
		text = strings.TrimSuffix(text, "K")
	case strings.HasSuffix(text, "M"):
		multiplier = 1_000_000
		text = strings.TrimSuffix(text, "M")
	case strings.HasSuffix(text, "B"):
		multiplier = 1_000_000_000
		text = strings.TrimSuffix(text, "B")
	}

	// 쉼표 제거
	text = strings.ReplaceAll(text, ",", "")

	val, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0
	}

	return int64(val * float64(multiplier))
}

// parseSubscriberCount: "2.76M subscribers"를 2760000으로 변환
func parseSubscriberCount(text string) int64 {
	text = strings.TrimSuffix(text, " subscribers")
	text = strings.TrimSuffix(text, " subscriber")
	return parseShortNumber(text)
}

// parseViewCount: "1,056,229,686 views"를 정수로 변환
func parseViewCount(text string) int64 {
	text = strings.TrimSuffix(text, " views")
	text = strings.TrimSuffix(text, " view")
	text = strings.ReplaceAll(text, ",", "")
	val, _ := strconv.ParseInt(text, 10, 64)
	return val
}

// parseVideoCount: "2,429 videos"를 정수로 변환
func parseVideoCount(text string) int64 {
	text = strings.TrimSuffix(text, " videos")
	text = strings.TrimSuffix(text, " video")
	text = strings.ReplaceAll(text, ",", "")
	val, _ := strconv.ParseInt(text, 10, 64)
	return val
}

// parseJoinedDate: "Joined Jul 2, 2019"를 Unix timestamp로 변환
func parseJoinedDate(text string) int64 {
	text = strings.TrimPrefix(text, "Joined ")
	if text == "" {
		return 0
	}

	// 다양한 날짜 형식 시도
	formats := []string{
		"Jan 2, 2006",
		"January 2, 2006",
		"2 Jan 2006",
		"2006-01-02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, text); err == nil {
			return t.Unix()
		}
	}

	return 0
}
