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

	client, err := NewClient(server.URL, "secret", nil)
	if err != nil {
		t.Fatalf("NewClient(%q) error = %v", server.URL, err)
	}
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

	client, err := NewClient(server.URL, "", nil)
	if err != nil {
		t.Fatalf("NewClient(%q) error = %v", server.URL, err)
	}
	_, err = client.GetRooms(t.Context())
	if err == nil {
		t.Fatal("GetRooms() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("error = %q, want status 502", err.Error())
	}
}

func TestNewClientRejectsUnsafeBaseURL(t *testing.T) {
	t.Parallel()

	tests := []string{
		"http://169.254.169.254",
		"https://example.com",
		"ftp://127.0.0.1:30001",
		"https://127.0.0.1:30001/internal",
		"https://127.0.0.1:30001?x=1",
		"https://user:pass@127.0.0.1:30001",
	}

	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			client, err := NewClient(raw, "", nil)
			if err == nil {
				t.Fatalf("NewClient(%q) error = nil, want rejection", raw)
			}
			if client != nil {
				t.Fatalf("NewClient(%q) client = %#v, want nil", raw, client)
			}
		})
	}
}

func TestNewClientAllowsConfiguredInternalHosts(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"http://localhost:30001",
		"https://127.0.0.1:30001",
		"https://[::1]:30001",
		"https://hololive-api:30001",
		"https://bot.internal:3443",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			client, err := NewClient(raw, "", nil)
			if err != nil {
				t.Fatalf("NewClient(%q) error = %v", raw, err)
			}
			if client == nil {
				t.Fatalf("NewClient(%q) client = nil", raw)
			}
		})
	}
}
