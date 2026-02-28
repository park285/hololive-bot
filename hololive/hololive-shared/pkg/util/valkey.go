package util

import (
	"errors"

	"github.com/valkey-io/valkey-go"
)

// IsValkeyNil: 에러가 Valkey nil 에러인지 확인합니다.
// 여러 단계로 래핑된 에러도 루프를 통해 언랩하여 판별한다.
func IsValkeyNil(err error) bool {
	for err != nil {
		if valkey.IsValkeyNil(err) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}
