package botrooms

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	irisroomscontracts "github.com/kapu/hololive-shared/pkg/contracts/irisrooms"
	"github.com/park285/iris-client-go/iris"
)

func TestClientGetRoomsSuccess(t *testing.T) {
	t.Parallel()

	roomType := "OM"
	roomName := "운영방"
	var gotPath, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(commoncontracts.APIKeyHeader)
		if err := json.NewEncoder(w).Encode(iris.RoomListResponse{Rooms: []iris.RoomSummary{
			{ChatID: 123, Type: &roomType, LinkName: &roomName},
		}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "secret", nil)
	got, err := client.GetRooms(t.Context())
	if err != nil {
		t.Fatalf("GetRooms() error = %v", err)
	}

	if gotPath != irisroomscontracts.ListPath {
		t.Fatalf("path = %q, want %q", gotPath, irisroomscontracts.ListPath)
	}
	if gotAPIKey != "secret" {
		t.Fatalf("%s = %q, want secret", commoncontracts.APIKeyHeader, gotAPIKey)
	}
	if got == nil || len(got.Rooms) != 1 || got.Rooms[0].ChatID != 123 {
		t.Fatalf("rooms = %+v, want chatId 123", got)
	}
}

func TestClientGetRoomsNon2xx(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	client := NewClient(server.URL, "", nil)
	_, err := client.GetRooms(t.Context())
	if err == nil {
		t.Fatal("GetRooms() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %q, want status 502", err.Error())
	}
}
