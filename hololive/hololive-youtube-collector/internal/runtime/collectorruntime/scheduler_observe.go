package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
)

// observePublishError는 관측 kind별 publish 결과만 센다. Fence 손실의 lease_lost 계측은 callback join 뒤
// handleRunError가 phase 표식을 보고 한 번만 기록한다.
func (e *collectionExecutor) observePublishError(spec *joblease.JobSpec, output collection.RunOutput, err error) {
	if supersededError(err) {
		e.observePublishOutcome(spec.Provider, output, outcomeSuperseded)

		return
	}

	e.observePublishOutcome(spec.Provider, output, outcomeRejected)
}

func (e *collectionExecutor) acquireProvider(ctx context.Context, provider contract.Provider) (resultErr error) {
	ctx, span := otel.Tracer("hololive/collector").Start(ctx, "youtube.collection.provider.wait")

	defer func() { finishCollectionSpan(span, resultErr) }()

	gate := e.gates[provider]
	if gate == nil {
		return collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "provider gate is not configured")
	}

	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("await provider admission slot: %w", err)
		}

		return nil
	}
}

func (e *collectionExecutor) releaseProvider(provider contract.Provider) {
	gate := e.gates[provider]
	if gate == nil {
		return
	}

	select {
	case <-gate:
	default:
	}
}

// observePublished는 commit이 끝난 뒤에만 호출된다. Inserted·duplicate는 durable 수락으로 보고 수락 시각을 남기며,
// checkpoint가 실제로 전진한 관측만 수락 간격을 기록한다. Collision은 수락이 아니다.
func (e *collectionExecutor) observePublished(output collection.RunOutput, result sourceobservation.PublishBatchResult) {
	committedAt := time.Now().UTC()

	for i := range output.ObservationCount() {
		envelope := output.ObservationMetadata(i)
		outcome, ok := publishedOutcome(result, i)

		if !ok {
			continue
		}

		e.metrics.ObservePublish(envelope.Provider, string(envelope.ObservationKind), outcome)
		e.metrics.ObserveCompleteness(envelope.Provider, string(envelope.ObservationKind), envelope.Completeness, envelope.Continuity)

		if outcome == outcomeCollision {
			continue
		}

		published := result.Results[i]
		e.metrics.ObserveAccepted(envelope.Provider, envelope.ObservationKind, committedAt, published.AcceptedInterval, published.HasAcceptedInterval)
	}
}

func (e *collectionExecutor) recordTerminalSuccess(published *sourceobservation.PublishBatchResult) {
	if e == nil || e.readiness == nil {
		return
	}

	e.readiness.ObserveCollectionSuccess()

	if published == nil {
		return
	}

	e.readiness.AddHandoffCandidates(handoffCandidateIDs(*published)...)
}

func handoffCandidateIDs(result sourceobservation.PublishBatchResult) []int64 {
	ids := make([]int64, 0, len(result.Results))
	for i := range result.Results {
		outcome, ok := publishedOutcome(result, i)
		if !ok || outcome == outcomeCollision {
			continue
		}

		id := result.Results[i].ObservationID
		if id <= 0 {
			continue
		}

		ids = append(ids, id)
	}

	return ids
}

func publishedOutcome(result sourceobservation.PublishBatchResult, index int) (string, bool) {
	if index < 0 || index >= len(result.Results) {
		return "", false
	}

	return publishOutcomeLabel(result.Results[index].Outcome)
}

func publishOutcomeLabel(outcome sourceobservation.PublishOutcome) (string, bool) {
	if outcome == sourceobservation.PublishInserted {
		return outcomeInserted, true
	}

	if outcome == sourceobservation.PublishDuplicate {
		return outcomeDuplicate, true
	}

	if outcome == sourceobservation.PublishCollision {
		return outcomeCollision, true
	}

	return "", false
}

func (e *collectionExecutor) observePublishOutcome(provider contract.Provider, output collection.RunOutput, outcome string) {
	for i := range output.ObservationCount() {
		envelope := output.ObservationMetadata(i)
		e.metrics.ObservePublish(provider, string(envelope.ObservationKind), outcome)
	}
}

