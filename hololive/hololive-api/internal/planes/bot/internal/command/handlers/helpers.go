package handlers

import (
	"errors"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

func memberLookupHandled(err error) bool {
	return errors.Is(err, handlercore.ErrMemberLookupHandled)
}
