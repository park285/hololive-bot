package messaging

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
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

	return &ParsedCommand{Type: botcommand.CommandBroadcastHistory, Params: parseBroadcastHistoryArgs(args), RawMessage: raw}, true
}

var broadcastHistoryCommandAliases = []string{"방송이력", "방송기록", "종료방송", "이전방송", "history", "broadcast_history"}

func (ma *MessageAdapter) isBroadcastHistoryCommand(cmd string) bool {
	return slices.Contains(broadcastHistoryCommandAliases, cmd)
}

var broadcastTimeCommandAliases = []string{"방송시간", "broadcast_time"}

func (ma *MessageAdapter) tryBroadcastTimeCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !slices.Contains(broadcastTimeCommandAliases, command) {
		return nil, false
	}

	var params botcommand.BroadcastTimeParams

	memberTokens := make([]string, 0, len(args))

	for _, arg := range args {
		if days, ok := parseBroadcastHistoryDaysToken(arg); ok {
			params.Days = days

			continue
		}

		memberTokens = append(memberTokens, arg)
	}

	params.Member = strings.TrimSpace(strings.Join(memberTokens, " "))

	return &ParsedCommand{Type: botcommand.CommandBroadcastTime, Params: params, RawMessage: raw}, true
}

var broadcastThumbnailCommandAliases = []string{"방송썸네일", "썸네일", "썸네일다운", "썸네일다운로드", "thumbnail", "thumbnail_download", "thumbnaildownload", "broadcast_thumbnail"}

func (ma *MessageAdapter) isBroadcastThumbnailCommand(cmd string) bool {
	return slices.Contains(broadcastThumbnailCommandAliases, cmd)
}

func broadcastThumbnailCommand(args []string, raw string) *ParsedCommand {
	var params botcommand.BroadcastThumbnailParams

	if len(args) > 0 {
		params.VideoID = parseBroadcastThumbnailVideoID(args[0])
	}

	return &ParsedCommand{Type: botcommand.CommandBroadcastThumbnail, Params: params, RawMessage: raw}
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

func parseBroadcastHistoryArgs(args []string) botcommand.BroadcastHistoryParams {
	var params botcommand.BroadcastHistoryParams

	memberTokens := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		consumed := applyBroadcastHistoryArg(&params, &memberTokens, args, i)

		i += consumed
	}

	if params.Member == "" {
		params.Member = strings.TrimSpace(strings.Join(memberTokens, " "))
	}

	return params
}

func applyBroadcastHistoryArg(params *botcommand.BroadcastHistoryParams, memberTokens *[]string, args []string, index int) int {
	token := strings.TrimSpace(args[index])
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

func applyBroadcastHistorySimpleToken(params *botcommand.BroadcastHistoryParams, _, normalized string) bool {
	if normalized == "최근" {
		return true
	}

	if isAllBroadcastHistoryToken(normalized) {
		params.Days = maxParsedBroadcastHistoryDays
		return true
	}

	return false
}

func applyBroadcastHistoryFilterArg(params *botcommand.BroadcastHistoryParams, token string, rest []string) (int, bool) {
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

func applyBroadcastHistoryValueToken(params *botcommand.BroadcastHistoryParams, token, normalized string, rest []string) bool {
	if days, ok := parseBroadcastHistoryDaysToken(token); ok {
		params.Days = days
		return true
	}

	if value, ok := parsePositiveInt(token); ok {
		if params.Days != 0 || containsBroadcastHistoryDaysToken(rest) {
			params.Limit = value
		} else {
			params.Days = value
		}

		return true
	}

	if isBroadcastHistoryTypeToken(normalized) {
		params.Type = token
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

	value = strings.TrimSpace(after)

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
		value := strings.TrimSpace(arg)
		if value != "" {
			return value, i + 1
		}
	}

	return "", len(args)
}

func consumeMemberBroadcastHistoryFilterValue(args []string) (value string, consumed int) {
	values := make([]string, 0, len(args))
	for _, arg := range args {
		value := strings.TrimSpace(arg)
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

func applyBroadcastHistoryFilter(params *botcommand.BroadcastHistoryParams, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	applier, ok := broadcastHistoryFilterAppliers[broadcastHistoryFilterKinds[key]]
	if ok {
		applier(params, value)
	}
}

func applyBroadcastHistoryDaysFilter(params *botcommand.BroadcastHistoryParams, value string) {
	if days, ok := parseBroadcastHistoryDays(value); ok {
		params.Days = days
	}
}

func applyBroadcastHistoryLimitFilter(params *botcommand.BroadcastHistoryParams, value string) {
	if limit, ok := parsePositiveInt(value); ok {
		params.Limit = limit
	}
}

var broadcastThumbnailActionAliases = []string{"썸네일", "thumbnail", "thumb", "다운로드", "download"}

func isBroadcastThumbnailAction(token string) bool {
	return slices.Contains(broadcastThumbnailActionAliases, stringutil.Normalize(token))
}

var allBroadcastHistoryTokens = []string{"전체", "전부", "모두", "all"}

func isAllBroadcastHistoryToken(token string) bool {
	return slices.Contains(allBroadcastHistoryTokens, token)
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
	"type": paramType, "타입": paramType, "방송타입": paramType, "종류": paramType, "category": paramType, "카테고리": paramType, "분류": paramType,
	"topic": paramTopic, "topic_id": paramTopic, "토픽": paramTopic,
	"days": paramDays, "day": paramDays, "일": paramDays, "기간": paramDays,
	"limit": paramLimit, "개수": paramLimit, "갯수": paramLimit,
	"member": paramMember, "멤버": paramMember,
}

var broadcastHistoryFilterAppliers = map[string]func(*botcommand.BroadcastHistoryParams, string){
	paramType:   func(params *botcommand.BroadcastHistoryParams, value string) { params.Type = value },
	paramTopic:  func(params *botcommand.BroadcastHistoryParams, value string) { params.Topic = value },
	paramDays:   applyBroadcastHistoryDaysFilter,
	paramLimit:  applyBroadcastHistoryLimitFilter,
	paramMember: func(params *botcommand.BroadcastHistoryParams, value string) { params.Member = value },
}
