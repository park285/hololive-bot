package orchcmd

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCommandRouterLogsUncertainReplyAsUnknown(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		cause error
		event string
		level slog.Level
	}{
		{name: "handoff unknown", cause: transport.ErrReplyOutcomeUnknown, event: EventBotReplyOutcomeUnknown, level: slog.LevelWarn},
		{name: "staging receipt lost", cause: transport.ErrReplyStagingFailed, event: EventBotReplyOutcomeUnknown, level: slog.LevelWarn},
		{name: "wrapped staging error", cause: fmt.Errorf("stage reply: %w", transport.ErrReplyStagingFailed), event: EventBotReplyOutcomeUnknown, level: slog.LevelWarn},
		{name: "backend failure", cause: errors.New("backend unavailable"), event: EventBotCommandExecuteFailed, level: slog.LevelError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer

			logger := slog.New(slog.NewJSONHandler(&output, nil))
			registry := command.NewRegistry()
			registry.Register(&failingRouterCommand{name: string(domain.CommandHelp), err: testCase.cause})

			router := NewCommandRouter(registry, logger, func(context.Context, string, string) error { return nil }, nil, nil)
			err := router.Execute(t.Context(), newPrivacyCommandContext(), domain.CommandHelp, nil)
			require.ErrorIs(t, err, testCase.cause)

			found := false

			for line := range strings.SplitSeq(strings.TrimSpace(output.String()), "\n") {
				var record struct {
					Level string `json:"level"`
					Event string `json:"event"`
				}

				require.NoError(t, jsonv2.Unmarshal([]byte(line), &record))

				if testCase.event == EventBotReplyOutcomeUnknown {
					require.NotEqual(t, EventBotCommandExecuteFailed, record.Event, "uncertainty must not enter command failure logs")
				}

				if record.Event == testCase.event {
					found = true

					require.Equal(t, testCase.level.String(), record.Level)
				}
			}

			require.True(t, found, "terminal reply event missing from actual logger output: %s", output.String())
		})
	}
}
