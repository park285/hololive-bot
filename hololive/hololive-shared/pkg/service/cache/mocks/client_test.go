package mocks

import (
	"context"
	"testing"
	"time"
)

func TestClientCloseDefaultsToNoopWhenNotStrict(t *testing.T) {
	client := NewLenientClient()

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestClientIsConnectedDefaultsToFalseWhenNotStrict(t *testing.T) {
	client := NewLenientClient()

	if client.IsConnected(context.Background()) {
		t.Fatal("IsConnected() = true, want false")
	}
}

func TestClientClosePanicsWhenStrict(t *testing.T) {
	client := NewStrictClient()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	_ = client.Close()
}

func TestClientReadMethodsDefaultToZeroValuesWhenLenient(t *testing.T) {
	client := NewLenientClient()

	members, err := client.SMembers(context.Background(), "rooms")
	if err != nil {
		t.Fatalf("SMembers() error = %v, want nil", err)
	}
	if members != nil {
		t.Fatalf("SMembers() = %v, want nil", members)
	}

	exists, err := client.Exists(context.Background(), "rooms")
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}
	if exists {
		t.Fatal("Exists() = true, want false")
	}

	allMembers, err := client.GetAllMembers(context.Background())
	if err != nil {
		t.Fatalf("GetAllMembers() error = %v, want nil", err)
	}
	if allMembers != nil {
		t.Fatalf("GetAllMembers() = %v, want nil", allMembers)
	}

	streams, found := client.GetStreams(context.Background(), "streams")
	if found {
		t.Fatal("GetStreams() found = true, want false")
	}
	if streams != nil {
		t.Fatalf("GetStreams() = %v, want nil", streams)
	}

	if err := client.WaitUntilReady(context.Background(), time.Second); err != nil {
		t.Fatalf("WaitUntilReady() error = %v, want nil", err)
	}

	if got := client.GetClient(); got != nil {
		t.Fatalf("GetClient() = %v, want nil", got)
	}

	if acquired, err := client.SetNX(context.Background(), "k", "v", time.Second); err != nil || acquired {
		t.Fatalf("SetNX() = (%v, %v), want (false, nil)", acquired, err)
	}

	if results := client.DoMulti(context.Background()); results != nil {
		t.Fatalf("DoMulti() = %v, want nil", results)
	}

	if deleted, err := client.CompareAndDelete(context.Background(), "k", "v"); err != nil || deleted {
		t.Fatalf("CompareAndDelete() = (%v, %v), want (false, nil)", deleted, err)
	}

	if expired, err := client.CompareAndExpire(context.Background(), "k", "v", time.Second); err != nil || expired {
		t.Fatalf("CompareAndExpire() = (%v, %v), want (false, nil)", expired, err)
	}

	if err := client.InitializeMemberDatabase(context.Background(), map[string]string{"mio": "ch"}); err != nil {
		t.Fatalf("InitializeMemberDatabase() error = %v, want nil", err)
	}

	if err := client.AddMember(context.Background(), "mio", "ch"); err != nil {
		t.Fatalf("AddMember() error = %v, want nil", err)
	}

	client.SetStreams(context.Background(), "streams", nil, time.Second)
}

func TestClientReadMethodsPanicWhenStrict(t *testing.T) {
	client := NewStrictClient()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	_, _ = client.SMembers(context.Background(), "rooms")
}
