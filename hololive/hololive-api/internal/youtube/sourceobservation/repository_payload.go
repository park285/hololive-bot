package sourceobservation

import (
	"errors"
	"fmt"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

var errStoredPayloadCorrupt = errors.New("source observation payload missing or corrupt")

// DB가 보장하는 digest·외래 키 외에, 소비자가 받은 JSONB와 정본 페이로드 해시도 대조한다.
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
