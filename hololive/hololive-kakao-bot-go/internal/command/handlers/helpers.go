package handlers

import "github.com/kapu/hololive-kakao-bot-go/internal/command/handlers/handlercore"

var FindMemberOrError = handlercore.FindMemberOrError
var FindActiveMemberOrError = handlercore.FindActiveMemberOrError

func validateMemberLookupDependencies(deps *Dependencies) error {
	return handlercore.ValidateMemberLookupDependencies(deps)
}
