package delivery

import (
	"errors"

	"github.com/park285/iris-client-go/v6/iris"
)

func ReplyStatusIdentityMismatch(err error) bool {
	return errors.Is(err, iris.ErrTransport) && errors.Is(err, iris.ErrPermanent)
}
