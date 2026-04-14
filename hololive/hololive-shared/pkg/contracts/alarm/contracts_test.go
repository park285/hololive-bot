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

package alarm_test

import (
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

func TestAlarmQueueContractConstants(t *testing.T) {
	t.Parallel()

	if contractsalarm.DispatchQueueKey != "alarm:dispatch:queue" {
		t.Fatalf("DispatchQueueKey = %q", contractsalarm.DispatchQueueKey)
	}
	if contractsalarm.DispatchRetryQueueKey != "alarm:dispatch:retry" {
		t.Fatalf("DispatchRetryQueueKey = %q", contractsalarm.DispatchRetryQueueKey)
	}
	if contractsalarm.DispatchDLQKey != "alarm:dispatch:dlq" {
		t.Fatalf("DispatchDLQKey = %q", contractsalarm.DispatchDLQKey)
	}
	if contractsalarm.NotifyClaimKeyPrefix != "notified:claim:" {
		t.Fatalf("NotifyClaimKeyPrefix = %q", contractsalarm.NotifyClaimKeyPrefix)
	}
	if contractsalarm.NotifyLogicalClaimKeyPrefix != "notified:claim:event:" {
		t.Fatalf("NotifyLogicalClaimKeyPrefix = %q", contractsalarm.NotifyLogicalClaimKeyPrefix)
	}
	if contractsalarm.QueueEnvelopeVersionV1 != 1 {
		t.Fatalf("QueueEnvelopeVersionV1 = %d", contractsalarm.QueueEnvelopeVersionV1)
	}
}

func TestAlarmQueueEnvelopeContract(t *testing.T) {
	t.Parallel()

	env := contractsalarm.AlarmQueueEnvelope{
		Version: contractsalarm.QueueEnvelopeVersionV1,
		Retry: &contractsalarm.AlarmQueueRetryMetadata{
			Attempt:       3,
			RetryAfterMS:  2500,
			NextVisibleAt: "2026-02-25T13:00:02.500Z",
			LastError:     "dispatcher unavailable",
		},
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
	if env.Retry == nil || env.Retry.Attempt != 3 {
		t.Fatalf("retry metadata = %+v", env.Retry)
	}
}
