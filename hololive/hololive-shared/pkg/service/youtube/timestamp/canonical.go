package timestamp

import (
	htmlpkg "html"
	"strings"
	"time"
)

const Layout = time.RFC3339Nano

var publishedAtParseLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05.999999999-0700",
}

func Normalize(value time.Time) time.Time {
	return value.UTC()
}

func NormalizePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	normalized := Normalize(*value)

	return &normalized
}

func Format(value time.Time) string {
	return Normalize(value).Format(Layout)
}

func ParsePublishedAt(value string) (*time.Time, bool) {
	cleaned := strings.TrimSpace(htmlpkg.UnescapeString(value))

	cleaned = strings.Trim(cleaned, `"`)

	if cleaned == "" {
		return nil, false
	}

	for _, layout := range publishedAtParseLayouts {
		parsed, err := time.Parse(layout, cleaned)
		if err != nil {
			continue
		}

		normalized := Normalize(parsed)

		return &normalized, true
	}

	return nil, false
}
