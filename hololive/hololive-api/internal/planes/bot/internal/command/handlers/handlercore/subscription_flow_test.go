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
	"fmt"
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

type subscriptionFlowTestCase struct {
	name      string
	operation string
	port      *fakeSubscriptionPort
	wantHook  string
	wantErr   error
}

func TestSubscriptionFlow_AllPaths(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	tests := []subscriptionFlowTestCase{
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
			assertSubscriptionFlowCase(t, &tc)
		})
	}
}

type subscriptionFlowCaseRunner struct {
	gotHook   string
	gotErr    error
	gotStatus bool
	hasStatus bool
	sentinel  error
	gotRoom   string
}

func assertSubscriptionFlowCase(t *testing.T, tc *subscriptionFlowTestCase) {
	t.Helper()

	runner := &subscriptionFlowCaseRunner{
		sentinel: errors.New("sentinel"),
		gotRoom:  "room-a",
	}
	flow := NewSubscriptionFlow(runner.config(tc.port))
	err := runner.run(flow, tc.operation)
	runner.assertResult(t, tc, err)
}

func (r *subscriptionFlowCaseRunner) config(port *fakeSubscriptionPort) *SubscriptionFlowConfig {
	return &SubscriptionFlowConfig{
		Port:                port,
		OnCheckError:        r.record("checkError"),
		OnAlreadySubscribed: r.recordMessage("alreadySubscribed"),
		OnSubscribeError:    r.record("subscribeError"),
		OnSubscribed:        r.recordMessage("subscribed"),
		OnNotSubscribed:     r.recordMessage("notSubscribed"),
		OnUnsubscribeError:  r.record("unsubscribeError"),
		OnUnsubscribed:      r.recordMessage("unsubscribed"),
		OnStatus:            r.recordStatus,
	}
}

func (r *subscriptionFlowCaseRunner) record(hook string) func(context.Context, error) error {
	return func(_ context.Context, err error) error {
		r.gotHook = hook
		r.gotErr = err
		return r.sentinel
	}
}

func (r *subscriptionFlowCaseRunner) recordMessage(hook string) func(context.Context) error {
	return func(context.Context) error {
		r.gotHook = hook
		return r.sentinel
	}
}

func (r *subscriptionFlowCaseRunner) recordStatus(_ context.Context, subscribed bool) error {
	r.hasStatus = true
	r.gotStatus = subscribed
	if subscribed {
		r.gotHook = "statusTrue"
	} else {
		r.gotHook = "statusFalse"
	}
	return r.sentinel
}

func (r *subscriptionFlowCaseRunner) run(flow SubscriptionFlow, operation string) error {
	switch operation {
	case "subscribe":
		return flow.Subscribe(context.Background(), r.gotRoom, "room-name")
	case "unsubscribe":
		return flow.Unsubscribe(context.Background(), r.gotRoom)
	case "status":
		return flow.Status(context.Background(), r.gotRoom)
	default:
		return fmt.Errorf("unknown operation %q", operation)
	}
}

func (r *subscriptionFlowCaseRunner) assertResult(t *testing.T, tc *subscriptionFlowTestCase, err error) {
	t.Helper()

	if r.gotHook != tc.wantHook {
		t.Fatalf("hook = %q, want %q", r.gotHook, tc.wantHook)
	}
	if !errors.Is(err, r.sentinel) {
		t.Fatalf("flow returned %v, want sentinel hook return value", err)
	}
	if tc.wantErr != nil && !errors.Is(r.gotErr, tc.wantErr) {
		t.Fatalf("error hook received %v, want %v", r.gotErr, tc.wantErr)
	}
	r.assertStatusHook(t, tc.wantHook)
	r.assertRoomMutation(t, tc)
}

func (r *subscriptionFlowCaseRunner) assertStatusHook(t *testing.T, wantHook string) {
	t.Helper()

	if wantHook == "statusTrue" && (!r.hasStatus || !r.gotStatus) {
		t.Fatalf("status hook not invoked with true")
	}
	if wantHook == "statusFalse" && (!r.hasStatus || r.gotStatus) {
		t.Fatalf("status hook not invoked with false")
	}
}

func (r *subscriptionFlowCaseRunner) assertRoomMutation(t *testing.T, tc *subscriptionFlowTestCase) {
	t.Helper()

	if tc.wantHook == "subscribed" && tc.port.subscribedRoomID != r.gotRoom {
		t.Fatalf("subscribe room id = %q, want %q", tc.port.subscribedRoomID, r.gotRoom)
	}
	if tc.wantHook == "unsubscribed" && tc.port.unsubscribedRoomID != r.gotRoom {
		t.Fatalf("unsubscribe room id = %q, want %q", tc.port.unsubscribedRoomID, r.gotRoom)
	}
}