func attemptResult(err error) string {
	if err == nil {
		return resultSuccess
	}

	if supersededError(err) {
		return resultSuperseded
	}

	return attemptFailureResult(err)
}

func supersededError(err error) bool {
	return errors.Is(err, collection.ErrProjectionStale) ||
		errors.Is(err, collection.ErrTargetDisabled)
}

func attemptFailureResult(err error) string {
	switch collecterr.CodeOf(err) {
	case collecterr.Timeout:
		return resultTimeout
	case collecterr.Canceled:
		return resultCanceled
	case collecterr.ParserDrift:
		return resultParserDrift
	case collecterr.Failed, collecterr.Cooldown, collecterr.Configuration, collecterr.ResponseTooLarge,
		collecterr.HelperBusy, collecterr.HelperProtocolMismatch, collecterr.Internal, collecterr.TargetRosterTooLarge,
		collecterr.PublishRejected, contract.ErrorObservationCollision, contract.ErrorShutdownRelease,
		contract.ErrorSupersededRelease, contract.ErrorRenewFailedRelease:
		return resultFailed
	default:
		return resultFailed
	}
}

func (e *collectionExecutor) retrySchedule(err error) (collection.RetrySchedule, error) {
	if boundsErr := e.retryBounds.Validate(); boundsErr != nil {
		return collection.RetrySchedule{}, fmt.Errorf("retry bounds: %w", boundsErr)
	}

	now := time.Now().UTC()
	bounds := e.retryBounds
	minAt := now.Add(bounds.Minimum)
	maxAt := now.Add(bounds.Maximum)
	hint := collecterr.RetryOf(err)

	var retryAt time.Time

	switch hint.Kind() {
	case collecterr.RetryAt:
		retryAt = hint.At()
	case collecterr.RetryAfter:
		retryAt = now.Add(hint.After())
	case collecterr.RetryDefault:
		retryAt = now.Add(bounds.Minimum + (bounds.Maximum-bounds.Minimum)/2)
	}

	// HTTP 429/503 및 helper cooldown의 명시적 하한만 보존합니다.
	// HelperBusy 등 다른 진단의 hint는 기존 일반 backoff 상한을 유지합니다.
	if (hint.Kind() == collecterr.RetryAt || hint.Kind() == collecterr.RetryAfter) &&
		collecterr.CodeOf(err) == collecterr.Cooldown && collecterr.ClassOf(err) == collecterr.ClassCooldown {
		if retryAt.Before(minAt) {
			retryAt = minAt
		}

		schedule, scheduleErr := collection.NewRetryNotBeforeSchedule(retryAt)
		if scheduleErr != nil {
			return collection.RetrySchedule{}, fmt.Errorf("cooldown schedule: %w", scheduleErr)
		}

		return schedule, nil
	}

	schedule, scheduleErr := collection.NewRetryAtSchedule(clampRetryAt(retryAt, minAt, maxAt))
	if scheduleErr != nil {
		return collection.RetrySchedule{}, fmt.Errorf("bounded retry schedule: %w", scheduleErr)
	}

	return schedule, nil
}

func clampRetryAt(retryAt, minAt, maxAt time.Time) time.Time {
	if retryAt.Before(minAt) {
		return minAt
	}

	if retryAt.After(maxAt) {
		return maxAt
	}

	return retryAt.UTC()
}

func (e *collectionExecutor) logFailure(ctx context.Context, phase, code, class, detail string, spec *joblease.JobSpec, proof *contract.LeaseProof) {
	if e.logger == nil {
		return
	}

	detail = collecterr.SanitizeDetail(detail)
	e.logger.WarnContext(ctx, "YouTube collection job failed",
		slog.String("job_key", spec.JobKey),
		slog.String("provider", string(spec.Provider)),
		slog.String("job_kind", spec.CollectionJobKind),
		slog.String("subject_key", spec.SubjectKey),
		slog.Int64("fence_epoch", proof.FenceEpoch),
		slog.Int64("projection_generation", proof.ProjectionGeneration),
		slog.String("error_code", code),
		slog.String("error_class", class),
		slog.String("error_detail", detail),
		slog.String("phase", phase),
	)
}
