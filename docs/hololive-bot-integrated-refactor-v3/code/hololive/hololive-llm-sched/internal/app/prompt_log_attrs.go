package app

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
)

func promptSummaryAttrs(provider, model, prompt string) []slog.Attr {
	prompt = strings.TrimSpace(prompt)
	attrs := []slog.Attr{
		slog.String("provider", strings.TrimSpace(provider)),
		slog.String("model", strings.TrimSpace(model)),
		slog.Int("prompt_len", len(prompt)),
	}
	if prompt != "" {
		sum := sha256.Sum256([]byte(prompt))
		attrs = append(attrs, slog.String("prompt_sha256_8", hex.EncodeToString(sum[:8])))
	}
	return attrs
}
