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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/park285/iris-client-go/iris"
)

func waitForAcceptedReplyHandoff(ctx context.Context, getter replyStatusGetter, accepted *iris.ReplyAcceptedResponse) error {
	if accepted == nil {
		return replyOutcomeUnknownError{reason: "iris returned no admission response"}
	}

	requestID := strings.TrimSpace(accepted.RequestID)
	if requestID == "" {
		return replyOutcomeUnknownError{reason: "iris admission response carried no request id"}
	}

	return waitForReplyHandoff(ctx, getter, requestID)
}

func waitForReplyHandoff(ctx context.Context, client replyStatusGetter, requestID string) error {
	ticker := time.NewTicker(replyStatusPollInterval)
	defer ticker.Stop()

	var lastQueryErr error

	for {
		outcome, err := checkReplyHandoffStatus(ctx, client, requestID)
		if outcome != replyOutcomeInFlight {
			return err
		}
		if err != nil {
			lastQueryErr = err
		}

		if waitReplyStatusPoll(ctx, ticker.C) {
			return replyOutcomeUnknownError{
				requestID: requestID,
				reason:    "reply status polling ended before handoff",
				detail:    lastReplyQueryErrorDetail(lastQueryErr),
				cause:     ctx.Err(),
			}
		}
	}
}

func lastReplyQueryErrorDetail(err error) string {
	if err == nil {
		return ""
	}

	return "last status query error: " + err.Error()
}

// 조회 실패는 reply의 결과가 아니라 관측 실패다. deadline까지는 in-flight로 두고 폴링을 이어간다.
func checkReplyHandoffStatus(ctx context.Context, client replyStatusGetter, requestID string) (replyOutcome, error) {
	status, err := client.GetReplyStatus(ctx, requestID)
	if err != nil {
		return replyOutcomeInFlight, err
	}
	if status == nil {
		return replyOutcomeUnknown, replyOutcomeUnknownError{
			requestID: requestID,
			reason:    "reply status response was empty",
		}
	}

	return replyHandoffStatusResult(requestID, status)
}

func replyHandoffStatusResult(requestID string, status *iris.ReplyStatusSnapshot) (replyOutcome, error) {
	outcome := classifyReplyState(status.State)
	if outcome == replyOutcomeHandoffCompleted || outcome == replyOutcomeInFlight {
		return outcome, nil
	}
	if outcome == replyOutcomeFailed {
		return outcome, replyStatusFailedError{requestID: requestID, detail: replyStatusDetail(status)}
	}
	return replyOutcomeUnknown, replyOutcomeUnknownError{
		requestID: requestID,
		reason:    fmt.Sprintf("iris reported state %q", strings.TrimSpace(status.State)),
		detail:    replyStatusDetail(status),
	}
}

func replyStatusDetail(status *iris.ReplyStatusSnapshot) string {
	if status.Detail == nil {
		return ""
	}

	return *status.Detail
}

func waitReplyStatusPoll(ctx context.Context, tick <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return true
	case <-tick:
		return false
	}
}
