package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/shared-go/v2/pkg/health"
)

func TestRetentionFailureKeepsRuntimeUnreadyOnlyUntilRetentionSucceeds(t *testing.T) {
	health.RemoveComponent(youtubeHealthComponent)
	t.Cleanup(func() { health.RemoveComponent(youtubeHealthComponent) })

	runtime := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})
	runtime.ready.Store(true)

	publishedReady := func() bool {
		response, _ := health.GetReadiness()

		return response.Components[youtubeHealthComponent].Ready
	}

	if runtime.stopAfterRetentionError(t.Context(), nil, errors.New("retention constraint violated")) {
		t.Fatal("retention loop stopped after a non-context error")
	}

	if runtime.Ready() || publishedReady() {
		t.Fatal("non-retryable retention failure left the plane ready")
	}

	runtime.stopAfterRetentionError(t.Context(), nil, context.DeadlineExceeded)

	if runtime.Ready() {
		t.Fatal("retryable retention error cleared the retention failure")
	}

	runtime.stopAfterRetentionError(t.Context(), nil, nil)

	if !runtime.Ready() || !publishedReady() {
		t.Fatal("successful retention tick did not restore readiness")
	}
}

func TestRetentionTimeoutDoesNotHideOtherRetentionFailure(t *testing.T) {
	health.RemoveComponent(youtubeHealthComponent)
	t.Cleanup(func() { health.RemoveComponent(youtubeHealthComponent) })

	runtime := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})
	runtime.ready.Store(true)

	runtime.stopAfterRetentionError(t.Context(), nil, errors.Join(errors.New("retention constraint violated"), context.DeadlineExceeded))

	if runtime.Ready() {
		t.Fatal("deadline in one retention phase hid the other phase's non-retryable failure")
	}
}

func TestRetentionSuccessDoesNotRestoreReadinessLostElsewhere(t *testing.T) {
	health.RemoveComponent(youtubeHealthComponent)
	t.Cleanup(func() { health.RemoveComponent(youtubeHealthComponent) })

	runtime := newTestRuntime(t, fakeClaimer{}, fakeConsumer{})

	runtime.stopAfterRetentionError(t.Context(), nil, errors.New("retention constraint violated"))
	runtime.stopAfterRetentionError(t.Context(), nil, nil)

	if runtime.Ready() {
		t.Fatal("retention success restored readiness that another loop had withdrawn")
	}
}
