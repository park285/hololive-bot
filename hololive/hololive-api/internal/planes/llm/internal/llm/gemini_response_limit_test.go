package llm

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadGeminiResponseEnforcesSizeLimit(t *testing.T) {
	t.Parallel()

	atLimit := bytes.Repeat([]byte("x"), maxGeminiResponseBytes)

	raw, err := readGeminiResponse(bytes.NewReader(atLimit))
	if err != nil {
		t.Fatalf("readGeminiResponse(at limit) error = %v", err)
	}

	if len(raw) != maxGeminiResponseBytes {
		t.Fatalf("len(raw) = %d, want %d", len(raw), maxGeminiResponseBytes)
	}

	_, err = readGeminiResponse(bytes.NewReader(append(atLimit, 'x')))
	if err == nil || !strings.Contains(err.Error(), "exceeds size limit") {
		t.Fatalf("readGeminiResponse(over limit) error = %v, want size limit error", err)
	}
}
