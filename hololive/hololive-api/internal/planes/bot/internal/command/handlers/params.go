package handlers

const (
	paramMember = "member"
	paramDays   = "days"
)

func stringParam(params map[string]any, key string) string {
	value, ok := params[key]
	if !ok {
		return ""
	}

	typed, ok := value.(string)
	if !ok {
		return ""
	}

	return typed
}

func intParam(params map[string]any, key string, defaultValue int) int {
	switch value := params[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return defaultValue
	}
}

func boolParam(params map[string]any, key string) bool {
	value, ok := params[key]
	if !ok {
		return false
	}

	typed, ok := value.(bool)

	return ok && typed
}
