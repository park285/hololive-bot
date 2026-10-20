// Package handoffcheck는 Iris 202 admission 뒤 /reply-status 관측을 발송 원장의 다음 행동으로 바꾸는 순수 판정과
// 확인 간격이다. alarm-dispatch·notification·youtube reconciler가 함께 쓰며, 원장 상태 어휘·전이 SQL·시도 예산은
// 각 원장이 소유한다. 시각은 원장 DB 시계로 잰 경과만 받고 이 패키지는 현재 시각을 읽지 않는다.
package handoffcheck

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
)

// FirstCheckDelay는 admission부터 첫 확인까지의 간격이다. Iris 같은 ID 재시도 창(약 2초)보다 길게 두어 첫 확인에서
// failed가 아직 queued로 보이는 경우를 bridge 지연이 그 창을 넘을 때로 줄인다. 원장은 첫 확인을
// min(FirstCheckDelay, 지평) 뒤로 예약해 지평이 더 짧아도 admission + 지평을 넘기지 않는다.
const FirstCheckDelay = 5 * time.Second

// checkSteps는 첫 확인 뒤의 간격이다. 마지막 간격을 지평까지 반복한다. 예정 확인 시각은 admission 기준
// 5s, 20s, 80s, 380s, 680s, …이고 원장은 각 예약을 admission + 지평에서 자른다.
var checkSteps = [...]time.Duration{15 * time.Second, time.Minute, 5 * time.Minute}

// NextStep은 admission부터 elapsed가 지난 확인 뒤의 다음 간격을 돌려준다. 예정 확인 시각 가운데 elapsed가 아직 닿지
// 않은 첫 시각의 간격이며, 확인이 늦게 돌았으면 건너뛴 간격은 다시 쓰지 않는다.
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

// 원장 last_error_code에 남는 handoff 결과 코드다.
const (
	CodeFailed         = "handoff_failed"
	CodeOutcomeUnknown = "handoff_outcome_unknown"
	CodeStatusMissing  = "handoff_status_missing"
	CodeUnconfirmed    = "handoff_unconfirmed"
)

// Action은 관측 하나에 대한 원장의 다음 행동이다.
type Action uint8

const (
	// ActionReschedule은 전이 없이 다음 확인을 예약한다.
	ActionReschedule Action = iota
	// ActionComplete는 handoff 완료를 확인으로 기록한다.
	ActionComplete
	// ActionReissue는 Iris가 dispatch 전에 포기한 요청을 다음 요청 세대로 다시 대기시킨다.
	ActionReissue
	// ActionExhausted는 Iris가 포기한 요청을 세대나 시도 예산이 없어 재발송하지 않고 종료한다.
	ActionExhausted
	// ActionQuarantine은 결과 불명을 운영 검토 대상으로 격리한다.
	ActionQuarantine
)

// String은 로그에 남기는 행동 이름이다.
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

// Input은 handoff 확인 한 번의 관측과 판정 조건이다.
type Input struct {
	// RequestID는 admission 202에서 저장한 Iris requestId다.
	RequestID string
	Snapshot  *iris.ReplyStatusSnapshot
	QueryErr  error
	// Elapsed는 원장 DB 시계로 잰 admission 뒤 경과, Horizon은 확인 지평(원장 executor attempt timeout)이다.
	Elapsed time.Duration
	Horizon time.Duration
	// Generation은 지금 요청의 재발급 세대이고, RetryBudgetLeft는 원장 시도 예산으로 한 번 더 보낼 수 있는지다.
	Generation      int
	RetryBudgetLeft bool
}

// Decision은 판정 결과다. Code·Detail은 전이하는 행동에서만 쓰며 원장 last_error_code·last_error에 남는다.
type Decision struct {
	Action Action
	Code   string
	Detail string
}

// Decide는 Iris /reply-status 관측을 원장 행동으로 바꾼다.
//   - handoff_completed는 확인 완료다.
//   - failed는 Iris가 dispatch 전에 포기한 요청이다. 세대가 iris.ReplyReissueMaxGenerations 미만이고 시도 예산이
//     남으면 재발급, 아니면 소진이다.
//   - outcome_unknown, 모르는 상태, 빈 응답, requestId 불일치, 404는 결과 불명이라 격리한다.
//   - 진행 중 상태와 404가 아닌 조회 오류는 지평 전까지 전이 없이 다시 확인하고, 지평에 닿으면 handoff 미확인으로
//     격리한다.
func Decide(in Input) Decision {
	if in.QueryErr != nil {
		if httpErr, ok := errors.AsType[*iris.HTTPError](in.QueryErr); ok && httpErr.StatusCode == http.StatusNotFound {
			return Decision{Action: ActionQuarantine, Code: CodeStatusMissing, Detail: "iris reply status not found"}
		}

		return pending(in, QueryErrorDetail(in.QueryErr))
	}

	if in.Snapshot == nil {
		return Decision{Action: ActionQuarantine, Code: CodeOutcomeUnknown, Detail: "iris reply status response is empty"}
	}

	if strings.TrimSpace(in.Snapshot.RequestID) != in.RequestID {
		return Decision{Action: ActionQuarantine, Code: CodeOutcomeUnknown, Detail: "iris reply status request id does not match"}
	}

	// 대소문자·공백 허용은 기존 발송 경로의 정책이며 SDK의 정확한 wire 분류와 분리한다.
	state := strings.ToLower(strings.TrimSpace(in.Snapshot.State))

	switch iris.ClassifyReplyStatus(state) {
	case iris.ReplyStatusHandoffCompleted:
		return Decision{Action: ActionComplete}
	case iris.ReplyStatusFailed:
		if in.Generation < iris.ReplyReissueMaxGenerations && in.RetryBudgetLeft {
			return Decision{Action: ActionReissue, Code: CodeFailed, Detail: "iris reply status failed"}
		}

		return Decision{Action: ActionExhausted, Code: CodeFailed, Detail: "iris reply status failed; reissue budget exhausted"}
	case iris.ReplyStatusPending:
		return pending(in, "iris reply status "+state)
	case iris.ReplyStatusUnknown:
		// outcome_unknown과 모르는 상태는 아래의 결과 불명 처리로 간다.
	}

	if state == "outcome_unknown" {
		return Decision{Action: ActionQuarantine, Code: CodeOutcomeUnknown, Detail: "iris reply status outcome_unknown"}
	}

	return Decision{Action: ActionQuarantine, Code: CodeOutcomeUnknown, Detail: "iris reply status is not recognized"}
}

func pending(in Input, last string) Decision {
	if in.Elapsed >= in.Horizon {
		return Decision{Action: ActionQuarantine, Code: CodeUnconfirmed, Detail: "iris handoff unconfirmed at horizon: " + last}
	}

	return Decision{Action: ActionReschedule}
}

// QueryErrorDetail은 조회 오류를 저장·로그 문구로 줄인다. SDK 오류 문구와 URL에는 /reply-status/{requestId} 경로가
// 섞이므로 원문 대신 HTTP 상태만 남긴다.
func QueryErrorDetail(err error) string {
	if httpErr, ok := errors.AsType[*iris.HTTPError](err); ok {
		return fmt.Sprintf("iris reply status query failed with http %d", httpErr.StatusCode)
	}

	return "iris reply status query failed"
}
