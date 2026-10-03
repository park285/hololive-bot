package collectorruntime

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
)

// 실패 원문과 대상 식별자는 trace에 복제하지 않고 기존 typed 분류를 기록한다.
func finishCollectionSpan(span trace.Span, err error) {
	outcome := attemptResult(err)
	if errors.Is(err, joblease.ErrFenceLost) {
		outcome = "outcome_unknown"
	} else if errors.Is(err, context.Canceled) {
		outcome = "canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		outcome = "timeout"
	}

	span.SetAttributes(attribute.String("collection.outcome", outcome))

	if err != nil {
		span.SetAttributes(attribute.String("error.type", string(collecterr.ClassOf(err))))
		span.SetStatus(codes.Error, outcome)
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End()
}
