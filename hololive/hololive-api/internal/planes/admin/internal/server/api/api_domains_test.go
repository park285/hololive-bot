package api

import (
	"reflect"
	"testing"
)

func TestDomainHandlers_FromValidHandler(t *testing.T) {
	base := &Handler{logger: newDiscardLogger()}
	domains := base.DomainHandlers()

	if domains == nil {
		t.Fatal("DomainHandlers returned nil")
	}

	assertDomainHandlerInitialized(t, "Member", domains.Member)
	assertDomainHandlerInitialized(t, "Alarm", domains.Alarm)
	assertDomainHandlerInitialized(t, "Room", domains.Room)
	assertDomainHandlerInitialized(t, "Stream", domains.Stream)
	assertDomainHandlerInitialized(t, "Stats", domains.Stats)
	assertDomainHandlerInitialized(t, "Settings", domains.Settings)
	assertDomainHandlerInitialized(t, "Template", domains.Template)
	assertDomainHandlerInitialized(t, "Profile", domains.Profile)
	assertDomainHandlerInitialized(t, "MajorEvent", domains.MajorEvent)
	assertDomainHandlerInitialized(t, "OAuth", domains.OAuth)
}

func assertDomainHandlerInitialized(t *testing.T, name string, domainHandler any) {
	t.Helper()

	value := reflect.ValueOf(domainHandler)
	if !value.IsValid() || value.IsNil() {
		t.Fatalf("%s domain handler not initialized", name)
	}
	if handler := value.Elem().FieldByName("Handler"); !handler.IsValid() || handler.IsNil() {
		t.Fatalf("%s domain handler base not initialized", name)
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
