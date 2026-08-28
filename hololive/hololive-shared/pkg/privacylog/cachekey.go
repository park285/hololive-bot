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

// keepTrailing은 식별자 구간 뒤에 오는 고정 구간 수다. Kakao 방 제목은 ':'를 포함할 수 있어
// 앞에서 세면 구간이 밀린다. 반면 streamID/channelID/unix/fingerprint/category는 ':'를 담을 수 없으므로
// 뒤에서 세야만 방 제목 길이와 무관하게 경계가 고정된다.
type identifierKeyRule struct {
	prefix       string
	keepTrailing int
}

// alarm/keys의 room 인자를 받는 Build* 함수와 1:1로 대응한다. 대응 누락은
// alarm/keys의 room_key_redaction_test.go가 잡는다.
var identifierKeyRules = []identifierKeyRule{
	{prefix: "notified:schedule:transition:event:", keepTrailing: 4},
	{prefix: "notified:schedule:transition:room:", keepTrailing: 3},
	{prefix: "notified:schedule:index:", keepTrailing: 2},
	{prefix: "notified:upcoming:event:", keepTrailing: 3},
	{prefix: "notified:claim:event:", keepTrailing: 4},
	{prefix: "notified:claim:", keepTrailing: 3},
	{prefix: "alarm:", keepTrailing: 0},
}

var verbatimKeyPrefixes = []string{
	"alarm:channel_registry",
	"alarm:channel_registry:version",
	"alarm:channel_subscribers:",
	"alarm:channel_subscribers_empty:",
	"alarm:chzzk_channels",
	"alarm:chzzk_channels_empty",
	"alarm:dispatch:",
	"alarm:member_names",
	"alarm:next_stream:",
	"alarm:registry",
	"alarm:room_names",
	"alarm:subscriber_cache_empty",
	"alarm:twitch_channel_logins",
	"alarm:twitch_channel_logins_empty",
	"alarm:twitch_logins",
	"alarm:user_names",
}

var identifierFieldKeys = map[string]struct{}{
	"alarm:room_names":      {},
	"alarm:user_names":      {},
	"membernews:room_names": {},
}

func CacheKeyAttr(key string) slog.Attr {
	return slog.String(KeyCacheKey, RedactCacheKey(key))
}

func CacheFieldAttr(key, field string) slog.Attr {
	return slog.String(KeyCacheField, RedactCacheField(key, field))
}

func RedactCacheKey(key string) string {
	rule, redacted := matchIdentifierKeyRule(key)
	if !redacted {
		return key
	}

	return rule.prefix + pseudonymizeLeadingSegments(key[len(rule.prefix):], rule.keepTrailing)
}

func RedactCacheField(key, field string) string {
	if _, sensitive := identifierFieldKeys[strings.TrimSpace(key)]; !sensitive {
		return field
	}

	return IdentifierToken(field)
}

func matchIdentifierKeyRule(key string) (identifierKeyRule, bool) {
	matched, found := longestIdentifierKeyRule(key)
	if !found || hasVerbatimKeyPrefix(key, len(matched.prefix)) {
		return identifierKeyRule{}, false
	}

	return matched, true
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

func hasVerbatimKeyPrefix(key string, longerThan int) bool {
	for _, prefix := range verbatimKeyPrefixes {
		family := strings.HasSuffix(prefix, segmentSeparator)
		if len(prefix) > longerThan && (key == prefix || family && strings.HasPrefix(key, prefix)) {
			return true
		}
	}

	return false
}

func pseudonymizeLeadingSegments(remainder string, keepTrailing int) string {
	if keepTrailing <= 0 {
		return IdentifierToken(remainder)
	}

	segments := strings.Split(remainder, segmentSeparator)
	if len(segments) <= keepTrailing {
		return IdentifierToken(remainder)
	}

	boundary := len(segments) - keepTrailing

	return IdentifierToken(strings.Join(segments[:boundary], segmentSeparator)) +
		segmentSeparator + strings.Join(segments[boundary:], segmentSeparator)
}
