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
	"strconv"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
)

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

func broadcastHistoryStringFilter(param string) func(map[string]any, string) {
	return func(params map[string]any, value string) {
		params[param] = value
	}
}

func applyBroadcastHistoryDaysFilter(params map[string]any, value string) {
	if days, ok := parseBroadcastHistoryDays(value); ok {
		params[paramDays] = days
	}
}

func applyBroadcastHistoryLimitFilter(params map[string]any, value string) {
	if limit, ok := parsePositiveInt(value); ok {
		params[paramLimit] = limit
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
	"type": paramType, "타입": paramType, "방송타입": paramType, "종류": paramType, "category": paramType, "카테고리": paramType, "분류": paramType,
	"topic": paramTopic, "topic_id": paramTopic, "토픽": paramTopic,
	"days": paramDays, "day": paramDays, "일": paramDays, "기간": paramDays,
	"limit": paramLimit, "개수": paramLimit, "갯수": paramLimit,
	"member": paramMember, "멤버": paramMember,
}

var broadcastHistoryFilterAppliers = map[string]func(map[string]any, string){
	paramType:   broadcastHistoryStringFilter(paramType),
	paramTopic:  broadcastHistoryStringFilter(paramTopic),
	paramDays:   applyBroadcastHistoryDaysFilter,
	paramLimit:  applyBroadcastHistoryLimitFilter,
	paramMember: broadcastHistoryStringFilter(paramMember),
}
