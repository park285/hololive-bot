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
	"os"
	"path/filepath"
	"testing"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

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
		SourcePayload: "{\"version\":1}",
	}
	if env.Version != 1 {
		t.Fatalf("version = %d, want 1", env.Version)
	}
	if env.Retry == nil || env.Retry.Attempt != 3 {
		t.Fatalf("retry metadata = %+v", env.Retry)
	}
	if env.SourcePayload == "" {
		t.Fatal("source payload should be set")
	}
}

func TestAlarmQueueEnvelopeV1FixtureRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readAlarmContractFixture(t, "envelope_v1.json")

	var envelope contractsalarm.AlarmQueueEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if envelope.Version != contractsalarm.QueueEnvelopeVersionV1 {
		t.Fatalf("version = %d, want %d", envelope.Version, contractsalarm.QueueEnvelopeVersionV1)
	}
	if envelope.Notification.RoomID != "room-1" {
		t.Fatalf("room_id = %q, want room-1", envelope.Notification.RoomID)
	}
	if envelope.Retry == nil || envelope.Retry.Attempt != 2 {
		t.Fatalf("retry metadata = %+v, want attempt 2", envelope.Retry)
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var roundTrip contractsalarm.AlarmQueueEnvelope
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	if roundTrip.Notification.RoomID != envelope.Notification.RoomID {
		t.Fatalf("roundTrip room_id = %q, want %q", roundTrip.Notification.RoomID, envelope.Notification.RoomID)
	}
}

func TestAlarmQueueRetryMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readAlarmContractFixture(t, "envelope_v1.json")

	var envelope contractsalarm.AlarmQueueEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	encoded, err := json.Marshal(envelope.Retry)
	if err != nil {
		t.Fatalf("marshal retry metadata: %v", err)
	}
	var retry contractsalarm.AlarmQueueRetryMetadata
	if err := json.Unmarshal(encoded, &retry); err != nil {
		t.Fatalf("unmarshal retry metadata: %v", err)
	}
	if retry.RetryAfterMS != 30000 {
		t.Fatalf("retry_after_ms = %d, want 30000", retry.RetryAfterMS)
	}
	if retry.LastError != "temporary upstream error" {
		t.Fatalf("last_error = %q, want temporary upstream error", retry.LastError)
	}
}

func readAlarmContractFixture(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}
