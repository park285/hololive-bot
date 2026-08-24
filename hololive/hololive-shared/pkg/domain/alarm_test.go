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
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmQueueEnvelope_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 123,
		Notification: domain.AlarmNotification{
			RoomID:       "room1",
			MinutesUntil: 5,
			Users:        []string{"user1"},
		},
		ClaimKeys:  []string{"notified:claim:room1:vid:123:LIVE"},
		EnqueuedAt: "2026-02-25T13:00:00Z",
		Version:    1,
		Retry: &domain.AlarmQueueRetryMetadata{
			Attempt:       2,
			RetryAfterMS:  1500,
			NextVisibleAt: "2026-02-25T13:00:01.500Z",
			LastError:     "temporary upstream timeout",
		},
	}

	data, err := jsonv2.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	var decoded domain.AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 실패: %v", err)
	}

	if decoded.Version != 1 {
		t.Errorf("Version = %d, want 1", decoded.Version)
	}

	if decoded.DispatchOutboxID != 123 {
		t.Errorf("DispatchOutboxID = %d, want 123", decoded.DispatchOutboxID)
	}

	if decoded.Notification.RoomID != "room1" {
		t.Errorf("RoomID = %q, want %q", decoded.Notification.RoomID, "room1")
	}

	if len(decoded.ClaimKeys) != 1 {
		t.Errorf("ClaimKeys len = %d, want 1", len(decoded.ClaimKeys))
	}

	if decoded.Retry == nil {
		t.Fatal("Retry = nil, want metadata")
	}

	if decoded.Retry.Attempt != 2 {
		t.Errorf("Retry.Attempt = %d, want 2", decoded.Retry.Attempt)
	}

	if decoded.Retry.RetryAfterMS != 1500 {
		t.Errorf("Retry.RetryAfterMS = %d, want 1500", decoded.Retry.RetryAfterMS)
	}

	if decoded.Retry.NextVisibleAt != "2026-02-25T13:00:01.500Z" {
		t.Errorf("Retry.NextVisibleAt = %q, want %q", decoded.Retry.NextVisibleAt, "2026-02-25T13:00:01.500Z")
	}

	if decoded.Retry.LastError != "temporary upstream timeout" {
		t.Errorf("Retry.LastError = %q, want %q", decoded.Retry.LastError, "temporary upstream timeout")
	}
}

func TestAlarmQueueEnvelope_JSONRoundtripYouTubeOutboxSource(t *testing.T) {
	t.Parallel()

	envelope := domain.AlarmQueueEnvelope{
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeShorts,
			RoomID:    "room1",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs:         []int64{101},
			Kind:              domain.OutboxKindNewShort,
			AlarmType:         domain.AlarmTypeShorts,
			ChannelID:         testChannelID,
			RenderTemplateKey: domain.TemplateKeyOutboxShorts,
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  101,
				ContentID: "short:abc",
				Payload:   `{"video_id":"abc","title":"테스트 쇼츠"}`,
			}},
		},
		ClaimKeys:  []string{"youtube-notification:NEW_SHORT:short:abc:room1"},
		EnqueuedAt: "2026-05-14T00:00:00Z",
		Version:    1,
	}

	data, err := jsonv2.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	var raw map[string]any

	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw Unmarshal 실패: %v", err)
	}

	if raw["source_kind"] != string(domain.AlarmDispatchSourceKindYouTubeOutbox) {
		t.Fatalf("source_kind = %v, want %q", raw["source_kind"], domain.AlarmDispatchSourceKindYouTubeOutbox)
	}

	if _, ok := raw["youtube_outbox"]; !ok {
		t.Fatal("youtube_outbox 필드 없음")
	}

	var decoded domain.AlarmQueueEnvelope

	if err := jsonv2.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 실패: %v", err)
	}

	if decoded.SourceKind != domain.AlarmDispatchSourceKindYouTubeOutbox {
		t.Fatalf("SourceKind = %q, want %q", decoded.SourceKind, domain.AlarmDispatchSourceKindYouTubeOutbox)
	}

	if decoded.YouTubeOutbox == nil {
		t.Fatal("YouTubeOutbox = nil")
	}

	if decoded.YouTubeOutbox.Items[0].ContentID != "short:abc" {
		t.Fatalf("ContentID = %q, want short:abc", decoded.YouTubeOutbox.Items[0].ContentID)
	}
}

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

	if err := jsonv2.Unmarshal([]byte(rustJSON), &env); err != nil {
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

	data, err := jsonv2.Marshal(&envelope)
	if err != nil {
		t.Fatalf("Marshal 실패: %v", err)
	}

	// schedule_change_message는 빈 문자열이면 직렬화에 포함되지 않아야 함
	var raw map[string]any

	if err := jsonv2.Unmarshal(data, &raw); err != nil {
		t.Fatalf("raw Unmarshal 실패: %v", err)
	}

	notif, ok := raw["notification"].(map[string]any)
	if !ok {
		t.Fatal("notification 필드 없음")
	}

	if _, exists := notif["schedule_change_message"]; exists {
		t.Error("schedule_change_message는 빈 값일 때 직렬화에 포함되면 안 됨")
	}

	if _, exists := raw["retry"]; exists {
		t.Error("retry는 nil일 때 직렬화에 포함되면 안 됨")
	}

	if _, exists := raw["source_payload"]; exists {
		t.Error("source_payload는 기본 envelope 직렬화에 포함되면 안 됨")
	}
}

