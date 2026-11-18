package durability

import "strings"

const MessageIdentityPrefix = "message:"

func MessageIdentity(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, MessageIdentityPrefix) {
		return trimmed
	}

	return MessageIdentityPrefix + trimmed
}
