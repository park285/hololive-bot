package command

import (
	"testing"
)

func TestMajorEventCommand_Name(t *testing.T) {
	cmd := &MajorEventCommand{}
	if cmd.Name() != "major_event" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "major_event")
	}
}

func TestMajorEventCommand_Description(t *testing.T) {
	cmd := &MajorEventCommand{}
	if cmd.Description() == "" {
		t.Error("Description() should not be empty")
	}
}
