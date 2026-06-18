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

	json "github.com/park285/shared-go/pkg/json"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
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

func TestAlarmHTTPRouteContracts(t *testing.T) {
	t.Parallel()

	if contractsalarm.BasePath != "/internal/alarm" {
		t.Fatalf("BasePath = %q, want /internal/alarm", contractsalarm.BasePath)
	}
	if contractsalarm.AddPath != "/internal/alarm/add" {
		t.Fatalf("AddPath = %q, want /internal/alarm/add", contractsalarm.AddPath)
	}
	if contractsalarm.RemovePath != "/internal/alarm/remove" {
		t.Fatalf("RemovePath = %q, want /internal/alarm/remove", contractsalarm.RemovePath)
	}
	if got := contractsalarm.RoomAlarmsPath("room1"); got != "/internal/alarm/room/room1" {
		t.Fatalf("RoomAlarmsPath() = %q, want /internal/alarm/room/room1", got)
	}
	if got := contractsalarm.RoomAlarmsViewPath("room1"); got != "/internal/alarm/room/room1/view" {
		t.Fatalf("RoomAlarmsViewPath() = %q, want /internal/alarm/room/room1/view", got)
	}
	if got := contractsalarm.NextStreamPath("ch1"); got != "/internal/alarm/next-stream/ch1" {
		t.Fatalf("NextStreamPath() = %q, want /internal/alarm/next-stream/ch1", got)
	}
	if contractsalarm.SettingsPath != "/internal/alarm/settings" {
		t.Fatalf("SettingsPath = %q, want /internal/alarm/settings", contractsalarm.SettingsPath)
	}
	if contractsalarm.RoomNamePath != "/internal/alarm/room-name" {
		t.Fatalf("RoomNamePath = %q, want /internal/alarm/room-name", contractsalarm.RoomNamePath)
	}
	if contractsalarm.UserNamePath != "/internal/alarm/user-name" {
		t.Fatalf("UserNamePath = %q, want /internal/alarm/user-name", contractsalarm.UserNamePath)
	}
	if contractsalarm.KeysPath != "/internal/alarm/keys" {
		t.Fatalf("KeysPath = %q, want /internal/alarm/keys", contractsalarm.KeysPath)
	}
}

func TestAlarmHTTPRouteContractsEscapePathParams(t *testing.T) {
	t.Parallel()

	if got := contractsalarm.RoomAlarmsPath("room/a b"); got != "/internal/alarm/room/room%2Fa%20b" {
		t.Fatalf("RoomAlarmsPath() = %q, want /internal/alarm/room/room%%2Fa%%20b", got)
	}
	if got := contractsalarm.RoomAlarmsViewPath("room/a b"); got != "/internal/alarm/room/room%2Fa%20b/view" {
		t.Fatalf("RoomAlarmsViewPath() = %q, want /internal/alarm/room/room%%2Fa%%20b/view", got)
	}
	if got := contractsalarm.NextStreamPath("ch/a b"); got != "/internal/alarm/next-stream/ch%2Fa%20b" {
		t.Fatalf("NextStreamPath() = %q, want /internal/alarm/next-stream/ch%%2Fa%%20b", got)
	}
}

