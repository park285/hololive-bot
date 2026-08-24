// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package transport

import (
	"errors"
	"fmt"
	"strings"
)

const (
	replyStateQueued           = "queued"
	replyStatePreparing        = "preparing"
	replyStatePrepared         = "prepared"
	replyStateSending          = "sending"
	replyStateHandoffCompleted = "handoff_completed"
	replyStateFailed           = "failed"
	replyStateOutcomeUnknown   = "outcome_unknown"
)

type replyOutcome int

const (
	replyOutcomeUnknown replyOutcome = iota
	replyOutcomeInFlight
	replyOutcomeHandoffCompleted
	replyOutcomeFailed
)

func classifyReplyState(state string) replyOutcome {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case replyStateHandoffCompleted:
		return replyOutcomeHandoffCompleted
	case replyStateFailed:
		return replyOutcomeFailed
	case replyStateOutcomeUnknown:
		return replyOutcomeUnknown
	default:
		return classifyNonterminalReplyState(state)
	}
}

func classifyNonterminalReplyState(state string) replyOutcome {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case replyStateQueued, replyStatePreparing, replyStatePrepared, replyStateSending:
		return replyOutcomeInFlight
	default:
		return replyOutcomeUnknown
	}
}

var (
	ErrReplyOutcomeUnknown = errors.New("iris reply outcome unknown")
	ErrReplyStatusFailed   = errors.New("iris reply failed")
)

type replyOutcomeUnknownError struct {
	requestID string
	reason    string
	detail    string
	cause     error
}

func (e replyOutcomeUnknownError) Error() string {
	target := "iris reply"

	if id := strings.TrimSpace(e.requestID); id != "" {
		target = "iris reply " + id
	}

	message := fmt.Sprintf("%s outcome unknown: %s", target, e.reason)
	if detail := strings.TrimSpace(e.detail); detail != "" {
		message += ": " + detail
	}

	if e.cause != nil {
		message += ": " + e.cause.Error()
	}

	return message
}

func (e replyOutcomeUnknownError) Unwrap() error { return e.cause }

func (e replyOutcomeUnknownError) Is(target error) bool { return target == ErrReplyOutcomeUnknown }

type replyStatusFailedError struct {
	requestID string
	detail    string
}

func (e replyStatusFailedError) Error() string {
	if strings.TrimSpace(e.detail) == "" {
		return fmt.Sprintf("iris reply %s failed", e.requestID)
	}

	return fmt.Sprintf("iris reply %s failed: %s", e.requestID, e.detail)
}

func (e replyStatusFailedError) Is(target error) bool { return target == ErrReplyStatusFailed }

func isReplyStatusFailed(err error) bool {
	var failed replyStatusFailedError

	return errors.As(err, &failed)
}

func IsReplyStatusFailed(err error) bool {
	return errors.Is(err, ErrReplyStatusFailed)
}

func isReplyOutcomeUnknown(err error) bool {
	return errors.Is(err, ErrReplyOutcomeUnknown)
}
