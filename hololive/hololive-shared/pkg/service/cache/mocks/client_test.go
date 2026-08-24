package mocks

import (
	"testing"
	"time"
)

func TestZeroValueClientIsStrict(t *testing.T) {
	client := &Client{}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	if _, err := client.Exists(t.Context(), "rooms"); err != nil {
		t.Fatalf("Exists() error = %v", err)
	}
}

func TestClientCloseDefaultsToNoopWhenNotStrict(t *testing.T) {
	client := NewLenientClient()

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestClientIsConnectedDefaultsToFalseWhenNotStrict(t *testing.T) {
	client := NewLenientClient()

	if client.IsConnected(t.Context()) {
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

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClientReadMethodsDefaultToZeroValuesWhenLenient(t *testing.T) {
	client := NewLenientClient()

	members, err := client.SMembers(t.Context(), "rooms")
	if err != nil {
		t.Fatalf("SMembers() error = %v, want nil", err)
	}

	if members != nil {
		t.Fatalf("SMembers() = %v, want nil", members)
	}

	exists, err := client.Exists(t.Context(), "rooms")
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}

	if exists {
		t.Fatal("Exists() = true, want false")
	}

	allMembers, err := client.GetAllMembers(t.Context())
	if err != nil {
		t.Fatalf("GetAllMembers() error = %v, want nil", err)
	}

	if allMembers != nil {
		t.Fatalf("GetAllMembers() = %v, want nil", allMembers)
	}

	streams, found := client.GetStreams(t.Context(), "streams")
	if found {
		t.Fatal("GetStreams() found = true, want false")
	}

	if streams != nil {
		t.Fatalf("GetStreams() = %v, want nil", streams)
	}
}

func TestClientLowLevelMethodsDefaultToZeroValuesWhenLenient(t *testing.T) {
	client := NewLenientClient()

	if err := client.WaitUntilReady(t.Context(), time.Second); err != nil {
		t.Fatalf("WaitUntilReady() error = %v, want nil", err)
	}

	if got := client.GetClient(); got != nil {
		t.Fatalf("GetClient() = %v, want nil", got)
	}

	if acquired, err := client.SetNX(t.Context(), "k", "v", time.Second); err != nil || acquired {
		t.Fatalf("SetNX() = (%v, %v), want (false, nil)", acquired, err)
	}

	if results := client.DoMulti(t.Context()); results != nil {
		t.Fatalf("DoMulti() = %v, want nil", results)
	}
}

func TestClientScriptAndMemberMethodsDefaultToZeroValuesWhenLenient(t *testing.T) {
	client := NewLenientClient()

	if deleted, err := client.CompareAndDelete(t.Context(), "k", "v"); err != nil || deleted {
		t.Fatalf("CompareAndDelete() = (%v, %v), want (false, nil)", deleted, err)
	}

	if expired, err := client.CompareAndExpire(t.Context(), "k", "v", time.Second); err != nil || expired {
		t.Fatalf("CompareAndExpire() = (%v, %v), want (false, nil)", expired, err)
	}

	if err := client.InitializeMemberDatabase(t.Context(), map[string]string{"mio": "ch"}); err != nil {
		t.Fatalf("InitializeMemberDatabase() error = %v, want nil", err)
	}

	client.SetStreams(t.Context(), "streams", nil, time.Second)
}

func TestClientReadMethodsPanicWhenStrict(t *testing.T) {
	client := NewStrictClient()

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	if _, err := client.SMembers(t.Context(), "rooms"); err != nil {
		t.Fatalf("SMembers() error = %v", err)
	}
}

func TestNewLenientClientDoesNotPanicOnUnsetExists(t *testing.T) {
	client := NewLenientClient()

	exists, err := client.Exists(t.Context(), "rooms")
	if err != nil {
		t.Fatalf("Exists() error = %v, want nil", err)
	}

	if exists {
		t.Fatal("Exists() = true, want false")
	}
}

func TestClientWriteMethodsDefaultToZeroValuesWhenLenient(t *testing.T) {
	client := NewLenientClient()

	if err := client.Set(t.Context(), "k", "v", time.Second); err != nil {
		t.Fatalf("Set() error = %v, want nil", err)
	}

	if err := client.MSet(t.Context(), map[string]any{"k": "v"}, time.Second); err != nil {
		t.Fatalf("MSet() error = %v, want nil", err)
	}

	if err := client.Del(t.Context(), "k"); err != nil {
		t.Fatalf("Del() error = %v, want nil", err)
	}

	if deleted, err := client.DelMany(t.Context(), []string{"k1", "k2"}); err != nil || deleted != 0 {
		t.Fatalf("DelMany() = (%v, %v), want (0, nil)", deleted, err)
	}

	if added, err := client.SAdd(t.Context(), "rooms", []string{"r1"}); err != nil || added != 0 {
		t.Fatalf("SAdd() = (%v, %v), want (0, nil)", added, err)
	}

	if removed, err := client.SRem(t.Context(), "rooms", []string{"r1"}); err != nil || removed != 0 {
		t.Fatalf("SRem() = (%v, %v), want (0, nil)", removed, err)
	}

	if err := client.HSet(t.Context(), "rooms", "name", "mio"); err != nil {
		t.Fatalf("HSet() error = %v, want nil", err)
	}

	if err := client.HMSet(t.Context(), "rooms", map[string]any{"name": "mio"}); err != nil {
		t.Fatalf("HMSet() error = %v, want nil", err)
	}

	if err := client.HDel(t.Context(), "rooms", "name"); err != nil {
		t.Fatalf("HDel() error = %v, want nil", err)
	}

	if err := client.Expire(t.Context(), "rooms", time.Second); err != nil {
		t.Fatalf("Expire() error = %v, want nil", err)
	}
}
