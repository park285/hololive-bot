package guardrail

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/pkg/outputguard"
	"github.com/park285/shared-go/pkg/promptguard"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/model"
)

var ErrOutputGuardUnavailable = errors.New("output guard unavailable")

var guardBlocks = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "hololive_llm_guard_blocks_total",
		Help: "Number of LLM input or output sources blocked by guard and boundary.",
	},
	[]string{"guard", "boundary"},
)

func CheckExternalContent(guard *promptguard.Guard, parts ...string) (promptguard.Evaluation, error) {
	if guard == nil {
		return promptguard.Evaluation{}, promptguard.ErrGuardUnavailable
	}
	return guard.Check(promptguard.CheckRequest{
		Text:        promptguard.JoinParts(parts...),
		Source:      promptguard.SourceWebSearchResult,
		Enforcement: promptguard.EnforcementInteractive,
	})
}

func FilterSearchResults(results []model.SearchResult, guard *promptguard.Guard, logger *slog.Logger, boundary string) ([]model.SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}
	if guard == nil {
		return nil, promptguard.ErrGuardUnavailable
	}
	if logger == nil {
		logger = slog.Default()
	}

	filtered := make([]model.SearchResult, 0, len(results))
	for i := range results {
		result := &results[i]
		evaluation, err := CheckExternalContent(guard, result.Title, result.URL, result.Content, result.PublishedDate)
		if err == nil {
			filtered = append(filtered, *result)
			continue
		}

		var blocked *promptguard.BlockedError
		if !errors.As(err, &blocked) {
			return nil, fmt.Errorf("check search result: %w", err)
		}

		RecordBlock("prompt", boundary)
		logger.Warn("LLM external search result blocked",
			slog.String("boundary", boundary),
			slog.Int("result_index", i),
			slog.String("decision", string(evaluation.Decision)),
			slog.Any("rules", blocked.Rules),
		)
	}
	return filtered, nil
}

func ValidateGeneratedOutput(guard *outputguard.Guard, text, boundary string) (outputguard.Evaluation, error) {
	if guard == nil {
		return outputguard.Evaluation{}, ErrOutputGuardUnavailable
	}

	evaluation := guard.Check(outputguard.CheckRequest{Text: text})
	if evaluation.Decision != outputguard.DecisionBlock {
		return evaluation, nil
	}

	RecordBlock("output", boundary)
	return evaluation, outputguard.ErrRestrictedGeneratedText
}

func RecordBlock(guard, boundary string) {
	guardBlocks.WithLabelValues(guard, boundary).Inc()
}
