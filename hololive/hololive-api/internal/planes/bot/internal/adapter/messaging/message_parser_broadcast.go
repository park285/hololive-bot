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
	"strconv"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/stringutil"
)

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
	if len(videoID) < 6 || len(videoID) > 32 {
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

	if member := stringutil.TrimSpace(strings.Join(memberTokens, " ")); member != "" && params["member"] == nil {
		params["member"] = member
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
		params["all"] = true
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
	} else if broadcastHistoryFilterKinds[key] == "member" {
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
		params["days"] = days
		return true
	}
	if value, ok := parsePositiveInt(token); ok {
		if _, hasDays := params["days"]; hasDays || containsBroadcastHistoryDaysToken(rest) {
			params["limit"] = value
		} else {
			params["days"] = value
		}
		return true
	}
	if isBroadcastHistoryTypeToken(normalized) {
		params["type"] = token
		return true
	}
	return false
}

func containsBroadcastHistoryDaysToken(args []string) bool {
	for _, arg := range args {
		if _, ok := parseBroadcastHistoryDaysToken(arg); ok {
			return true
		}
		if key, _, ok := splitBroadcastHistoryFilter(arg); ok && broadcastHistoryFilterKinds[key] == "days" {
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
	if broadcastHistoryFilterKinds[key] != "member" {
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

func isBroadcastHistoryOptionBoundary(token string) bool {
	normalized := stringutil.Normalize(token)
	if normalized == "최근" || isAllBroadcastHistoryToken(normalized) || isBroadcastHistoryTypeToken(normalized) {
		return true
	}
	if _, _, ok := splitBroadcastHistoryFilter(token); ok {
		return true
	}
	if _, ok := parseBroadcastHistoryDaysToken(token); ok {
		return true
	}
	_, ok := parsePositiveInt(token)
	return ok
}

func isBroadcastHistoryFilterKey(key string) bool {
	_, ok := broadcastHistoryFilterKinds[key]
	return ok
}

func applyBroadcastHistoryFilter(params map[string]any, key, value string) {
	value = stringutil.TrimSpace(value)
	if value == "" {
		return
	}
	applier, ok := broadcastHistoryFilterAppliers[broadcastHistoryFilterKinds[key]]
	if ok {
		applier(params, value)
	}
}

func applyBroadcastHistoryStringFilter(params map[string]any, key, value string) {
	params[key] = value
}

func applyBroadcastHistoryDaysFilter(params map[string]any, value string) {
	if days, ok := parseBroadcastHistoryDays(value); ok {
		params["days"] = days
	}
}

func applyBroadcastHistoryLimitFilter(params map[string]any, value string) {
	if limit, ok := parsePositiveInt(value); ok {
		params["limit"] = limit
	}
}

func isBroadcastThumbnailAction(token string) bool {
	return stringutil.ContainsString([]string{"썸네일", "thumbnail", "thumb", "다운로드", "download"}, stringutil.Normalize(token))
}

func isAllBroadcastHistoryToken(token string) bool {
	return stringutil.ContainsString([]string{"전체", "전부", "모두", "all"}, token)
}

func parseBroadcastHistoryDays(token string) (int, bool) {
	normalized := strings.TrimSpace(strings.ToLower(token))
	normalized = strings.TrimSuffix(normalized, "days")
	normalized = strings.TrimSuffix(normalized, "day")
	normalized = strings.TrimSuffix(normalized, "일")
	return parsePositiveInt(normalized)
}

func parseBroadcastHistoryDaysToken(token string) (int, bool) {
	normalized := strings.TrimSpace(strings.ToLower(token))
	if !strings.HasSuffix(normalized, "days") && !strings.HasSuffix(normalized, "day") && !strings.HasSuffix(normalized, "일") {
		return 0, false
	}
	return parseBroadcastHistoryDays(normalized)
}

func parsePositiveInt(token string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(token))
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func isBroadcastHistoryTypeToken(token string) bool {
	return broadcasttype.IsAlias(token)
}

var broadcastHistoryFilterKinds = map[string]string{
	"type": "type", "타입": "type", "방송타입": "type", "종류": "type", "category": "type", "카테고리": "type", "분류": "type",
	"topic": "topic", "topic_id": "topic", "토픽": "topic",
	"days": "days", "day": "days", "일": "days", "기간": "days",
	"limit": "limit", "개수": "limit", "갯수": "limit",
	"member": "member", "멤버": "member",
}

var broadcastHistoryFilterAppliers = map[string]func(map[string]any, string){
	"type":   func(params map[string]any, value string) { applyBroadcastHistoryStringFilter(params, "type", value) },
	"topic":  func(params map[string]any, value string) { applyBroadcastHistoryStringFilter(params, "topic", value) },
	"days":   applyBroadcastHistoryDaysFilter,
	"limit":  applyBroadcastHistoryLimitFilter,
	"member": func(params map[string]any, value string) { applyBroadcastHistoryStringFilter(params, "member", value) },
}
