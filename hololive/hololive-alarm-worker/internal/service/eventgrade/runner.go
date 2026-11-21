package eventgrade

import (
	"context"
	"log/slog"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/alarm"
)

const (
	consumeInterval = 10 * time.Second
	consumeBatch    = 50
)

var gradeRegions = [...]contract.GradeRegion{"jp", "us", "as"}

type Runner struct {
	repo      *Repository
	publisher dispatchPublisher
	logger    *slog.Logger
}

func NewRunner(repo *Repository, publisher dispatchPublisher, logger *slog.Logger) *Runner {
	return &Runner{repo: repo, publisher: publisher, logger: logger}
}

func (r *Runner) Start(ctx context.Context) error {
	ticker := time.NewTicker(consumeInterval)
	defer ticker.Stop()

	for {
		r.tick(ctx)

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (r *Runner) tick(ctx context.Context) {
	regions := make([]string, 0, len(gradeRegions))

	for _, region := range gradeRegions {
		if ctx.Err() != nil {
			return
		}

		result, err := r.repo.ConsumeRegion(ctx, region, consumeBatch)
		if result.Rejected > 0 {
			r.logger.WarnContext(ctx, "Event grade observations rejected", slog.String("region", string(region)), slog.Int("count", result.Rejected))
		}

		if err != nil {
			r.logger.ErrorContext(ctx, "Event grade consumption stopped", slog.String("region", string(region)), slog.Any("error", err))

			continue
		}

		regions = append(regions, string(region))
	}

	if err := r.repo.FinalizeRegions(ctx, r.publisher, regions); err != nil && ctx.Err() == nil {
		r.logger.ErrorContext(ctx, "Event grade finalization failed", slog.Any("error", err))
	}
}
