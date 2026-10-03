package sourceobservation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

func newLeaseToken() (string, error) {
	var token [32]byte

	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return hex.EncodeToString(token[:]), nil
}

func boundedErrorDetail(value string) string {
	if len(value) <= maxErrorTextBytes {
		return value
	}

	return value[:maxErrorTextBytes]
}
