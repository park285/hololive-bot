package timestamp

import (
	htmlpkg "html"
	"strings"
	"time"
)

type Field string

const (
	FieldPublishedAt Field = "published_at"
	FieldSentAt      Field = "sent_at"
)

type Rule struct {
	Field   Field
	Meaning string
}

type Policy struct {
	Location    *time.Location
	Layout      string
	PublishedAt Rule
	SentAt      Rule
}

var Canonical = Policy{
	Location: time.UTC,
	Layout:   time.RFC3339Nano,
	PublishedAt: Rule{
		Field:   FieldPublishedAt,
		Meaning: "actual_youtube_publish_time",
	},
	SentAt: Rule{
		Field:   FieldSentAt,
		Meaning: "first_successful_alarm_dispatch_time",
	},
}

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
	return Normalize(value).Format(Canonical.Layout)
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
