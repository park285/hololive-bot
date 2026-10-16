package sourceobservation

import (
	"errors"
	"fmt"
)

// ErrPayloadTooLarge는 raw/canonical JSON 또는 PostgreSQL JSONB text의 payload 상한 초과다.
// Scope/cursor의 정규화 오류와 구별하며, 수집기는 response_too_large/RESOURCE_LIMIT로 처리한다.
var ErrPayloadTooLarge = errors.New("source observation payload is too large")

func validatePayloadStorageSize(canonical []byte) error {
	// 정규형의 각 byte가 추가 공백을 최대 하나만 만들므로 이 범위는 재순회가 필요 없다.
	if len(canonical) <= MaxPayloadBytes/2 {
		return nil
	}

	if size := canonicalPayloadJSONBTextSize(canonical); size > MaxPayloadBytes {
		return fmt.Errorf("%w: JSONB text is %d bytes, maximum %d", ErrPayloadTooLarge, size, MaxPayloadBytes)
	}

	return nil
}

// canonicalPayloadJSONBTextSize는 검증된 canonical-json-v1만 받는다. 이 profile의 숫자는
// 지수/소수점 없는 safe integer이며 문자열의 UTF-8/escape 표기는 PostgreSQL과 같다.
// JSONB text는 문자열 밖 member ':'와 원소 ',' 뒤에만 공백 하나를 추가한다.
// Key 정렬은 길이를 바꾸지 않고 빈 container는 공백을 만들지 않는다.
func canonicalPayloadJSONBTextSize(canonical []byte) int {
	size := len(canonical)
	quoted := false

	for i := 0; i < len(canonical); i++ {
		switch canonical[i] {
		case '\\':
			if quoted {
				i++ // escape된 따옴표/역슬래시는 문자열 경계를 바꾸지 않는다.
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
