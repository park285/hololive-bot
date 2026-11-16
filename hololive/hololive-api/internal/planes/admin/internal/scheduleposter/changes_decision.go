package scheduleposter

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"slices"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
)

type kindCriteria struct {
	Announce string `json:"announce"`
	Change   string `json:"change"`
	Cancel   string `json:"cancel"`
	Weekly   string `json:"weekly"`
	Other    string `json:"other"`
}

type kindQuestion struct {
	Type         string       `json:"type"`
	Instructions string       `json:"instructions"`
	Criteria     kindCriteria `json:"criteria"`
}

type postQuestions struct {
	ScheduleInfo decisionQuestion `json:"schedule_info"`
	Kind         kindQuestion     `json:"kind"`
}

type postDecisionRequest struct {
	Model     string        `json:"model"`
	Input     string        `json:"input"`
	Questions postQuestions `json:"questions"`
}

// 판별 결합 규칙의 정밀도·재현율은 이 문구와 순서로 측정했다. 바꾸면 다시 측정해야 한다.
var postDecisionQuestions = postQuestions{
	ScheduleInfo: decisionQuestion{
		Type: "noul",
		Instructions: "The input is a social media post written by a VTuber. Does this post contain schedule information about an upcoming " +
			"stream, collaboration, or event (such as its date, start time, planned content, time change, postponement, cancellation, " +
			"day off, or undecided status)?",
		Criteria: decisionCriteria{
			TrueDescription: "States a future or today's upcoming stream schedule, start time, planned topic, time change, postponement, " +
				"cancellation, day off, or tentative plan.",
			FalseDescription: "Greetings, thanks, already-started or going-live-right-now link drops without schedule info, merch or " +
				"VOD/Shorts promotions, or casual chat.",
		},
	},
	Kind: kindQuestion{
		Type:         "choice",
		Instructions: "The input is a social media post written by a VTuber. What kind of stream schedule message is this post?",
		Criteria: kindCriteria{
			Announce: "Announces the date, start time, or planned content of a specific upcoming stream or collaboration.",
			Change:   "Changes the start time, date, content, or tentative status of a previously planned stream.",
			Cancel:   "Cancels, postpones, or announces a day off from streaming.",
			Weekly:   "Presents a weekly schedule covering multiple days.",
			Other: "Anything outside these categories, including greetings, going-live-now posts, merch or video promotions, " +
				"and casual chat.",
		},
	},
}

var (
	postKinds       = []string{"announce", "change", "cancel", "weekly", "other"}
	unrelatedKinds  = []string{"weekly", "other"}
	safetyBlockCode = []string{"safety", "prohibited_content", "blocklist", "image_safety", "content_blocked"}
)

type postDecisionResponse struct {
	Answers struct {
		ScheduleInfo struct {
			Noul *float64 `json:"noul"`
		} `json:"schedule_info"`
		Kind struct {
			Choice *string `json:"choice"`
		} `json:"kind"`
	} `json:"answers"`
	PromptFeedback struct {
		BlockReason string `json:"block_reason"`
	} `json:"prompt_feedback"`
	Usage struct {
		TotalInputTokens int32 `json:"total_input_tokens"`
	} `json:"usage"`
}

// Decisions API 자체의 400은 code가 문자열이고, 게이트웨이의 400은 정수다.
type postDecisionError struct {
	Error struct {
		Code    any    `json:"code"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"error"`
}

func (j *geminiJudge) classifyPost(ctx context.Context, body string) (postAnswer, usage, error) {
	request, err := jsonv2.Marshal(postDecisionRequest{Model: decisionModel, Input: body, Questions: postDecisionQuestions})
	if err != nil {
		return postAnswer{}, usage{}, fmt.Errorf("encode post decision request: %w", err)
	}

	status, raw, err := j.decide(ctx, request)

	switch {
	case errors.Is(err, errReviewUnavailable):
		return postAnswer{}, usage{}, fmt.Errorf("%w: classify post: %w", errChangeServiceFailed, err)
	case err != nil:
		return postAnswer{}, usage{}, fmt.Errorf("%w: classify post: %w", errChangeOutcomeUnknown, err)
	case status != http.StatusOK:
		return postAnswer{}, usage{}, postDecisionFailure(status, raw)
	}

	return decodePostDecision(raw)
}

// postDecisionFailure가 글 탓으로 닫는 400은 안전 차단뿐이다. 다른 400은 요청 꼴·키·할당량 문제라 글 탓으로 두면 모든 글이 종단된다.
// 글 reason은 관리 API 계약의 닫힌 enum이라 Decisions 오류 코드는 오류 문구에만 남긴다.
func postDecisionFailure(status int, raw []byte) error {
	var failure postDecisionError

	if err := jsonv2.Unmarshal(raw, &failure); err != nil {
		failure = postDecisionError{}
	}

	var code string

	if value, isString := failure.Error.Code.(string); isString {
		code = value
	}

	err := fmt.Errorf("post decision status %d %s %s: %s", status, failure.Error.Status, clip(code), clip(failure.Error.Message))

	switch {
	case status == http.StatusBadRequest && slices.Contains(safetyBlockCode, code):
		return &postFaultError{reason: reasonBlocked, err: err}
	case status == http.StatusBadRequest, status == http.StatusUnauthorized, status == http.StatusForbidden,
		status == http.StatusNotFound, status == http.StatusTooManyRequests, status == http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %w", errChangeServiceFailed, err)
	default:
		return fmt.Errorf("%w: %w", errChangeOutcomeUnknown, err)
	}
}

func decodePostDecision(raw []byte) (postAnswer, usage, error) {
	var decoded postDecisionResponse

	if err := jsonv2.Unmarshal(raw, &decoded); err != nil {
		return postAnswer{}, usage{}, &postFaultError{reason: reasonBadAnswer, err: errors.New("decode post decision response")}
	}

	spent := usage{model: decisionModel, prompt: decoded.Usage.TotalInputTokens}

	if blocked := decoded.PromptFeedback.BlockReason; blocked != "" {
		return postAnswer{}, spent, &postFaultError{reason: reasonBlocked, err: fmt.Errorf("post decision input blocked: %s", clip(blocked))}
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
