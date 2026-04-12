package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	runtimeapp "github.com/kapu/hololive-stream-ingester/internal/runtime"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts baseline config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	baseline, err := runtimeapp.CollectCommunityShortsTargetBaseline(ctx, cfg, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts target baseline: %v\n", err)
		os.Exit(1)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(baseline); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write community/shorts target baseline JSON: %v\n", err)
		os.Exit(1)
	}
}
