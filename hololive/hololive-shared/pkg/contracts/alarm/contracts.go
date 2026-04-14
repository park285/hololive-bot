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

package alarm

import "github.com/kapu/hololive-shared/pkg/domain"

const (
	DispatchQueueKey      = "alarm:dispatch:queue"
	DispatchRetryQueueKey = "alarm:dispatch:retry"
	DispatchDLQKey        = "alarm:dispatch:dlq"

	NotifyClaimKeyPrefix        = "notified:claim:"
	NotifyLogicalClaimKeyPrefix = "notified:claim:event:"

	QueueEnvelopeVersionV1 uint8 = 1
)

type AlarmQueueEnvelope struct {
	Notification domain.AlarmNotification `json:"notification"`
	ClaimKeys    []string                 `json:"claim_keys"`
	EnqueuedAt   string                   `json:"enqueued_at"`
	Version      uint8                    `json:"version"`
	Retry        *AlarmQueueRetryMetadata `json:"retry,omitempty"`
}

type AlarmQueueRetryMetadata struct {
	Attempt       int    `json:"attempt,omitempty"`
	RetryAfterMS  int64  `json:"retry_after_ms,omitempty"`
	NextVisibleAt string `json:"next_visible_at,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}
