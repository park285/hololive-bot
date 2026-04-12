// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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

// 기본값은 "최근 10일"이다.
func ResolveStatsPeriod(now time.Time, raw string) (time.Time, string) {
	normalized := NormalizeStatsPeriodToken(raw)
	if normalized == "" {
		normalized = "days:10"
	}

	if start, label, ok := resolveNamedStatsPeriod(now, normalized); ok {
		return start, label
	}

	if start, label, ok := resolvePrefixedStatsPeriod(now, normalized); ok {
		return start, label
	}

	return now.AddDate(0, 0, -10), "최근 10일"
}

func resolveNamedStatsPeriod(now time.Time, normalized string) (time.Time, string, bool) {
	switch normalized {
	case "today":
		return now.Add(-24 * time.Hour), "오늘", true
	case "week":
		return now.AddDate(0, 0, -7), "최근 7일", true
	case "month":
		return now.AddDate(0, -1, 0), "최근 1개월", true
	case "quarter":
		return now.AddDate(0, -3, 0), "최근 1분기", true
	case "year":
		return now.AddDate(-1, 0, 0), "최근 1년", true
	default:
		return time.Time{}, "", false
	}
}

func resolvePrefixedStatsPeriod(now time.Time, normalized string) (time.Time, string, bool) {
	if start, label, ok := resolveRelativeStatsPeriod(normalized, "days:", 5, "일", func(value int) time.Time {
		return now.AddDate(0, 0, -value)
	}); ok {
		return start, label, true
	}

	if start, label, ok := resolveRelativeStatsPeriod(normalized, "weeks:", 6, "주", func(value int) time.Time {
		return now.AddDate(0, 0, -7*value)
	}); ok {
		return start, label, true
	}

	if start, label, ok := resolveRelativeStatsPeriod(normalized, "months:", 7, "개월", func(value int) time.Time {
		return now.AddDate(0, -value, 0)
	}); ok {
		return start, label, true
	}

	if start, label, ok := resolveRelativeStatsPeriod(normalized, "quarters:", 9, "분기", func(value int) time.Time {
		return now.AddDate(0, -3*value, 0)
	}); ok {
		return start, label, true
	}

	if start, label, ok := resolveRelativeStatsPeriod(normalized, "years:", 6, "년", func(value int) time.Time {
		return now.AddDate(-value, 0, 0)
	}); ok {
		return start, label, true
	}

	if start, label, ok := resolveRelativeStatsPeriod(normalized, "hours:", 6, "시간", func(value int) time.Time {
		return now.Add(-time.Duration(value) * time.Hour)
	}); ok {
		return start, label, true
	}

	return time.Time{}, "", false
}

func resolveRelativeStatsPeriod(
	normalized, prefix string,
	prefixLen int,
	unit string,
	calcStart func(value int) time.Time,
) (time.Time, string, bool) {
	if !strings.HasPrefix(normalized, prefix) {
		return time.Time{}, "", false
	}
	value, ok := parsePositiveInt(normalized[prefixLen:])
	if !ok {
		return time.Time{}, "", false
	}
	return calcStart(value), formatRelativeLabel(value, unit), true
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
