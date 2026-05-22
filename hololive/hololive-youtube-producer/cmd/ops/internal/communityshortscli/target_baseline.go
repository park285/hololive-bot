package communityshortscli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
)

func runTargetBaselineCommand(ctx commandContext, args []string) error {
	fs := newFlagSet(ctx, "target-baseline")
	if err := fs.Parse(args); err != nil {
		return err
	}

	appConfig, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load community/shorts baseline config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(ctx.stderr, nil))
	reqCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	baseline, err := communityshorts.CollectTargetBaseline(reqCtx, appConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to collect community/shorts target baseline: %w", err)
	}

	encoder := json.NewEncoder(ctx.stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(baseline); err != nil {
		return fmt.Errorf("failed to write community/shorts target baseline JSON: %w", err)
	}
	return nil
}
