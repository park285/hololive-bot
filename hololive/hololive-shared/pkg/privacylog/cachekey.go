package privacylog

import (
	"log/slog"
	"strings"
)

const (
	KeyCacheKey   = "key"
	KeyCacheField = "field"

	segmentSeparator = ":"
)

type identifierKeyRule struct {
	prefix       string
	keepTrailing int
}

var identifierKeyRules = []identifierKeyRule{
	{prefix: "notified:schedule:transition:event:", keepTrailing: 4},
	{prefix: "notified:schedule:transition:room:", keepTrailing: 3},
	{prefix: "notified:schedule:index:", keepTrailing: 2},
	{prefix: "notified:upcoming:event:", keepTrailing: 3},
	{prefix: "notified:claim:event:", keepTrailing: 4},
	{prefix: "notified:claim:", keepTrailing: 3},
}

func CacheKeyAttr(key string) slog.Attr {
	return slog.String(KeyCacheKey, RedactCacheKey(key))
}

func CacheFieldAttr(field string) slog.Attr {
	return slog.String(KeyCacheField, field)
}

func RedactCacheKey(key string) string {
	rule, redacted := longestIdentifierKeyRule(key)
	if !redacted {
		return key
	}

	return rule.prefix + pseudonymizeLeadingSegments(key[len(rule.prefix):], rule.keepTrailing)
}

func longestIdentifierKeyRule(key string) (identifierKeyRule, bool) {
	var matched identifierKeyRule

	found := false

	for _, rule := range identifierKeyRules {
		if len(rule.prefix) > len(matched.prefix) && strings.HasPrefix(key, rule.prefix) {
			matched, found = rule, true
		}
	}

	return matched, found
}

func pseudonymizeLeadingSegments(remainder string, keepTrailing int) string {
	segments := strings.Split(remainder, segmentSeparator)
	if len(segments) <= keepTrailing {
		return IdentifierToken(remainder)
	}

	boundary := len(segments) - keepTrailing

	return IdentifierToken(strings.Join(segments[:boundary], segmentSeparator)) +
		segmentSeparator + strings.Join(segments[boundary:], segmentSeparator)
}
