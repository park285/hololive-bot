package handoff

import (
	"fmt"
	"strings"
)

type Mode string

const (
	ModeOff     Mode = "off"
	ModeShadow  Mode = "shadow"
	ModeCutover Mode = "cutover"
)

func ParseMode(raw string) (Mode, error) {
	mode := Mode(strings.ToLower(strings.TrimSpace(raw)))
	if mode == "" {
		return ModeOff, nil
	}
	switch mode {
	case ModeOff, ModeShadow, ModeCutover:
		return mode, nil
	default:
		return "", fmt.Errorf("parse alarm dispatch handoff mode: unsupported mode %q", raw)
	}
}
