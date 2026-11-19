package handoffcheck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/park285/iris-client-go/v5/iris"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const FirstCheckDelay = 5 * time.Second

var checkSteps = [...]time.Duration{15 * time.Second, time.Minute, 5 * time.Minute}

func NextStep(elapsed time.Duration) time.Duration {
	offset := FirstCheckDelay

	for _, step := range checkSteps {
		offset += step
		if elapsed < offset {
			return step
		}
	}

	return checkSteps[len(checkSteps)-1]
}

type Action uint8

const (
	ActionReschedule Action = iota
	ActionComplete
	ActionReissue
	ActionExhausted
	ActionQuarantine
)

func (a Action) String() string {
	switch a {
	case ActionReschedule:
		return "reschedule"
	case ActionComplete:
		return "complete"
	case ActionReissue:
		return "reissue"
	case ActionExhausted:
		return "exhausted"
	case ActionQuarantine:
		return "quarantine"
	}

	return fmt.Sprintf("action(%d)", uint8(a))
}

type Input struct {
	RequestID       string
	Snapshot        *iris.ReplyStatusSnapshot
	QueryErr        error
	Elapsed         time.Duration
	Horizon         time.Duration
	Generation      int
	RetryBudgetLeft bool
}

type Decision struct {
	Action Action
	Code   string
	Detail string
}

func Decide(in Input) Decision {
	if in.QueryErr != nil {
		if httpErr, ok := errors.AsType[*iris.HTTPError](in.QueryErr); ok && httpErr.StatusCode == http.StatusNotFound {
			return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeStatusMissing, Detail: "iris reply status not found"}
		}

		if delivery.ReplyStatusIdentityMismatch(in.QueryErr) {
			return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeOutcomeUnknown, Detail: "iris reply status request id does not match"}
		}

		return pending(in, QueryErrorDetail(in.QueryErr))
	}

	if in.Snapshot == nil {
		return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeOutcomeUnknown, Detail: "iris reply status response is empty"}
	}

	if in.Snapshot.RequestID != in.RequestID {
		return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeOutcomeUnknown, Detail: "iris reply status request id does not match"}
	}

	state := in.Snapshot.State

	switch iris.ClassifyReplyStatus(state) {
	case iris.ReplyStatusHandoffCompleted:
		return Decision{Action: ActionComplete}
	case iris.ReplyStatusFailed:
		if in.Generation < iris.ReplyReissueMaxGenerations && in.RetryBudgetLeft {
			return Decision{Action: ActionReissue, Code: contractsalarm.HandoffCodeFailed, Detail: "iris reply status failed"}
		}

		return Decision{Action: ActionExhausted, Code: contractsalarm.HandoffCodeFailed, Detail: "iris reply status failed; reissue budget exhausted"}
	case iris.ReplyStatusPending:
		return pending(in, "iris reply status "+state)
	case iris.ReplyStatusUnknown:
	}

	if state == "outcome_unknown" {
		return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeOutcomeUnknown, Detail: "iris reply status outcome_unknown"}
	}

	return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeOutcomeUnknown, Detail: "iris reply status is not recognized"}
}

func pending(in Input, last string) Decision {
	if in.Elapsed >= in.Horizon {
		return Decision{Action: ActionQuarantine, Code: contractsalarm.HandoffCodeUnconfirmed, Detail: "iris handoff unconfirmed at horizon: " + last}
	}

	return Decision{Action: ActionReschedule}
}

func QueryErrorDetail(err error) string {
	if httpErr, ok := errors.AsType[*iris.HTTPError](err); ok {
		return fmt.Sprintf("iris reply status query failed with http %d", httpErr.StatusCode)
	}

	return "iris reply status query failed"
}

type ReplyStatusClient interface {
	GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error)
}