func TestAlarmQueueEnvelopeContract(t *testing.T) {
	t.Parallel()

	env := domain.AlarmQueueEnvelope{
		DispatchOutboxID: 99,
		Notification: domain.AlarmNotification{
			AlarmType: domain.AlarmTypeLive,
			RoomID:    "room-contract",
		},
		SourceKind: domain.AlarmDispatchSourceKindYouTubeOutbox,
		YouTubeOutbox: &domain.YouTubeOutboxDispatchPayload{
			OutboxIDs: []int64{99},
			Kind:      domain.OutboxKindNewVideo,
			AlarmType: domain.AlarmTypeLive,
			ChannelID: "UC_contract",
			Items: []domain.YouTubeOutboxItem{{
				OutboxID:  99,
				ContentID: "video:contract",
				Payload:   `{"video_id":"contract"}`,
			}},
		},
		Version:    contractsalarm.QueueEnvelopeVersionV1,
		ClaimKeys:  []string{"notified:claim:room-contract"},
		EnqueuedAt: "2026-02-25T13:00:00Z",
		Retry: &domain.AlarmQueueRetryMetadata{
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
	if env.DispatchOutboxID != 99 {
		t.Fatalf("dispatch_outbox_id = %d, want 99", env.DispatchOutboxID)
	}
	if env.SourceKind != domain.AlarmDispatchSourceKindYouTubeOutbox {
		t.Fatalf("source_kind = %q, want %q", env.SourceKind, domain.AlarmDispatchSourceKindYouTubeOutbox)
	}
	if env.YouTubeOutbox == nil {
		t.Fatal("youtube_outbox should be set")
	}
}

func TestAlarmQueueEnvelopeV1FixtureRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readAlarmContractFixture(t, "envelope_v1.json")

	var envelope domain.AlarmQueueEnvelope
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

	if envelope.DispatchOutboxID != 42 {
		t.Fatalf("dispatch_outbox_id = %d, want 42", envelope.DispatchOutboxID)
	}
	if envelope.SourceKind != domain.AlarmDispatchSourceKindYouTubeOutbox {
		t.Fatalf("source_kind = %q, want %q", envelope.SourceKind, domain.AlarmDispatchSourceKindYouTubeOutbox)
	}
	if envelope.YouTubeOutbox == nil {
		t.Fatal("youtube_outbox should be non-nil")
	}
	if envelope.YouTubeOutbox.ChannelID != "UC_fixture" {
		t.Fatalf("youtube_outbox.channel_id = %q, want UC_fixture", envelope.YouTubeOutbox.ChannelID)
	}
	if envelope.SourcePayload() != `{"version":1}` {
		t.Fatalf("source_payload = %q, want {\"version\":1}", envelope.SourcePayload())
	}
	if err := envelope.ValidateCanonicalDispatch(); err != nil {
		t.Fatalf("ValidateCanonicalDispatch: %v", err)
	}

	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var roundTrip domain.AlarmQueueEnvelope
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal round trip: %v", err)
	}
	if roundTrip.Notification.RoomID != envelope.Notification.RoomID {
		t.Fatalf("roundTrip room_id = %q, want %q", roundTrip.Notification.RoomID, envelope.Notification.RoomID)
	}
	if roundTrip.DispatchOutboxID != envelope.DispatchOutboxID {
		t.Fatalf("roundTrip dispatch_outbox_id = %d, want %d", roundTrip.DispatchOutboxID, envelope.DispatchOutboxID)
	}
	if roundTrip.SourceKind != envelope.SourceKind {
		t.Fatalf("roundTrip source_kind = %q, want %q", roundTrip.SourceKind, envelope.SourceKind)
	}
	if roundTrip.YouTubeOutbox == nil || roundTrip.YouTubeOutbox.ChannelID != envelope.YouTubeOutbox.ChannelID {
		t.Fatalf("roundTrip youtube_outbox mismatch")
	}
	if roundTrip.SourcePayload() != envelope.SourcePayload() {
		t.Fatalf("roundTrip source_payload = %q, want %q", roundTrip.SourcePayload(), envelope.SourcePayload())
	}
}

func TestAlarmQueueRetryMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	raw := readAlarmContractFixture(t, "envelope_v1.json")

	var envelope domain.AlarmQueueEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	encoded, err := json.Marshal(envelope.Retry)
	if err != nil {
		t.Fatalf("marshal retry metadata: %v", err)
	}
	var retry domain.AlarmQueueRetryMetadata
	if err := json.Unmarshal(encoded, &retry); err != nil {
		t.Fatalf("unmarshal retry metadata: %v", err)
	}
	if retry.RetryAfterMS != 30000 {
		t.Fatalf("retry_after_ms = %d, want 30000", retry.RetryAfterMS)
	}
	if retry.LastError != "temporary upstream error" {
		t.Fatalf("last_error = %q, want temporary upstream error", retry.LastError)
	}
	if retry.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2", retry.Attempt)
	}
	if retry.NextVisibleAt != "2026-02-25T13:00:30Z" {
		t.Fatalf("next_visible_at = %q, want 2026-02-25T13:00:30Z", retry.NextVisibleAt)
	}
}

// version-0(legacy)는 domain unmarshal 레이어에서 거부되지 않고 파싱된다.
// consumer 수용 분기(parseEnvelope의 case 0)는 queue 패키지 테스트가 담당한다.
func TestAlarmQueueEnvelopeVersionZeroParsesAtDomainLayer(t *testing.T) {
	t.Parallel()

	versionZeroJSON := `{
		"notification": {
			"room_id": "room-legacy",
			"channel": null,
			"stream": null,
			"minutes_until": 10,
			"users": ["user-a"]
		},
		"claim_keys": ["notified:claim:room-legacy"],
		"enqueued_at": "2026-02-25T13:00:00Z",
		"version": 0
	}`

	var env domain.AlarmQueueEnvelope
	if err := json.Unmarshal([]byte(versionZeroJSON), &env); err != nil {
		t.Fatalf("unmarshal version-0 envelope: %v", err)
	}
	if env.Version != 0 {
		t.Fatalf("Version = %d, want 0", env.Version)
	}
	if env.Notification.RoomID != "room-legacy" {
		t.Fatalf("RoomID = %q, want room-legacy", env.Notification.RoomID)
	}
	if len(env.ClaimKeys) != 1 {
		t.Fatalf("ClaimKeys len = %d, want 1", len(env.ClaimKeys))
	}
}

func readAlarmContractFixture(t *testing.T, name string) []byte {
	t.Helper()

	var raw []byte
	var err error
	switch name {
	case "envelope_unsupported_version.json":
		raw, err = os.ReadFile(filepath.Join("testdata", "envelope_unsupported_version.json"))
	case "envelope_v1.json":
		raw, err = os.ReadFile(filepath.Join("testdata", "envelope_v1.json"))
	default:
		t.Fatalf("fixture %s is not allowed", name)
	}
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}
