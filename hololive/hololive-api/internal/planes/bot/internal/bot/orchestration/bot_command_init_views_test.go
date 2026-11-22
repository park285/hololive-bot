package orchestration

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

type stubCommandInitStreamProvider struct{}

func (s *stubCommandInitStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubCommandInitStreamProvider) Stop() {}

type stubCommandInitCalendarFinder struct{}

func (s *stubCommandInitCalendarFinder) FindMembersWithCelebrationsInMonth(context.Context, int, int) ([]domain.CalendarEntry, error) {
	return nil, nil
}

func TestCommandInitView_ToCommandDependencies(t *testing.T) {
	streamProvider := &stubCommandInitStreamProvider{}
	b := &Bot{logger: slog.New(slog.DiscardHandler), postgres: databasemocks.NewUnconnectedClient(t)}

	view := b.commandInitView(&Dependencies{
		Holodex: streamProvider,
	}, nil, nil)

	deps := view.toCommandDependencies()
	if deps == nil {
		t.Fatal("toCommandDependencies() returned nil")
	}

	if deps.Holodex != streamProvider {
		t.Fatal("holodex mapping mismatch")
	}

	if deps.SendMessage == nil || deps.SendImage == nil || deps.SendImages == nil || deps.SendError == nil {
		t.Fatal("send function mappings must not be nil")
	}
}

func TestCommandInitView_AssemblesCommands(t *testing.T) {
	b := &Bot{logger: slog.New(slog.DiscardHandler), postgres: databasemocks.NewUnconnectedClient(t)}
	view := b.commandInitView(&Dependencies{}, &stubCommandInitCalendarFinder{}, nil)

	deps := view.toCommandDependencies()
	commands := view.buildCommands(deps)

	gotNames := make([]string, 0, len(commands))
	for _, cmd := range commands {
		if cmd.Command == nil || cmd.CheckDependencies == nil {
			t.Fatal("buildCommands() returned nil command")
		}

		gotNames = append(gotNames, cmd.Name())
	}

	wantNames := []string{
		testHelpCommandName,
		"live",
		"upcoming",
		"schedule",
		"schedule_poster",
		"event_grade",
		"alarm",
		"member_info",
		"broadcast_history",
		"broadcast_thumbnail",
		"broadcast_time",
		"calendar",
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("command names = %v, want %v", gotNames, wantNames)
	}
}

func TestInitializeCommandsRejectsMissingCommandDependency(t *testing.T) {
	b := &Bot{logger: slog.New(slog.DiscardHandler), postgres: databasemocks.NewUnconnectedClient(t)}

	err := b.initializeCommands(&Dependencies{}, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "initialize command help: ") {
		t.Fatalf("initializeCommands() error = %v, want help dependency error", err)
	}

	if b.commandExecutor != nil {
		t.Fatal("command router must not be built when a command dependency is missing")
	}
}

var (
	_ domain.StreamProvider                 = (*stubCommandInitStreamProvider)(nil)
	_ handlercore.CelebrationCalendarFinder = (*stubCommandInitCalendarFinder)(nil)
)
