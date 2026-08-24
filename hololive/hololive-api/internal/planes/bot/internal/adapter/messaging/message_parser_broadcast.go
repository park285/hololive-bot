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

package messaging

import (
	"net/url"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const maxParsedBroadcastHistoryDays = 365

func (ma *MessageAdapter) tryBroadcastHistoryCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if ma.isBroadcastThumbnailCommand(command) {
		return broadcastThumbnailCommand(args, raw), true
	}

	if !ma.isBroadcastHistoryCommand(command) {
		return nil, false
	}

	if len(args) > 0 && isBroadcastThumbnailAction(args[0]) {
		return broadcastThumbnailCommand(args[1:], raw), true
	}

	return &ParsedCommand{Type: domain.CommandBroadcastHistory, Params: parseBroadcastHistoryArgs(args), RawMessage: raw}, true
}

func (ma *MessageAdapter) isBroadcastHistoryCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"방송이력", "방송기록", "종료방송", "이전방송", "history", "broadcast_history"}, cmd)
}

func (ma *MessageAdapter) isBroadcastThumbnailCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"방송썸네일", "썸네일", "썸네일다운", "썸네일다운로드", "thumbnail", "thumbnail_download", "thumbnaildownload", "broadcast_thumbnail"}, cmd)
}

func broadcastThumbnailCommand(args []string, raw string) *ParsedCommand {
	params := make(map[string]any)

	if len(args) > 0 {
		params["video_id"] = parseBroadcastThumbnailVideoID(args[0])
	}

	return &ParsedCommand{Type: domain.CommandBroadcastThumbnail, Params: params, RawMessage: raw}
}

func parseBroadcastThumbnailVideoID(raw string) string {
	value := strings.TrimSpace(raw)
	if videoID, ok := youtubeVideoIDFromURL(value); ok {
		return videoID
	}

	return value
}

func youtubeVideoIDFromURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", false
	}

	host := strings.TrimPrefix(strings.ToLower(parsed.Hostname()), "www.")
	switch host {
	case "youtube.com", "m.youtube.com", "music.youtube.com":
		return youtubeVideoIDFromYouTubeURL(parsed)
	case "youtu.be":
		return firstYouTubePathVideoID(parsed)
	default:
		return "", false
	}
}

func youtubeVideoIDFromYouTubeURL(parsed *url.URL) (string, bool) {
	if parsed == nil {
		return "", false
	}

	if strings.Trim(parsed.Path, "/") == "watch" {
		return cleanYouTubeVideoID(parsed.Query().Get("v"))
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", false
	}

	switch parts[0] {
	case "shorts", "live", "embed", "v":
		return cleanYouTubeVideoID(parts[1])
	default:
		return "", false
	}
}

func firstYouTubePathVideoID(parsed *url.URL) (string, bool) {
	if parsed == nil {
		return "", false
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 {
		return "", false
	}

	return cleanYouTubeVideoID(parts[0])
}

func cleanYouTubeVideoID(raw string) (string, bool) {
	videoID := strings.TrimSpace(raw)
	if !looksLikeYouTubeVideoID(videoID) {
		return "", false
	}

	return videoID, true
}

const youtubeVideoIDChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

func looksLikeYouTubeVideoID(videoID string) bool {
	if len(videoID) != 11 {
		return false
	}

	for _, r := range videoID {
		if !strings.ContainsRune(youtubeVideoIDChars, r) {
			return false
		}
	}

	return true
}

func parseBroadcastHistoryArgs(args []string) map[string]any {
	params := make(map[string]any)
	memberTokens := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		consumed := applyBroadcastHistoryArg(params, &memberTokens, args, i)

		i += consumed
	}

	if member := stringutil.TrimSpace(strings.Join(memberTokens, " ")); member != "" && params[paramMember] == nil {
		params[paramMember] = member
	}

	return params
}

