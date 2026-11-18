package sourceobservation

import (
	"errors"
	"fmt"
)

var ErrPayloadTooLarge = errors.New("source observation payload is too large")

func validatePayloadStorageSize(canonical []byte) error {
	if len(canonical) <= MaxPayloadBytes/2 {
		return nil
	}

	if size := canonicalPayloadJSONBTextSize(canonical); size > MaxPayloadBytes {
		return fmt.Errorf("%w: JSONB text is %d bytes, maximum %d", ErrPayloadTooLarge, size, MaxPayloadBytes)
	}

	return nil
}

func canonicalPayloadJSONBTextSize(canonical []byte) int {
	size := len(canonical)
	quoted := false

	for i := 0; i < len(canonical); i++ {
		switch canonical[i] {
		case '\\':
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ':', ',':
			if !quoted {
				size++
			}
		}
	}

	return size
}
