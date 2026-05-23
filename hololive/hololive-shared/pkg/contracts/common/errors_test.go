package common_test

import (
	"testing"

	json "github.com/park285/hololive-bot/shared-go/pkg/json"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
)

func TestErrorResponseCompatibilityShape(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(commoncontracts.ErrorResponse{Error: "notification_in_progress"})
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}
	if string(payload) != `{"error":"notification_in_progress"}` {
		t.Fatalf("payload = %s, want compatibility shape", payload)
	}
}

func TestErrorResponseAdditiveFields(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"error":"no_subscribed_members",
		"message":"no subscribed members",
		"request_id":"req-1",
		"details":{"room_id":"room-1"}
	}`)

	var response commoncontracts.ErrorResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if response.Error != "no_subscribed_members" {
		t.Fatalf("Error = %q, want no_subscribed_members", response.Error)
	}
	if response.Message != "no subscribed members" {
		t.Fatalf("Message = %q, want no subscribed members", response.Message)
	}
	if response.RequestID != "req-1" {
		t.Fatalf("RequestID = %q, want req-1", response.RequestID)
	}
	if response.Details["room_id"] != "room-1" {
		t.Fatalf("Details[room_id] = %v, want room-1", response.Details["room_id"])
	}
}
