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

package template

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

func formatNumberKR(v any) string {
	n, ok := toInt64(v)
	if !ok {
		return fmt.Sprintf("%v", v)
	}

	return formatNumberKRInt64(n)
}

func formatNumberKRInt64(n int64) string {
	if n < 0 {
		return "-" + formatNumberKRInt64(-n)
	}

	switch {
	case n >= 100_000_000:
		return fmt.Sprintf("%.1f억", float64(n)/100_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1f만", float64(n)/10_000)
	case n >= 1000:
		return fmt.Sprintf("%.1f천", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < 7*24*time.Hour {
		return formatRecentDuration(d)
	}

	return t.Format(time.DateOnly)
}

func formatRecentDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "방금 전"
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	default:
		return fmt.Sprintf("%d일 전", int(d.Hours()/24))
	}
}

func formatDate(layout string, t time.Time) string {
	return t.Format(layout)
}

func defaultValue(def, val string) string {
	if val == "" {
		return def
	}

	return val
}

func nl2br(s string) string {
	return strings.ReplaceAll(s, "\n", "<br>")
}

func stripTags(s string) string {
	var result strings.Builder

	inTag := false

	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}

		if r == '>' {
			inTag = false
			continue
		}

		if !inTag {
			result.WriteRune(r)
		}
	}

	return result.String()
}

func urlEncode(s string) string {
	return url.QueryEscape(s)
}

func toTitle(s string) string {
	if s == "" {
		return ""
	}

	firstRune, size := utf8.DecodeRuneInString(s)
	if firstRune == utf8.RuneError {
		return s
	}

	return strings.ToUpper(string(firstRune)) + s[size:]
}
