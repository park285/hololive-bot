package bootstrap

import (
	"log/slog"
	"testing"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func TestBotMessageStringRequirementsSatisfiedBySeed(t *testing.T) {
	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	if err := store.Load(t.Context()); err != nil {
		t.Fatalf("load message strings: %v", err)
	}

	if err := store.Validate(botMessageStringRequirements()); err != nil {
		t.Fatalf("Validate(bot plane) error = %v", err)
	}
}

func TestBotMessageStringRequirementsCoverErrorAndCalendarKeys(t *testing.T) {
	requirements := botMessageStringRequirements()

	want := map[messagestrings.Key]bool{
		{Namespace: messagestrings.NamespaceError, Name: "unknown_command"}:           false,
		{Namespace: messagestrings.NamespaceError, Name: "command_processing_failed"}: false,
		messagestrings.CalendarUnknown:                                                false,
	}

	for _, key := range requirements.Keys {
		lookup := messagestrings.Key{Namespace: key.Namespace, Name: key.Name}
		if _, ok := want[lookup]; ok {
			want[lookup] = true
		}
	}

	for key, found := range want {
		if !found {
			t.Errorf("bot plane requirements miss %s", key)
		}
	}
}
