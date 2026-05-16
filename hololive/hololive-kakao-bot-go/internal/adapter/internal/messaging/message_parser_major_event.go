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
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (ma *MessageAdapter) isMajorEventCommand(cmd string) bool {
	return stringutil.ContainsString([]string{"이벤트", "행사", "행사알림", "이벤트알림"}, cmd)
}

func (ma *MessageAdapter) tryMajorEventCommand(command string, args []string, raw string) (*ParsedCommand, bool) {
	if !ma.isMajorEventCommand(command) {
		return nil, false
	}

	params := map[string]any{"action": majorEventAction(args)}
	return &ParsedCommand{Type: domain.CommandMajorEvent, Params: params, RawMessage: raw}, true
}

func majorEventAction(args []string) string {
	if len(args) == 0 {
		return actionStatus
	}

	actions := map[string]string{
		"켜기":   "on",
		"on":   "on",
		"구독":   "on",
		"끄기":   "off",
		"off":  "off",
		"해제":   "off",
		"목록":   actionStatus,
		"list": actionStatus,
		"상태":   actionStatus,
	}
	if action, ok := actions[stringutil.Normalize(args[0])]; ok {
		return action
	}
	return actionStatus
}
