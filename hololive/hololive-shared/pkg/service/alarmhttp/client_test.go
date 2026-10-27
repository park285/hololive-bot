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

package alarmhttp

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	var missing *http.Response

	return missing, nil
}

type trackingBody struct {
	closed atomic.Bool
}

func (*trackingBody) Read([]byte) (int, error) {
	return 0, http.ErrBodyReadAfterClose
}

func (b *trackingBody) Close() error {
	b.closed.Store(true)

	return nil
}

type fixedResponseTransport struct {
	response *http.Response
}

func (t fixedResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return t.response, nil
}

func TestClientDoRequestNilResponse(t *testing.T) {
	client := newTestClient("https://alarm.example", "")

	client.httpClient.Transport = nilResponseTransport{}

	resp, err := client.doRequest(t.Context(), http.MethodGet, "/alarms", http.NoBody, false)
	if resp != nil && resp.Body != nil {
		t.Cleanup(func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Errorf("close response body: %v", closeErr)
			}
		})
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil *Response")
}

func TestClientDoRequestClosesRejectedResponse(t *testing.T) {
	body := &trackingBody{}
	client := newTestClient("https://alarm.example", "")

	client.httpClient.Transport = fixedResponseTransport{response: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Header:     make(http.Header),
		Body:       body,
	}}

	resp, err := client.doRequest(t.Context(), http.MethodGet, "/alarms", http.NoBody, false) //nolint:bodyclose // 오류 경로가 body를 닫고 nil response를 반환하는 계약을 검증한다.

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, body.closed.Load())
}

func TestClientRejectsNilAddRequest(t *testing.T) {
	client := newTestClient("https://alarm.example", "")
	added, err := client.AddAlarm(t.Context(), nil)
	require.Error(t, err)
	assert.False(t, added)
}

func TestClientRejectsUnsuccessfulEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, map[string]any{"success": false, "error": "alarm_add_failed", "message": "add failed"}))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(server.URL, "")
	_, err := client.AddAlarm(t.Context(), &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testClientChannelID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "add failed")
}

func TestClientSendsAPIKeyHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "secret-key", r.Header.Get("X-API-Key"))
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, map[string]any{"success": true, "data": map[string]bool{"added": true}}))
	}))
	t.Cleanup(server.Close)

	client := newTestClient(server.URL, "secret-key")
	added, err := client.AddAlarm(t.Context(), &domain.AddAlarmRequest{RoomID: testRoomID, ChannelID: testClientChannelID})
	require.NoError(t, err)
	assert.True(t, added)
}

func TestDecodeAPIEnvelopeRejectsMalformedAndMissingSuccess(t *testing.T) {
	for _, body := range []string{"not-json", `{"data":{"added":true}}`} {
		got, err := decodeAPIEnvelope[contractsalarm.AddResponse]("/test", strings.NewReader(body))
		require.Error(t, err)
		assert.Equal(t, contractsalarm.AddResponse{}, got)
	}
}

func TestClientPutNoDataIgnoresDataAndSurfacesEnvelopeFailure(t *testing.T) {
	t.Parallel()

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, map[string]any{"success": true, "data": map[string]string{"ignored": "value"}}))
	}))
	t.Cleanup(ok.Close)
	require.NoError(t, newTestClient(ok.URL, "").SetRoomName(t.Context(), testRoomID, "Room One"))

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, jsonv2.MarshalWrite(w, map[string]any{"success": false, "message": "rename failed"}))
	}))
	t.Cleanup(failing.Close)

	err := newTestClient(failing.URL, "").SetRoomName(t.Context(), testRoomID, "Room One")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rename failed")
}
