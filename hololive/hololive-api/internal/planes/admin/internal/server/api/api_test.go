package api

import (
	"testing"
)

func TestNewAPIHandler_AllFieldsAssigned(t *testing.T) {
	h := NewHandler(&HandlerDeps{
		Common: CommonDeps{Logger: newDiscardLogger()},
	})

	if h == nil {
		t.Fatal("NewHandler returned nil")
	}

	if h.logger == nil {
		t.Fatal("logger not assigned")
	}

	if h.startTime.IsZero() {
		t.Fatal("startTime not set")
	}
}

func TestNewAPIHandler_MemberIndexLoaderNilWhenRepoNil(t *testing.T) {
	h := NewHandler(&HandlerDeps{
		Common: CommonDeps{Logger: newDiscardLogger()},
	})

	if h.memberIndexLoader != nil {
		t.Fatal("memberIndexLoader should be nil when repository is nil")
	}
}

func TestEnsureDefaults_NilReceiver(t *testing.T) {
	var h *Handler

	got := h.ensureDefaults()

	if got == nil {
		t.Fatal("ensureDefaults on nil receiver returned nil")
	}

	if got.startTime.IsZero() {
		t.Fatal("startTime not set on nil receiver")
	}
}

func TestEnsureDefaults_PreservesExistingFields(t *testing.T) {
	logger := newDiscardLogger()
	h := &Handler{logger: logger}
	got := h.ensureDefaults()

	if got.logger != logger {
		t.Fatal("ensureDefaults overwrote existing logger")
	}
}

func TestEnsureDefaults_Idempotent(t *testing.T) {
	h := &Handler{}
	first := h.ensureDefaults()
	st := first.startTime

	second := first.ensureDefaults()
	if !second.startTime.Equal(st) {
		t.Fatal("ensureDefaults replaced existing startTime")
	}
}
