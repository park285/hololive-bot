package membernews

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/pkg/promptguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/guardrail"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
)

func filterPromptCandidates(candidates []model.FilteredCandidate, guard *promptguard.Guard, logger *slog.Logger) ([]model.FilteredCandidate, error) {
	if len(candidates) == 0 {
		return candidates, nil
	}
	if guard == nil {
		return nil, promptguard.ErrGuardUnavailable
	}
	if logger == nil {
		logger = slog.Default()
	}

	filtered := make([]model.FilteredCandidate, 0, len(candidates))
	for i := range candidates {
		candidate := &candidates[i]
		parts := make([]string, 0, 3+len(candidate.Candidate.Members))
		parts = append(parts, candidate.Candidate.Title, candidate.Candidate.Description, candidate.SourceURL)
		parts = append(parts, candidate.Candidate.Members...)
		evaluation, err := guardrail.CheckExternalContent(guard, parts...)
		if err == nil {
			filtered = append(filtered, *candidate)
			continue
		}

		var blocked *promptguard.BlockedError
		if !errors.As(err, &blocked) {
			return nil, fmt.Errorf("check member news candidate: %w", err)
		}

		guardrail.RecordBlock("prompt", "membernews_candidate")
		logger.Warn("Member news prompt candidate blocked",
			slog.Int("candidate_id", candidate.Candidate.ID),
			slog.String("decision", string(evaluation.Decision)),
			slog.Any("rules", blocked.Rules),
		)
	}
	return filtered, nil
}
