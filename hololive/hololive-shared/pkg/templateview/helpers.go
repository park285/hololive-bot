package templateview

import (
	"strings"

	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
)

func SplitTemplateInstruction(rendered string) (instruction, body string) {
	trimmed := strings.TrimLeft(rendered, "\r\n")
	if trimmed == "" {
		return "", ""
	}

	parts := strings.SplitN(trimmed, "\n", 2)
	instruction = stringutil.TrimSpace(strings.TrimSuffix(parts[0], "\r"))
	if len(parts) < 2 {
		return instruction, ""
	}

	body = strings.TrimLeft(parts[1], "\r\n")
	return instruction, body
}
