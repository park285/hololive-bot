package app

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"testing"

	"github.com/kapu/hololive-shared/pkg/applifecycle"
)

const (
	lifecycleAdminName   = "admin"
	lifecycleBotName     = "bot"
	lifecycleLLMName     = "llm"
	lifecycleYouTubeName = "youtube"
)

func TestRuntimeStartPropagatesContextAndErrorChannel(t *testing.T) {
	type contextKey struct{}

	ctx := context.WithValue(t.Context(), contextKey{}, "runtime")
	errCh := make(chan error, 1)
	group := &runtimeGroupStub{start: func(gotCtx context.Context, gotErrCh chan<- error) {
		if gotCtx != ctx {
			t.Fatal("Start() did not preserve the runtime context")
		}

		if gotErrCh != errCh {
			t.Fatal("Start() did not preserve the runtime error channel")
		}
	}}
	runtime := &Runtime{group: group}

	runtime.Start(ctx, errCh)
}

func TestRuntimeShutdownPreservesJoinedErrorsAndAttemptsAllPlanes(t *testing.T) {
	firstErr := errors.New("admin shutdown")
	secondErr := errors.New("llm shutdown")

	var calls []string

	group := applifecycle.NewGroupRuntime(slog.Default(),
		applifecycle.GroupComponent{
			Name: lifecycleLLMName,
			Shutdown: func(context.Context) error {
				calls = append(calls, lifecycleLLMName)

				return secondErr
			},
		},
		applifecycle.GroupComponent{
			Name: lifecycleAdminName,
			Shutdown: func(context.Context) error {
				calls = append(calls, lifecycleAdminName)

				return firstErr
			},
		},
	)
	runtime := &Runtime{group: group}

	err := runtime.Shutdown(t.Context())

	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("Shutdown() error = %v, want both component errors", err)
	}

	if want := []string{lifecycleAdminName, lifecycleLLMName}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("Shutdown() calls = %v, want %v", calls, want)
	}
}

func TestRuntimePlaneOrder(t *testing.T) {
	tests := []struct {
		name      string
		youtube   bool
		wantStart []string
	}{
		{name: "youtube disabled", wantStart: []string{lifecycleLLMName, lifecycleAdminName, lifecycleBotName}},
		{name: "youtube enabled", youtube: true, wantStart: []string{lifecycleYouTubeName, lifecycleLLMName, lifecycleAdminName, lifecycleBotName}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var (
				starts []string
				stops  []string
			)

			components := lifecycleTestComponents(test.youtube, &starts, &stops)
			runtime := &Runtime{group: applifecycle.NewGroupRuntime(slog.Default(), components...)}

			runtime.Start(t.Context(), make(chan error, 1))

			if err := runtime.Shutdown(t.Context()); err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}

			if !reflect.DeepEqual(starts, test.wantStart) {
				t.Fatalf("Start() order = %v, want %v", starts, test.wantStart)
			}

			wantStops := append([]string(nil), test.wantStart...)
			for left, right := 0, len(wantStops)-1; left < right; left, right = left+1, right-1 {
				wantStops[left], wantStops[right] = wantStops[right], wantStops[left]
			}

			if !reflect.DeepEqual(stops, wantStops) {
				t.Fatalf("Shutdown() order = %v, want %v", stops, wantStops)
			}
		})
	}
}

func TestRuntimeCloseIsIdempotentAndOrdered(t *testing.T) {
	var calls []string

	runtime := &Runtime{closeSteps: []func(){
		func() { calls = append(calls, lifecycleBotName) },
		func() { calls = append(calls, lifecycleAdminName) },
		func() { calls = append(calls, lifecycleLLMName) },
		func() { calls = append(calls, lifecycleYouTubeName) },
	}}

	runtime.Close()
	runtime.Close()

	if want := []string{lifecycleBotName, lifecycleAdminName, lifecycleLLMName, lifecycleYouTubeName}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("Close() calls = %v, want %v", calls, want)
	}
}

type runtimeGroupStub struct {
	start func(context.Context, chan<- error)
}

func (g *runtimeGroupStub) Start(ctx context.Context, errCh chan<- error) {
	if g.start != nil {
		g.start(ctx, errCh)
	}
}

func (g *runtimeGroupStub) Shutdown(context.Context) error {
	return nil
}

func lifecycleTestComponents(withYouTube bool, starts, stops *[]string) []applifecycle.GroupComponent {
	names := []string{lifecycleLLMName, lifecycleAdminName, lifecycleBotName}

	if withYouTube {
		names = append([]string{lifecycleYouTubeName}, names...)
	}

	components := make([]applifecycle.GroupComponent, 0, len(names))
	for _, name := range names {
		components = append(components, applifecycle.GroupComponent{
			Name: name,
			Start: func(context.Context, chan<- error) {
				*starts = append(*starts, name)
			},
			Shutdown: func(context.Context) error {
				*stops = append(*stops, name)

				return nil
			},
		})
	}

	return components
}