func applyBroadcastHistoryArg(params map[string]any, memberTokens *[]string, args []string, index int) int {
	token := stringutil.TrimSpace(args[index])
	if token == "" {
		return 0
	}

	normalized := stringutil.Normalize(token)
	if applyBroadcastHistorySimpleToken(params, token, normalized) {
		return 0
	}

	if consumed, ok := applyBroadcastHistoryFilterArg(params, token, args[index+1:]); ok {
		return consumed
	}

	if applyBroadcastHistoryValueToken(params, token, normalized, args[index+1:]) {
		return 0
	}

	*memberTokens = append(*memberTokens, token)

	return 0
}

func applyBroadcastHistorySimpleToken(params map[string]any, _, normalized string) bool {
	if normalized == "최근" {
		return true
	}

	if isAllBroadcastHistoryToken(normalized) {
		params[paramDays] = maxParsedBroadcastHistoryDays
		return true
	}

	return false
}

func applyBroadcastHistoryFilterArg(params map[string]any, token string, rest []string) (int, bool) {
	key, value, ok := splitBroadcastHistoryFilter(token)
	if !ok {
		return 0, false
	}

	consumed := 0

	if value == "" {
		value, consumed = consumeBroadcastHistoryFilterValue(key, rest)
	} else if broadcastHistoryFilterKinds[key] == paramMember {
		var tail string

		tail, consumed = consumeMemberBroadcastHistoryFilterValue(rest)

		if tail != "" {
			value += " " + tail
		}
	}

	applyBroadcastHistoryFilter(params, key, value)

	return consumed, true
}

func applyBroadcastHistoryValueToken(params map[string]any, token, normalized string, rest []string) bool {
	if days, ok := parseBroadcastHistoryDaysToken(token); ok {
		params[paramDays] = days
		return true
	}

	if value, ok := parsePositiveInt(token); ok {
		if _, hasDays := params[paramDays]; hasDays || containsBroadcastHistoryDaysToken(rest) {
			params[paramLimit] = value
		} else {
			params[paramDays] = value
		}

		return true
	}

	if isBroadcastHistoryTypeToken(normalized) {
		params[paramType] = token
		return true
	}

	return false
}

func containsBroadcastHistoryDaysToken(args []string) bool {
	for _, arg := range args {
		if _, ok := parseBroadcastHistoryDaysToken(arg); ok {
			return true
		}

		if key, _, ok := splitBroadcastHistoryFilter(arg); ok && broadcastHistoryFilterKinds[key] == paramDays {
			return true
		}
	}

	return false
}

func splitBroadcastHistoryFilter(token string) (key, value string, ok bool) {
	before, after, found := strings.Cut(token, ":")
	if !found {
		before, after, found = strings.Cut(token, "=")
	}

	if !found {
		return "", "", false
	}

	key = stringutil.Normalize(before)
	if !isBroadcastHistoryFilterKey(key) {
		return "", "", false
	}

	value = stringutil.TrimSpace(after)

	return key, value, true
}

func consumeBroadcastHistoryFilterValue(key string, args []string) (value string, consumed int) {
	if broadcastHistoryFilterKinds[key] != paramMember {
		return consumeSingleBroadcastHistoryFilterValue(args)
	}

	return consumeMemberBroadcastHistoryFilterValue(args)
}

func consumeSingleBroadcastHistoryFilterValue(args []string) (value string, consumed int) {
	for i, arg := range args {
		value := stringutil.TrimSpace(arg)
		if value != "" {
			return value, i + 1
		}
	}

	return "", len(args)
}

func consumeMemberBroadcastHistoryFilterValue(args []string) (value string, consumed int) {
	values := make([]string, 0, len(args))
	for _, arg := range args {
		value := stringutil.TrimSpace(arg)
		if value == "" {
			consumed++
			continue
		}

		if isBroadcastHistoryOptionBoundary(value) {
			break
		}

		values = append(values, value)
		consumed++
	}

	return strings.Join(values, " "), consumed
}
