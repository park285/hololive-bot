package fxapp

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/shared-go/v2/pkg/telemetry"
	"go.uber.org/fx"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/constants"
)

type constructionContextKey struct{}

type constructionTelemetry func(context.Context) error

func (f constructionTelemetry) Shutdown(ctx context.Context) error { return f(ctx) }

func TestApplicationConstructionDeadlineStillFlushesTelemetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		buildErr := errors.New("runtime construction failed")
		closeErr := errors.New("telemetry flush failed")
		cleanups := 0
		params := successfulApplicationParams(nil)

		params.dependencies.newTelemetry = func(context.Context, telemetry.Config) (telemetryResource, error) {
			return constructionTelemetry(func(ctx context.Context) error {
				cleanups++

				assertConstructionCleanupContext(ctx, t)
				// 실제 telemetry.Shutdown은 부모 취소와 분리한 5초 flush를 소유한다.
				time.Sleep(5 * time.Second)

				return closeErr
			}), nil
		}
		params.dependencies.buildRuntime = func(ctx context.Context, _ *apiconfig.RuntimeConfig, _ *slog.Logger) (runtimeResource, error) {
			<-ctx.Done()

			return nil, errors.Join(buildErr, ctx.Err())
		}

		buildCtx, cancel := constructionBuildContext(t)

		defer cancel()

		start := time.Now()
		application, err := newApplication(buildCtx, params)

		if application != nil || !errors.Is(err, buildErr) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, closeErr) {
			t.Fatalf("construction = (%v, %v), want original build/deadline/cleanup errors", application, err)
		}

		if elapsed := time.Since(start); elapsed != constants.AppTimeout.Build+5*time.Second || cleanups != 1 {
			t.Fatalf("construction elapsed = %v, telemetry cleanups = %d, want build + flush/1", elapsed, cleanups)
		}
	})
}

func TestApplicationConstructionFailureClosesPartialResourcesOnce(t *testing.T) {
	for _, coordinatorMissing := range []bool{false, true} {
		t.Run(map[bool]string{false: "invoke failure", true: "coordinator missing"}[coordinatorMissing], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				buildErr := errors.New("registration failed")
				buildCtx, cancel := constructionBuildContext(t)

				defer cancel()

				var calls []string

				params := successfulApplicationParams(nil)

				params.dependencies.newTelemetry = func(context.Context, telemetry.Config) (telemetryResource, error) {
					return constructionTelemetry(func(ctx context.Context) error {
						assertConstructionCleanupContext(ctx, t)

						calls = append(calls, "telemetry")

						return nil
					}), nil
				}
				params.dependencies.buildRuntime = func(context.Context, *apiconfig.RuntimeConfig, *slog.Logger) (runtimeResource, error) {
					return &lifecycleTestRuntime{closeRuntime: func(ctx context.Context) error {
						assertConstructionCleanupContext(ctx, t)

						calls = append(calls, "runtime")

						return nil
					}}, nil
				}

				if coordinatorMissing {
					// Fx graph의 등록 대상과 반환 state를 나누어 constructor 성공 뒤 누락 분기에 도달시킨다.
					params.extraOptions = []fx.Option{
						fx.Replace(&applicationState{}),
						fx.Invoke(func(runtimeResource) { <-buildCtx.Done() }),
					}
				} else {
					params.extraOptions = []fx.Option{fx.Invoke(func(runtimeResource) error {
						<-buildCtx.Done()

						return buildErr
					})}
				}

				application, err := newApplication(buildCtx, params)
				if application != nil || !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("construction = (%v, %v), want nil with build deadline", application, err)
				}

				if !coordinatorMissing && !errors.Is(err, buildErr) {
					t.Fatalf("construction lost registration error: %v", err)
				}

				if coordinatorMissing && !strings.Contains(err.Error(), "lifecycle coordinator was not registered") {
					t.Fatalf("construction did not reach the missing coordinator branch: %v", err)
				}

				if want := []string{"runtime", "telemetry"}; !reflect.DeepEqual(calls, want) {
					t.Fatalf("partial cleanup calls = %v, want %v", calls, want)
				}
			})
		})
	}
}

func TestApplicationConstructionRollbackUsesExistingShutdownBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })

		defer finish()

		cleanups := 0
		params := successfulApplicationParams(nil)

		params.dependencies.newTelemetry = func(context.Context, telemetry.Config) (telemetryResource, error) {
			return constructionTelemetry(func(ctx context.Context) error {
				cleanups++

				assertConstructionCleanupContext(ctx, t)
				<-release

				return nil
			}), nil
		}
		params.dependencies.buildRuntime = func(ctx context.Context, _ *apiconfig.RuntimeConfig, _ *slog.Logger) (runtimeResource, error) {
			<-ctx.Done()

			return nil, ctx.Err()
		}

		buildCtx, cancel := constructionBuildContext(t)

		defer cancel()

		start := time.Now()
		application, err := newApplication(buildCtx, params)

		synctest.Wait()

		if application != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("construction = (%v, %v), want nil/deadline", application, err)
		}

		if elapsed := time.Since(start); elapsed != constants.AppTimeout.Build+constants.AppTimeout.Shutdown || cleanups != 1 {
			t.Fatalf("construction elapsed = %v, cleanups = %d, want shared build + cleanup budgets/1", elapsed, cleanups)
		}

		finish()
		synctest.Wait()

		if cleanups != 1 {
			t.Fatalf("cleanup callback count = %d, want 1", cleanups)
		}
	})
}

func constructionBuildContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	return context.WithTimeout(context.WithValue(t.Context(), constructionContextKey{}, "startup"), constants.AppTimeout.Build)
}

func assertConstructionCleanupContext(ctx context.Context, t *testing.T) {
	t.Helper()

	if ctx.Err() != nil || ctx.Value(constructionContextKey{}) != "startup" {
		t.Errorf("cleanup context lost construction values or remained canceled: %v", ctx.Err())
	}

	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) != constants.AppTimeout.Shutdown {
		t.Errorf("cleanup deadline = %v, want existing shutdown budget", deadline)
	}
}
