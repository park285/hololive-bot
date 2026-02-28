package holodex

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// MockRequester simulates Holodex API interactions
type MockRequester struct {
	DoRequestFunc func(ctx context.Context, method, path string, params url.Values) ([]byte, error)
}

func (m *MockRequester) DoRequest(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
	if m.DoRequestFunc != nil {
		return m.DoRequestFunc(ctx, method, path, params)
	}
	return nil, nil
}

func (m *MockRequester) IsCircuitOpen() bool {
	return false
}

func TestRequester_BatchParamVerification(t *testing.T) {
	mockReq := &MockRequester{}
	mockReq.DoRequestFunc = func(ctx context.Context, method, path string, params url.Values) ([]byte, error) {
		if path != "/users/live" {
			t.Errorf("Expected path /users/live, got %s", path)
		}

		if method != "GET" {
			t.Errorf("Expected method GET, got %s", method)
		}

		channels := params.Get("channels")
		if !strings.Contains(channels, "ch1") || !strings.Contains(channels, "ch2") {
			t.Errorf("Expected channels param to contain ch1,ch2, got %s", channels)
		}

		return []byte(`[]`), nil
	}

	ctx := context.Background()
	channelIDs := []string{"ch1", "ch2"}

	params := url.Values{}
	params.Set("channels", strings.Join(channelIDs, ","))

	_, err := mockReq.DoRequest(ctx, "GET", "/users/live", params)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}
