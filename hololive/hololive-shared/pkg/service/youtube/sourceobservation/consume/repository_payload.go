package consume

import (
	"errors"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

var errStoredPayloadCorrupt = errors.New("source observation payload missing or corrupt")

// The database protects the typed digest and FK; this verifies that the JSONB
// returned to Go still matches the canonical payload hash used by consumers.
func validateStoredObservationPayload(payload []byte, digest string) error {
	if len(payload) == 0 || len(digest) != 64 {
		return errStoredPayloadCorrupt
	}

	canonical, err := contract.CanonicalizeJSON(payload)
	if err != nil {
		return fmt.Errorf("%w: %w", errStoredPayloadCorrupt, err)
	}

	if contract.SHA256Hex(canonical) != digest {
		return errStoredPayloadCorrupt
	}

	return nil
}
