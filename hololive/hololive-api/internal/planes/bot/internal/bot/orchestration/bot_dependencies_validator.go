package orchestration

import (
	"errors"
)

func validateBotDependencies(deps *Dependencies) error {
	if deps == nil {
		return errors.New("bot dependencies are required")
	}

	if deps.Logger == nil {
		return errors.New("logger dependency is required")
	}

	if deps.Client == nil {
		return errors.New("iris client dependency is required")
	}

	if deps.ReplyOutbox == nil {
		return errors.New("reply outbox dependency is required")
	}

	if deps.MessageAdapter == nil {
		return errors.New("message adapter dependency is required")
	}

	if deps.Formatter == nil {
		return errors.New("response formatter dependency is required")
	}

	if deps.Cache == nil {
		return errors.New("cache dependency is required")
	}

	if deps.Postgres == nil {
		return errors.New("postgres dependency is required")
	}

	if deps.Postgres.GetPool() == nil {
		return errors.New("postgres pool is required")
	}

	if deps.Holodex == nil {
		return errors.New("holodex dependency is required")
	}

	if deps.Alarm == nil {
		return errors.New("alarm service dependency is required")
	}

	if deps.Matcher == nil {
		return errors.New("matcher dependency is required")
	}

	if deps.MembersData == nil {
		return errors.New("member data dependency is required")
	}

	if deps.MemberRepository == nil {
		return errors.New("member repository dependency is required")
	}

	return nil
}
