package delivery

import (
	"errors"

	"github.com/park285/iris-client-go/v5/iris"
)

// ReplyStatusIdentityMismatch는 SDK가 조회한 요청과 다른 상태 응답을 거절했는지 판정합니다.
func ReplyStatusIdentityMismatch(err error) bool {
	return errors.Is(err, iris.ErrTransport) && errors.Is(err, iris.ErrPermanent)
}
