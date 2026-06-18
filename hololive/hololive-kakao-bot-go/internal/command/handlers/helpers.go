package handlers

import (
	"errors"

	"github.com/kapu/hololive-kakao-bot-go/internal/command/handlers/handlercore"
)

var FindMemberOrError = handlercore.FindMemberOrError
var FindActiveMemberOrError = handlercore.FindActiveMemberOrError
var FindMemberWithCandidatesOrError = handlercore.FindMemberWithCandidatesOrError
var FindActiveMemberWithCandidatesOrError = handlercore.FindActiveMemberWithCandidatesOrError

func validateMemberLookupDependencies(deps *Dependencies) error {
	return handlercore.ValidateMemberLookupDependencies(deps)
}

func memberLookupHandled(err error) bool {
	return errors.Is(err, handlercore.ErrMemberLookupHandled)
}
