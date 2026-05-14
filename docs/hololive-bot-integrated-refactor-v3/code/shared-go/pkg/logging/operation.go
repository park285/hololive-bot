package logging

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

type OperationOptions struct {
	Name         string
	IDPrefix     string
	Runtime      string
	Component    string
	StartEvent   string
	SuccessEvent string
	FailureEvent string
	SkipStartLog bool
	Attrs        []slog.Attr
}

func RunOperation(ctx context.Context, logger *slog.Logger, opts OperationOptions, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "operation"
	}

	jobID := JobIDFromContext(ctx)
	if jobID == "" {
		prefix := opts.IDPrefix
		if strings.TrimSpace(prefix) == "" {
			prefix = name
		}
		jobID = NewID(prefix)
		ctx = WithJobID(ctx, jobID)
	}

	if opts.Runtime != "" {
		ctx = WithRuntime(ctx, opts.Runtime)
	}
	if opts.Component != "" {
		ctx = WithComponent(ctx, opts.Component)
	}

	baseAttrs := make([]slog.Attr, 0, 2+len(opts.Attrs))
	baseAttrs = append(baseAttrs, Operation(name))
	baseAttrs = append(baseAttrs, opts.Attrs...)

	start := time.Now()
	if !opts.SkipStartLog {
		Info(ctx, logger, eventOrDefault(opts.StartEvent, name+".started"), "operation started", baseAttrs...)
	}

	err := fn(ctx)
	attrs := append([]slog.Attr{}, baseAttrs...)
	attrs = append(attrs, SinceMS(start))

	if err != nil {
		attrs = append(attrs, ErrorAttrs(err)...)
		Error(ctx, logger, eventOrDefault(opts.FailureEvent, name+".failed"), "operation failed", attrs...)
		return err
	}

	Info(ctx, logger, eventOrDefault(opts.SuccessEvent, name+".succeeded"), "operation succeeded", attrs...)
	return nil
}

func eventOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
