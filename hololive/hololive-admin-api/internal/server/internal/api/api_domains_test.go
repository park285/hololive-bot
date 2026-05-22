package api

import (
	"testing"
)

func TestDomainHandlers_FromValidHandler(t *testing.T) {
	base := &Handler{logger: newDiscardLogger()}
	domains := base.DomainHandlers()

	if domains == nil {
		t.Fatal("DomainHandlers returned nil")
	}

	if domains.Member == nil || domains.Member.Handler == nil {
		t.Fatal("Member domain handler not initialized")
	}
	if domains.Alarm == nil || domains.Alarm.Handler == nil {
		t.Fatal("Alarm domain handler not initialized")
	}
	if domains.Room == nil || domains.Room.Handler == nil {
		t.Fatal("Room domain handler not initialized")
	}
	if domains.Stream == nil || domains.Stream.Handler == nil {
		t.Fatal("Stream domain handler not initialized")
	}
	if domains.Stats == nil || domains.Stats.Handler == nil {
		t.Fatal("Stats domain handler not initialized")
	}
	if domains.Settings == nil || domains.Settings.Handler == nil {
		t.Fatal("Settings domain handler not initialized")
	}
	if domains.Template == nil || domains.Template.Handler == nil {
		t.Fatal("Template domain handler not initialized")
	}
	if domains.Milestone == nil || domains.Milestone.Handler == nil {
		t.Fatal("Milestone domain handler not initialized")
	}
	if domains.Profile == nil || domains.Profile.Handler == nil {
		t.Fatal("Profile domain handler not initialized")
	}
	if domains.MajorEvent == nil || domains.MajorEvent.Handler == nil {
		t.Fatal("MajorEvent domain handler not initialized")
	}
	if domains.OAuth == nil || domains.OAuth.Handler == nil {
		t.Fatal("OAuth domain handler not initialized")
	}
}

func TestDomainHandlers_EnsuresDefaults(t *testing.T) {
	base := &Handler{}
	domains := base.DomainHandlers()

	if domains.Stream.streamState == nil {
		t.Fatal("streamState not initialized by ensureDefaults via DomainHandlers")
	}

	if domains.Stream.startTime.IsZero() {
		t.Fatal("startTime not initialized by ensureDefaults via DomainHandlers")
	}
}

func TestDomainHandlers_SharedBaseHandler(t *testing.T) {
	base := &Handler{logger: newDiscardLogger()}
	domains := base.DomainHandlers()

	if domains.Member.Handler != domains.Alarm.Handler {
		t.Fatal("domain handlers do not share the same base Handler")
	}
	if domains.Room.Handler != domains.Stats.Handler {
		t.Fatal("domain handlers do not share the same base Handler")
	}
}
