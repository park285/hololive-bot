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

	"github.com/park285/shared-go/pkg/stringutil"
)

const (
	statsPeriodWeek    = "week"
	statsPeriodMonth   = "month"
	statsPeriodQuarter = "quarter"
	statsPeriodYear    = "year"

	statsUnitDays     = "days"
	statsUnitWeeks    = "weeks"
	statsUnitMonths   = "months"
	statsUnitQuarters = "quarters"
	statsUnitYears    = "years"
	statsUnitHours    = "hours"
)

var (
	statsNumericPattern = regexp.MustCompile(`(?i)^(?:최근|last)?\s*(\d+)\s*(일|days?|d|주|weeks?|w|개월|달|months?|m|분기|quarters?|q|년|연|years?|y|시간|hours?|h)(?:\s*(?:간|동안))?$`)

	// 키워드 > 정규화된 값 매핑
	periodKeywords = map[string]string{
		"오늘": "today", "today": "today",
		"주간": statsPeriodWeek, "week": statsPeriodWeek, "weekly": statsPeriodWeek,
		"월간": statsPeriodMonth, "month": statsPeriodMonth, "monthly": statsPeriodMonth,
		"분기": statsPeriodQuarter, "quarter": statsPeriodQuarter, "quarterly": statsPeriodQuarter,
		"연간": statsPeriodYear, "년간": statsPeriodYear, "year": statsPeriodYear, "yearly": statsPeriodYear, "annual": statsPeriodYear, "annually": statsPeriodYear,
	}

	// prefix 정보
	periodPrefixes = []struct {
		prefix string
		length int
		unit   string
	}{
		{"days:", 5, statsUnitDays},
		{"weeks:", 6, statsUnitWeeks},
		{"months:", 7, statsUnitMonths},
		{"quarters:", 9, statsUnitQuarters},
		{"years:", 6, statsUnitYears},
		{"hours:", 6, statsUnitHours},
	}

	// 단위 별칭 > 정규화된 단위 매핑
	periodUnits = map[string]string{
		"일": statsUnitDays, "day": statsUnitDays, "days": statsUnitDays, "d": statsUnitDays,
		"주": statsUnitWeeks, "week": statsUnitWeeks, "weeks": statsUnitWeeks, "w": statsUnitWeeks,
		"개월": statsUnitMonths, "달": statsUnitMonths, "month": statsUnitMonths, "months": statsUnitMonths, "m": statsUnitMonths,
		"분기": statsUnitQuarters, "quarter": statsUnitQuarters, "quarters": statsUnitQuarters, "q": statsUnitQuarters,
		"년": statsUnitYears, "연": statsUnitYears, "year": statsUnitYears, "years": statsUnitYears, "y": statsUnitYears,
		"시간": statsUnitHours, "hour": statsUnitHours, "hours": statsUnitHours, "h": statsUnitHours,
	}

	namedStatsPeriods = map[string]namedStatsPeriod{
		"today":            {label: "오늘", start: func(now time.Time) time.Time { return now.Add(-24 * time.Hour) }},
		statsPeriodWeek:    {label: "최근 7일", start: func(now time.Time) time.Time { return now.AddDate(0, 0, -7) }},
		statsPeriodMonth:   {label: "최근 1개월", start: func(now time.Time) time.Time { return now.AddDate(0, -1, 0) }},
		statsPeriodQuarter: {label: "최근 1분기", start: func(now time.Time) time.Time { return now.AddDate(0, -3, 0) }},
		statsPeriodYear:    {label: "최근 1년", start: func(now time.Time) time.Time { return now.AddDate(-1, 0, 0) }},
	}
)

type namedStatsPeriod struct {
	label string
	start func(now time.Time) time.Time
}

func NormalizeStatsPeriodToken(raw string) string {
	token := stringutil.TrimSpace(raw)
	if token == "" {
		return ""
	}

	lower := stringutil.Normalize(token)

	if normalized := normalizePrefixedStatsPeriodToken(lower); normalized != "" {
		return normalized
	}

	if normalized, ok := periodKeywords[lower]; ok {
		return normalized
	}

	return normalizeNumericStatsPeriodToken(token)
}

func normalizePrefixedStatsPeriodToken(lower string) string {
	for _, p := range periodPrefixes {
		if normalized := normalizeStatsPeriodPrefix(lower, p.prefix, p.length, p.unit); normalized != "" {
			return normalized
		}
	}
	return ""
}

func normalizeStatsPeriodPrefix(lower, prefix string, length int, unit string) string {
	if !strings.HasPrefix(lower, prefix) {
		return ""
	}
	value, ok := parsePositiveInt(lower[length:])
	if !ok {
		return ""
	}
	return unit + ":" + strconv.Itoa(value)
}

func normalizeNumericStatsPeriodToken(token string) string {
	matches := statsNumericPattern.FindStringSubmatch(token)
	if len(matches) != 3 {
		return ""
	}
	value, ok := parsePositiveInt(matches[1])
	if !ok {
		return ""
	}
	unit, ok := periodUnits[stringutil.Normalize(matches[2])]
	if !ok {
		return ""
	}
	return unit + ":" + strconv.Itoa(value)
}

// 기본값은 "최근 10일"이다.
func ResolveStatsPeriod(now time.Time, raw string) (start time.Time, label string) {
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
	period, ok := namedStatsPeriods[normalized]
	if !ok {
		return time.Time{}, "", false
	}
	return period.start(now), period.label, true
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
