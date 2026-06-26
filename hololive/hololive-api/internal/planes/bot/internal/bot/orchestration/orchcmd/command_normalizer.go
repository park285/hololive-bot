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

package orchcmd

import (
	"maps"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/stringutil"
)

const (
	CommandKeyAlarm            = "alarm"
	CommandKeyNewsSubscription = "news_subscription"
)

func NormalizeCommandKey(cmdType domain.CommandType, params map[string]any) (commandKey string, normalizedParams map[string]any) {
	typeStr := stringutil.Normalize(cmdType.String())

	if after, ok := strings.CutPrefix(typeStr, "alarm_"); ok {
		action := after
		newParams := cloneParamsWithAction(params, action)

		return CommandKeyAlarm, newParams
	}

	if typeStr == CommandKeyAlarm {
		if _, hasAction := params["action"]; !hasAction {
			newParams := cloneParamsWithAction(params, "list")
			return CommandKeyAlarm, newParams
		}
	}

	if after, ok := strings.CutPrefix(typeStr, "news_subscription_"); ok {
		action := after
		newParams := cloneParamsWithAction(params, action)

		return CommandKeyNewsSubscription, newParams
	}

	if typeStr == CommandKeyNewsSubscription {
		if _, hasAction := params["action"]; !hasAction {
			newParams := cloneParamsWithAction(params, "status")
			return CommandKeyNewsSubscription, newParams
		}
	}

	return typeStr, params
}

func cloneParamsWithAction(params map[string]any, action string) map[string]any {
	newParams := make(map[string]any)
	maps.Copy(newParams, params)

	newParams["action"] = action

	return newParams
}
