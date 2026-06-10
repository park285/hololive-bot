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

package handlercore

import (
	"context"
	"errors"
	"testing"
)

type fakeSubscriptionPort struct {
	isSubscribed       bool
	isSubscribedErr    error
	subscribeErr       error
	unsubscribeErr     error
	subscribedRoomID   string
	subscribedRoomName string
	unsubscribedRoomID string
}

func (f *fakeSubscriptionPort) IsSubscribed(_ context.Context, _ string) (bool, error) {
	if f.isSubscribedErr != nil {
		return false, f.isSubscribedErr
	}

	return f.isSubscribed, nil
}

func (f *fakeSubscriptionPort) Subscribe(_ context.Context, roomID, roomName string) error {
	if f.subscribeErr != nil {
		return f.subscribeErr
	}

	f.subscribedRoomID = roomID
	f.subscribedRoomName = roomName
	f.isSubscribed = true

	return nil
}

func (f *fakeSubscriptionPort) Unsubscribe(_ context.Context, roomID string) error {
	if f.unsubscribeErr != nil {
		return f.unsubscribeErr
	}

	f.unsubscribedRoomID = roomID
	f.isSubscribed = false

	return nil
}

func TestSubscriptionFlow_AllPaths(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	tests := []struct {
		name      string
		operation string
		port      *fakeSubscriptionPort
		wantHook  string
		wantErr   error
	}{
		{
			name:      "subscribe check error",
			operation: "subscribe",
			port:      &fakeSubscriptionPort{isSubscribedErr: boom},
			wantHook:  "checkError",
			wantErr:   boom,
		},
		{
			name:      "subscribe already subscribed",
			operation: "subscribe",
			port:      &fakeSubscriptionPort{isSubscribed: true},
			wantHook:  "alreadySubscribed",
		},
		{
			name:      "subscribe newly subscribed",
			operation: "subscribe",
			port:      &fakeSubscriptionPort{isSubscribed: false},
			wantHook:  "subscribed",
		},
		{
			name:      "subscribe mutate error",
			operation: "subscribe",
			port:      &fakeSubscriptionPort{isSubscribed: false, subscribeErr: boom},
			wantHook:  "subscribeError",
			wantErr:   boom,
		},
		{
			name:      "unsubscribe check error",
			operation: "unsubscribe",
			port:      &fakeSubscriptionPort{isSubscribedErr: boom},
			wantHook:  "checkError",
			wantErr:   boom,
		},
		{
			name:      "unsubscribe not subscribed",
			operation: "unsubscribe",
			port:      &fakeSubscriptionPort{isSubscribed: false},
			wantHook:  "notSubscribed",
		},
		{
			name:      "unsubscribe newly unsubscribed",
			operation: "unsubscribe",
			port:      &fakeSubscriptionPort{isSubscribed: true},
			wantHook:  "unsubscribed",
		},
		{
			name:      "unsubscribe mutate error",
			operation: "unsubscribe",
			port:      &fakeSubscriptionPort{isSubscribed: true, unsubscribeErr: boom},
			wantHook:  "unsubscribeError",
			wantErr:   boom,
		},
		{
			name:      "status check error",
			operation: "status",
			port:      &fakeSubscriptionPort{isSubscribedErr: boom},
			wantHook:  "checkError",
			wantErr:   boom,
		},
		{
			name:      "status subscribed",
			operation: "status",
			port:      &fakeSubscriptionPort{isSubscribed: true},
			wantHook:  "statusTrue",
		},
		{
			name:      "status unsubscribed",
			operation: "status",
			port:      &fakeSubscriptionPort{isSubscribed: false},
			wantHook:  "statusFalse",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotHook   string
				gotErr    error
				gotStatus bool
				hasStatus bool
				sentinel  = errors.New("sentinel")
				gotRoom   = "room-a"
				roomName  = "room-name"
			)

			record := func(hook string) func(context.Context, error) error {
				return func(_ context.Context, err error) error {
					gotHook = hook
					gotErr = err
					return sentinel
				}
			}
			recordMsg := func(hook string) func(context.Context) error {
				return func(_ context.Context) error {
					gotHook = hook
					return sentinel
				}
			}

			flow := NewSubscriptionFlow(SubscriptionFlowConfig{
				Port:                tc.port,
				OnCheckError:        record("checkError"),
				OnAlreadySubscribed: recordMsg("alreadySubscribed"),
				OnSubscribeError:    record("subscribeError"),
				OnSubscribed:        recordMsg("subscribed"),
				OnNotSubscribed:     recordMsg("notSubscribed"),
				OnUnsubscribeError:  record("unsubscribeError"),
				OnUnsubscribed:      recordMsg("unsubscribed"),
				OnStatus: func(_ context.Context, subscribed bool) error {
					hasStatus = true
					gotStatus = subscribed
					if subscribed {
						gotHook = "statusTrue"
					} else {
						gotHook = "statusFalse"
					}
					return sentinel
				},
			})

			var err error
			switch tc.operation {
			case "subscribe":
				err = flow.Subscribe(context.Background(), gotRoom, roomName)
			case "unsubscribe":
				err = flow.Unsubscribe(context.Background(), gotRoom)
			case "status":
				err = flow.Status(context.Background(), gotRoom)
			default:
				t.Fatalf("unknown operation %q", tc.operation)
			}

			if gotHook != tc.wantHook {
				t.Fatalf("hook = %q, want %q", gotHook, tc.wantHook)
			}

			if err != sentinel {
				t.Fatalf("flow returned %v, want sentinel hook return value", err)
			}

			if tc.wantErr != nil && !errors.Is(gotErr, tc.wantErr) {
				t.Fatalf("error hook received %v, want %v", gotErr, tc.wantErr)
			}

			if tc.wantHook == "statusTrue" && (!hasStatus || !gotStatus) {
				t.Fatalf("status hook not invoked with true")
			}
			if tc.wantHook == "statusFalse" && (!hasStatus || gotStatus) {
				t.Fatalf("status hook not invoked with false")
			}

			if tc.wantHook == "subscribed" && tc.port.subscribedRoomID != gotRoom {
				t.Fatalf("subscribe room id = %q, want %q", tc.port.subscribedRoomID, gotRoom)
			}
			if tc.wantHook == "unsubscribed" && tc.port.unsubscribedRoomID != gotRoom {
				t.Fatalf("unsubscribe room id = %q, want %q", tc.port.unsubscribedRoomID, gotRoom)
			}
		})
	}
}
