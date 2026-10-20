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
)

var ErrReplyOutcomeUnknown = errors.New("iris reply outcome unknown")

// replyOutcomeUnknownError는 admission 응답이 유실됐거나 durable admission을 증명하지 못한 경우다.
type replyOutcomeUnknownError struct {
	reason string
	cause  error
}

func (e replyOutcomeUnknownError) Error() string {
	message := "iris reply outcome unknown: " + e.reason
	if e.cause != nil {
		message += ": " + e.cause.Error()
	}

	return message
}

func (e replyOutcomeUnknownError) Unwrap() error { return e.cause }

func (e replyOutcomeUnknownError) Is(target error) bool { return target == ErrReplyOutcomeUnknown }

// IsReplyOutcomeUnknown은 Iris 전달 또는 영속 응답 저장의 결과가 불명인지 판정한다.
// 저장 응답이 유실돼도 outbox 행은 커밋됐을 수 있으므로 추가 응답을 보내면 안 된다.
func IsReplyOutcomeUnknown(err error) bool {
	return errors.Is(err, ErrReplyOutcomeUnknown) || errors.Is(err, ErrReplyStagingFailed)
}
