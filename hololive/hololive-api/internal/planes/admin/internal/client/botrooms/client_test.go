package botrooms

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/park285/iris-client-go/v3/iris"
	sharedh3 "github.com/park285/shared-go/v2/pkg/h3"

	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	irisroomscontracts "github.com/kapu/hololive-shared/pkg/contracts/irisrooms"
)

func TestClientGetRoomsSuccess(t *testing.T) {
	t.Parallel()

	roomType := "OM"
	roomName := "운영방"

	var gotPath, gotAPIKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(commoncontracts.APIKeyHeader)

		if err := jsonv2.MarshalWrite(w, iris.RoomListResponse{Rooms: []iris.RoomSummary{
			{ChatID: 123, Type: &roomType, LinkName: &roomName},
		}}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(server.URL, "secret", sharedh3.ClientOptions{})
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

	client, err := NewClient(server.URL, "", sharedh3.ClientOptions{})
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

			client, err := NewClient(raw, "", sharedh3.ClientOptions{})
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

			// https transport 구성은 HOLOLIVE_INTERNAL_H3_* env가 필요하므로 여기서는 URL 정책만 확인한다.
			validated, err := validateInternalBotRoomsBaseURL(raw)
			if err != nil {
				t.Fatalf("validateInternalBotRoomsBaseURL(%q) error = %v", raw, err)
			}

			if validated == "" {
				t.Fatalf("validateInternalBotRoomsBaseURL(%q) = empty", raw)
			}
		})
	}
}

// https bot 내부 URL은 H3 전용 서버다. 명시한 H3 options가 없으면 TCP client로 내려가지 않고 오류다
// (stack audit 2026-09-26).
func TestNewClientRequiresInternalH3OptionsForHTTPS(t *testing.T) {
	t.Parallel()

	client, err := NewClient("https://127.0.0.1:30001", "", sharedh3.ClientOptions{})
	if err == nil || client != nil {
		t.Fatalf("NewClient(https) = (%v, %v), want missing internal H3 options error", client, err)
	}

	if !strings.Contains(err.Error(), "HOLOLIVE_INTERNAL_H3_CA_CERT_FILE") {
		t.Fatalf("error = %q, want missing HOLOLIVE_INTERNAL_H3_CA_CERT_FILE", err)
	}

	if client, err := NewClient("http://localhost:30001", "", sharedh3.ClientOptions{}); err != nil || client == nil {
		t.Fatalf("NewClient(http) = (%v, %v), want plain internal client", client, err)
	}
}
