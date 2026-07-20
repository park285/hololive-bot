package scheduler

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/pkg/promptguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/guardrail"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func filterPromptEvents(events []*domain.MajorEvent, guard *promptguard.Guard, logger *slog.Logger) ([]*domain.MajorEvent, error) {
	if len(events) == 0 {
		return events, nil
	}
	if guard == nil {
		return nil, promptguard.ErrGuardUnavailable
	}
	if logger == nil {
		logger = slog.Default()
	}

	filtered := make([]*domain.MajorEvent, 0, len(events))
	for i := range events {
		event := events[i]
		allowed, err := majorEventPromptAllowed(event, guard, logger)
		if err != nil {
			return nil, err
		}
		if allowed {
			filtered = append(filtered, event)
		}
	}
	return filtered, nil
}

func majorEventPromptAllowed(event *domain.MajorEvent, guard *promptguard.Guard, logger *slog.Logger) (bool, error) {
	if event == nil {
		return false, nil
	}

	parts := make([]string, 0, 3+len(event.Members))
	parts = append(parts, event.Title, event.Description, event.Link)
	parts = append(parts, event.Members...)
	evaluation, err := guardrail.CheckExternalContent(guard, parts...)
	if err == nil {
		return true, nil
	}

	var blocked *promptguard.BlockedError
	if !errors.As(err, &blocked) {
		return false, fmt.Errorf("check major event: %w", err)
	}

	guardrail.RecordBlock("prompt", "majorevent_candidate")
	logger.Warn("Major event prompt candidate blocked",
		slog.Int("event_id", event.ID),
		slog.String("decision", string(evaluation.Decision)),
		slog.Any("rules", blocked.Rules),
	)
	return false, nil
}
