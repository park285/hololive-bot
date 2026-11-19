package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"google.golang.org/genai/interactions/models/decisions"

	"github.com/kapu/hololive-api/internal/service/geminidecision"
	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

var postDecisionQuestions = map[string]decisions.Question{
	"schedule_info": geminidecision.NoulQuestion(
		"The input is a social media post written by a VTuber. Does this post contain schedule information about an upcoming "+
			"stream, collaboration, or event (such as its date, start time, planned content, time change, postponement, cancellation, "+
			"day off, or undecided status)?",
		"States a future or today's upcoming stream schedule, start time, planned topic, time change, postponement, "+
			"cancellation, day off, or tentative plan.",
		"Greetings, thanks, already-started or going-live-right-now link drops without schedule info, merch or "+
			"VOD/Shorts promotions, or casual chat.",
	),
	"kind": geminidecision.ChoiceQuestion(
		"The input is a social media post written by a VTuber. What kind of stream schedule message is this post?",
		map[string]string{
			"announce": "Announces the date, start time, or planned content of a specific upcoming stream or collaboration.",
			"change":   "Changes the start time, date, content, or tentative status of a previously planned stream.",
			"cancel":   "Cancels, postpones, or announces a day off from streaming.",
			"weekly":   "Presents a weekly schedule covering multiple days.",
			"other": "Anything outside these categories, including greetings, going-live-now posts, merch or video promotions, " +
				"and casual chat.",
		},
	),
}

var (
	postKinds      = []string{"announce", "change", "cancel", "weekly", "other"}
	unrelatedKinds = []string{"weekly", "other"}
)

type postAnswers struct {
	ScheduleInfo noulAnswer `json:"schedule_info"`
	Kind         struct {
		Choice *string `json:"choice"`
	} `json:"kind"`
}

func (j *geminiJudge) classifyPost(ctx context.Context, body string) (postAnswer, usage, error) {
	decoded, err := j.decisions.Create[postAnswers, geminidecision.Feedback](ctx, geminidecision.Request{
		Input: decisions.NewInput(body), Questions: postDecisionQuestions,
		QuestionOrder: []string{"schedule_info", "kind"},
		ChoiceOrder:   map[string][]string{"kind": postKinds},
	})

	err = unreachable(err)

	if status, ok := errors.AsType[*geminidecision.StatusError](err); ok && status.SafetyBlocked() {
		return postAnswer{}, usage{}, &postFaultError{reason: reasonBlocked, err: err}
	}

	switch {
	case errors.Is(err, geminidecision.ErrRejected), errors.Is(err, geminidecision.ErrUnavailable), errors.Is(err, errReviewUnavailable):
		return postAnswer{}, usage{}, fmt.Errorf("%w: classify post: %w", errChangeServiceFailed, err)
	case errors.Is(err, geminidecision.ErrInvalidResponse):
		return postAnswer{}, usage{}, &postFaultError{reason: reasonBadAnswer, err: err}
	case err != nil:
		return postAnswer{}, usage{}, fmt.Errorf("%w: classify post: %w", errChangeOutcomeUnknown, err)
	}

	spent := usage{model: decisionModel, prompt: decoded.Usage.TotalInputTokens}

	if decoded.PromptFeedback.BlockReason != "" {
		return postAnswer{}, spent, &postFaultError{reason: reasonBlocked, err: errors.New("post decision input blocked")}
	}

	score, choice := decoded.Answers.ScheduleInfo.Noul, decoded.Answers.Kind.Choice
	if score == nil || choice == nil || *score < 0 || *score > 1 || !slices.Contains(postKinds, *choice) {
		return postAnswer{}, spent, &postFaultError{reason: reasonBadAnswer, err: errors.New("post decision answer is missing or out of range")}
	}

	return postAnswer{ScheduleInfo: *score, Kind: *choice}, spent, nil
}

func (a postAnswer) decision(scheduleInfoMin float64) posterstore.PostDecision {
	status := posterstore.PostNotRelevant

	if a.ScheduleInfo >= scheduleInfoMin && !slices.Contains(unrelatedKinds, a.Kind) {
		status = posterstore.PostRelevant
	}

	return posterstore.PostDecision{Status: status, ScheduleInfo: a.ScheduleInfo, Kind: a.Kind}
}
