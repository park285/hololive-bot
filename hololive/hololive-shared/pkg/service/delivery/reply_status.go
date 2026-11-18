package delivery

import (
	"errors"

	"github.com/park285/iris-client-go/v5/iris"
)

func ReplyStatusIdentityMismatch(err error) bool {
	return errors.Is(err, iris.ErrTransport) && errors.Is(err, iris.ErrPermanent)
}
