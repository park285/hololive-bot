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

package holodexprovider

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
