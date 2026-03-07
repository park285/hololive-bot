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

package adapter

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (ma *MessageAdapter) trySubscriberCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isSubscriberCommand(command) {
		return nil, false
	}
	// 멤버 이름이 없으면 에러 처리를 위해 빈 member로 전달
	member := stringutil.TrimSpace(strings.Join(args, " "))
	return &ParsedCommand{
		Type:       domain.CommandSubscriber,
		Params:     map[string]any{"member": member},
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isSubscriberCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"구독자", "subscriber", "subs"}, cmd)
}

func (ma *MessageAdapter) tryStatsCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isStatsCommand(command) {
		return nil, false
	}
	return &ParsedCommand{
		Type:       domain.CommandStats,
		Params:     ma.parseStatsArgs(args),
		RawMessage: raw,
	}, true
}

func (ma *MessageAdapter) isStatsCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"구독자순위", "순위", "통계", "stats", "ranking"}, cmd)
}

func (ma *MessageAdapter) parseStatsArgs(args []string) map[string]any {
	params := map[string]any{"action": "gainers"}
	for _, arg := range args {
		token := stringutil.TrimSpace(arg)
		if token == "" {
			continue
		}

		if strings.Contains(token, "=") {
			parts := strings.SplitN(token, "=", 2)
			if len(parts) != 2 {
				continue
			}

			key := stringutil.TrimSpace(parts[0])
			value := stringutil.TrimSpace(parts[1])
			if key == "" || value == "" {
				continue
			}

			lowerKey := stringutil.Normalize(key)
			if isStatsPeriodKey(lowerKey) {
				if canonical := normalizePeriodToken(value); canonical != "" {
					params["period"] = canonical
				} else {
					params["period"] = value
				}
			} else if canonical := normalizePeriodToken(value); canonical != "" {
				params["period"] = canonical
			}
			continue
		}

		if canonical := normalizePeriodToken(token); canonical != "" {
			params["period"] = canonical
		}
	}

	return params
}

func isStatsPeriodKey(key string) bool {
	switch key {
	case "period", "기간", "주기", "순위", "랭킹", "구독자", "통계":
		return true
	}
	return false
}

func normalizePeriodToken(raw string) string {
	return domain.NormalizeStatsPeriodToken(raw)
}
