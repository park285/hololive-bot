package domain

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

var (
	statsNumericPattern = regexp.MustCompile(`(?i)^(?:최근|last)?\s*(\d+)\s*(일|days?|d|주|weeks?|w|개월|달|months?|m|분기|quarters?|q|년|연|years?|y|시간|hours?|h)(?:\s*(?:간|동안))?$`)

	// 키워드 > 정규화된 값 매핑
	periodKeywords = map[string]string{
		"오늘": "today", "today": "today",
		"주간": "week", "week": "week", "weekly": "week",
		"월간": "month", "month": "month", "monthly": "month",
		"분기": "quarter", "quarter": "quarter", "quarterly": "quarter",
		"연간": "year", "년간": "year", "year": "year", "yearly": "year", "annual": "year", "annually": "year",
	}

	// prefix 정보
	periodPrefixes = []struct {
		prefix string
		length int
		unit   string
	}{
		{"days:", 5, "days"},
		{"weeks:", 6, "weeks"},
		{"months:", 7, "months"},
		{"quarters:", 9, "quarters"},
		{"years:", 6, "years"},
		{"hours:", 6, "hours"},
	}

	// 단위 별칭 > 정규화된 단위 매핑
	periodUnits = map[string]string{
		"일": "days", "day": "days", "days": "days", "d": "days",
		"주": "weeks", "week": "weeks", "weeks": "weeks", "w": "weeks",
		"개월": "months", "달": "months", "month": "months", "months": "months", "m": "months",
		"분기": "quarters", "quarter": "quarters", "quarters": "quarters", "q": "quarters",
		"년": "years", "연": "years", "year": "years", "years": "years", "y": "years",
		"시간": "hours", "hour": "hours", "hours": "hours", "h": "hours",
	}
)

// NormalizeStatsPeriodToken: 사용자가 입력한 기간 관련 토큰을 파싱하여 정규화된 형태(예: "days:7")로 변환합니다.
func NormalizeStatsPeriodToken(raw string) string {
	token := stringutil.TrimSpace(raw)
	if token == "" {
		return ""
	}

	lower := stringutil.Normalize(token)

	// prefix 처리
	for _, p := range periodPrefixes {
		if strings.HasPrefix(lower, p.prefix) {
			if value, ok := parsePositiveInt(lower[p.length:]); ok {
				return p.unit + ":" + strconv.Itoa(value)
			}
		}
	}

	// 키워드 매칭
	if normalized, ok := periodKeywords[lower]; ok {
		return normalized
	}

	// 숫자+단위 형식 처리
	if matches := statsNumericPattern.FindStringSubmatch(token); len(matches) == 3 {
		value, err := strconv.Atoi(matches[1])
		if err != nil || value <= 0 {
			return ""
		}

		if unit, ok := periodUnits[stringutil.Normalize(matches[2])]; ok {
			return unit + ":" + strconv.Itoa(value)
		}
	}

	return ""
}

// ResolveStatsPeriod: 정규화된 기간 토큰을 해석하여 기준 시작 시간(Calculated Start Time)과 사용자 표시용 레이블을 반환합니다.
// 기본값은 "최근 10일"이다.
func ResolveStatsPeriod(now time.Time, raw string) (time.Time, string) {
	normalized := NormalizeStatsPeriodToken(raw)
	if normalized == "" {
		normalized = "days:10"
	}

	switch normalized {
	case "today":
		return now.Add(-24 * time.Hour), "오늘"
	case "week":
		return now.AddDate(0, 0, -7), "최근 7일"
	case "month":
		return now.AddDate(0, -1, 0), "최근 1개월"
	case "quarter":
		return now.AddDate(0, -3, 0), "최근 1분기"
	case "year":
		return now.AddDate(-1, 0, 0), "최근 1년"
	}

	if strings.HasPrefix(normalized, "days:") {
		if days, ok := parsePositiveInt(normalized[5:]); ok {
			return now.AddDate(0, 0, -days), formatRelativeLabel(days, "일")
		}
	}

	if strings.HasPrefix(normalized, "weeks:") {
		if weeks, ok := parsePositiveInt(normalized[6:]); ok {
			return now.AddDate(0, 0, -7*weeks), formatRelativeLabel(weeks, "주")
		}
	}

	if strings.HasPrefix(normalized, "months:") {
		if months, ok := parsePositiveInt(normalized[7:]); ok {
			return now.AddDate(0, -months, 0), formatRelativeLabel(months, "개월")
		}
	}

	if strings.HasPrefix(normalized, "quarters:") {
		if quarters, ok := parsePositiveInt(normalized[9:]); ok {
			return now.AddDate(0, -3*quarters, 0), formatRelativeLabel(quarters, "분기")
		}
	}

	if strings.HasPrefix(normalized, "years:") {
		if years, ok := parsePositiveInt(normalized[6:]); ok {
			return now.AddDate(-years, 0, 0), formatRelativeLabel(years, "년")
		}
	}

	if strings.HasPrefix(normalized, "hours:") {
		if hours, ok := parsePositiveInt(normalized[6:]); ok {
			return now.Add(-time.Duration(hours) * time.Hour), formatRelativeLabel(hours, "시간")
		}
	}

	return now.AddDate(0, 0, -10), "최근 10일"
}

func parsePositiveInt(raw string) (int, bool) {
	value, err := strconv.Atoi(stringutil.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func formatRelativeLabel(value int, unit string) string {
	if value == 1 {
		return "최근 1" + unit
	}
	return "최근 " + strconv.Itoa(value) + unit
}
