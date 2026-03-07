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

package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmQueueEnvelope_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:       "room1",
			MinutesUntil: 5,
			Users:        []string{"user1"},
		},
		ClaimKeys:  []string{"notified:claim:room1:vid:123:LIVE"},
		EnqueuedAt: "2026-02-25T13:00:00Z",
		Version:    1,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	var decoded domain.AlarmQueueEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 실패: %v", err)
	}

	if decoded.Version != 1 {
		t.Errorf("Version = %d, want 1", decoded.Version)
	}
	if decoded.Notification.RoomID != "room1" {
		t.Errorf("RoomID = %q, want %q", decoded.Notification.RoomID, "room1")
	}
	if len(decoded.ClaimKeys) != 1 {
		t.Errorf("ClaimKeys len = %d, want 1", len(decoded.ClaimKeys))
	}
}

// TestAlarmQueueEnvelope_RustCompatibility: Rust serde 생성 JSON과의 호환성 검증
func TestAlarmQueueEnvelope_RustCompatibility(t *testing.T) {
	t.Parallel()

	// Rust serde에서 생성하는 JSON 형식
	rustJSON := `{
		"notification": {
			"room_id": "room42",
			"channel": null,
			"stream": null,
			"minutes_until": 3,
			"users": []
		},
		"claim_keys": ["notified:claim:room42:stream1:1740492000:LIVE", "notified:claim:event:room42:UC_ch:1740492000:abc123:LIVE"],
		"enqueued_at": "2026-02-25T13:00:00+00:00",
		"version": 1
	}`

	var env domain.AlarmQueueEnvelope
	if err := json.Unmarshal([]byte(rustJSON), &env); err != nil {
		t.Fatalf("Rust JSON 역직렬화 실패: %v", err)
	}

	if env.Notification.RoomID != "room42" {
		t.Errorf("RoomID = %q, want %q", env.Notification.RoomID, "room42")
	}
	if env.Notification.MinutesUntil != 3 {
		t.Errorf("MinutesUntil = %d, want 3", env.Notification.MinutesUntil)
	}
	if len(env.ClaimKeys) != 2 {
		t.Errorf("ClaimKeys len = %d, want 2", len(env.ClaimKeys))
	}
	if env.Version != 1 {
		t.Errorf("Version = %d, want 1", env.Version)
	}
	if env.EnqueuedAt != "2026-02-25T13:00:00+00:00" {
		t.Errorf("EnqueuedAt = %q, want %q", env.EnqueuedAt, "2026-02-25T13:00:00+00:00")
	}
}

// TestAlarmQueueEnvelope_OmitsScheduleChangeMessage: schedule_change_message omitempty 검증
func TestAlarmQueueEnvelope_OmitsScheduleChangeMessage(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			RoomID:       "room99",
			MinutesUntil: 0,
			Users:        []string{},
		},
		ClaimKeys:  []string{},
		EnqueuedAt: "2026-02-25T14:00:00Z",
		Version:    1,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	// schedule_change_message는 빈 문자열이면 직렬화에 포함되지 않아야 함
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw Unmarshal 실패: %v", err)
	}

	notif, ok := raw["notification"].(map[string]any)
	if !ok {
		t.Fatal("notification 필드 없음")
	}
	if _, exists := notif["schedule_change_message"]; exists {
		t.Error("schedule_change_message는 빈 값일 때 직렬화에 포함되면 안 됨")
	}
}