func TestNewAlarmNotification_UsesExplicitLiveDispatchRoute(t *testing.T) {
	t.Parallel()

	notification := domain.NewAlarmNotification("room-live", nil, nil, 5, nil, "")
	if notification.AlarmType != domain.AlarmTypeLive {
		t.Fatalf("AlarmType = %q, want %q", notification.AlarmType, domain.AlarmTypeLive)
	}

	if err := notification.ValidateLiveDispatchRoute(); err != nil {
		t.Fatalf("ValidateLiveDispatchRoute() error = %v", err)
	}
}

func TestAlarmNotificationStartingPhase(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.August, 17, 0, 31, 45, 0, time.UTC)
	tests := map[string]struct {
		notification *domain.AlarmNotification
		wantStarting bool
		wantCatchup  bool
	}{
		"nil": {},
		"zero minute starts without stream state": {
			notification: &domain.AlarmNotification{MinutesUntil: 0},
			wantStarting: true,
		},
		"positive minute upcoming stays prelive": {
			notification: &domain.AlarmNotification{
				MinutesUntil: 5,
				Stream:       &domain.Stream{Status: domain.StreamStatusUpcoming},
			},
		},
		"positive minute live is catchup": {
			notification: &domain.AlarmNotification{
				MinutesUntil: 5,
				Stream:       &domain.Stream{Status: domain.StreamStatusLive},
			},
			wantStarting: true,
			wantCatchup:  true,
		},
		"positive minute actual start is catchup": {
			notification: &domain.AlarmNotification{
				MinutesUntil: 5,
				Stream:       &domain.Stream{Status: domain.StreamStatusUpcoming, StartActual: &startedAt},
			},
			wantStarting: true,
			wantCatchup:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tc.notification.IsStarting(); got != tc.wantStarting {
				t.Fatalf("IsStarting() = %t, want %t", got, tc.wantStarting)
			}

			if got := tc.notification.IsLiveCatchup(); got != tc.wantCatchup {
				t.Fatalf("IsLiveCatchup() = %t, want %t", got, tc.wantCatchup)
			}
		})
	}
}

func TestAlarmTypesValueStaysString(t *testing.T) {
	t.Parallel()

	for name, types := range map[string]domain.AlarmTypes{
		"empty": nil,
		"multi": {domain.AlarmTypeLive, domain.AlarmTypeCommunity},
	} {
		value, err := types.Value()
		if err != nil {
			t.Fatalf("%s: Value() error = %v", name, err)
		}

		if _, ok := value.(string); !ok {
			t.Fatalf("%s: Value() = %T, want string — exec 모드에서 []byte는 bytea(\\x hex)로 인코딩되어 alarm_type[] 파싱이 깨진다", name, value)
		}
	}
}
