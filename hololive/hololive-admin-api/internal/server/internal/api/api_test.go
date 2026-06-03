package api

import (
	"testing"
)

func TestNewAPIHandler_AllFieldsAssigned(t *testing.T) {
	h := NewHandler(HandlerDeps{
		Common: CommonDeps{Logger: newDiscardLogger()},
	})

	if h == nil {
		t.Fatal("NewHandler returned nil")
	}

	if h.logger == nil {
		t.Fatal("logger not assigned")
	}

	if h.streamState == nil {
		t.Fatal("streamState not initialized")
	}

	if h.startTime.IsZero() {
		t.Fatal("startTime not set")
	}
}

func TestNewAPIHandler_MemberIndexLoaderNilWhenRepoNil(t *testing.T) {
	h := NewHandler(HandlerDeps{
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
	if got.streamState == nil {
		t.Fatal("streamState not created on nil receiver")
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
	if got.streamState == nil {
		t.Fatal("streamState not created")
	}
}

func TestEnsureDefaults_Idempotent(t *testing.T) {
	h := &Handler{}
	first := h.ensureDefaults()
	ss := first.streamState
	st := first.startTime

	second := first.ensureDefaults()
	if second.streamState != ss {
		t.Fatal("ensureDefaults replaced existing streamState")
	}
	if second.startTime != st {
		t.Fatal("ensureDefaults replaced existing startTime")
	}
}

func TestEnsureStreamState_NilReceiver(t *testing.T) {
	var h *Handler
	ss := h.ensureStreamState()
	if ss == nil {
		t.Fatal("ensureStreamState on nil receiver returned nil")
	}
}

func TestEnsureStreamState_CreatesIfMissing(t *testing.T) {
	h := &Handler{}
	ss := h.ensureStreamState()
	if ss == nil {
		t.Fatal("ensureStreamState returned nil")
	}

	ss2 := h.ensureStreamState()
	if ss != ss2 {
		t.Fatal("ensureStreamState should return same instance on second call")
	}
}

func TestHasCommunityShortsOpsRepository(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var h *Handler
		if h.HasCommunityShortsOpsRepository() {
			t.Fatal("nil receiver should return false")
		}
	})

	t.Run("nil repository", func(t *testing.T) {
		h := &Handler{}
		if h.HasCommunityShortsOpsRepository() {
			t.Fatal("nil communityShortsOps should return false")
		}
	})
}
