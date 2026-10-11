package notificationdelivery

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPayloadPreservesStoredWireBytes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload Payload
		wire    string
	}{
		{
			name:    "producer message",
			payload: Payload{Message: "알림\n<&>"},
			wire:    `{"message":"알림\n<&>"}`,
		},
		{
			name:    "empty producer message remains present",
			payload: Payload{},
			wire:    `{"message":""}`,
		},
		{
			name:    "preparation failure evidence",
			payload: Payload{KnownUnsent: true, Message: "source"},
			wire:    `{"known_unsent":true,"message":"source"}`,
		},
		{
			name: "pinned initial request",
			payload: Payload{Message: "source", Request: &Request{
				Body: `{"text":"알림"}`, Route: "markdown", BaseID: "delivery:1",
			}},
			wire: `{"message":"source","request":{"body":"{\"text\":\"알림\"}","route":"markdown","base_id":"delivery:1","generation":0}}`,
		},
		{
			name: "exhausted request retains preparation evidence",
			payload: Payload{KnownUnsent: true, Message: "source", Request: &Request{
				Body: "pinned", Route: "text", BaseID: "delivery:1", Generation: 2, Exhausted: true,
			}},
			wire: `{"known_unsent":true,"message":"source","request":{"body":"pinned","route":"text","base_id":"delivery:1","generation":2,"exhausted":true}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := jsonv2.Marshal(tc.payload)
			require.NoError(t, err)
			require.Equal(t, tc.wire, string(raw))

			var restored Payload

			require.NoError(t, jsonv2.Unmarshal([]byte(tc.wire), &restored))
			require.Equal(t, tc.payload, restored)
		})
	}
}

func TestRequestPreservesCASBytes(t *testing.T) {
	var previous *Request

	previousJSON, err := jsonv2.Marshal(previous)
	require.NoError(t, err)
	require.JSONEq(t, "null", string(previousJSON))

	request := Request{Body: "body\n", Route: "text", BaseID: "delivery:1"}
	raw, err := jsonv2.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, `{"body":"body\n","route":"text","base_id":"delivery:1","generation":0}`, string(raw))
}
