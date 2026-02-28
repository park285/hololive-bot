package bot

import (
	"maps"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func normalizeCommandKey(cmdType domain.CommandType, params map[string]any) (string, map[string]any) {
	typeStr := stringutil.Normalize(cmdType.String())

	if after, ok := strings.CutPrefix(typeStr, "alarm_"); ok {
		action := after
		newParams := cloneParamsWithAction(params, action)
		return commandKeyAlarm, newParams
	}

	if typeStr == commandKeyAlarm {
		if _, hasAction := params["action"]; !hasAction {
			newParams := cloneParamsWithAction(params, "list")
			return commandKeyAlarm, newParams
		}
	}

	if after, ok := strings.CutPrefix(typeStr, "news_subscription_"); ok {
		action := after
		newParams := cloneParamsWithAction(params, action)
		return commandKeyNewsSubscription, newParams
	}

	if typeStr == commandKeyNewsSubscription {
		if _, hasAction := params["action"]; !hasAction {
			newParams := cloneParamsWithAction(params, "status")
			return commandKeyNewsSubscription, newParams
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
