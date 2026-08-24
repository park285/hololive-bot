package handlers

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

func validateMemberLookupDependencies(deps *handlercore.Dependencies) error {
	if err := handlercore.ValidateMemberLookupDependencies(deps); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

func memberLookupHandled(err error) bool {
	return errors.Is(err, handlercore.ErrMemberLookupHandled)
}
