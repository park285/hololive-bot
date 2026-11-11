package app

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"testing"
)

const (
	lifecycleAdminName   = "admin"
	lifecycleBotName     = "bot"
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
	secondErr := errors.New("youtube shutdown")

	var calls []string

	group := newPlaneGroup(slog.Default(),
		planeComponent{
			Name: lifecycleYouTubeName,
			Shutdown: func(context.Context) error {
				calls = append(calls, lifecycleYouTubeName)

				return secondErr
			},
		},
		planeComponent{
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

	if want := []string{lifecycleAdminName, lifecycleYouTubeName}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("Shutdown() calls = %v, want %v", calls, want)
	}
}

func TestRuntimePlaneOrder(t *testing.T) {
	var (
		starts []string
		stops  []string
	)

	runtime := &Runtime{group: newPlaneGroup(slog.Default(), lifecycleTestComponents(&starts, &stops)...)}

	runtime.Start(t.Context(), make(chan error, 1))

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	wantStart := []string{lifecycleYouTubeName, lifecycleAdminName, lifecycleBotName}
	if !reflect.DeepEqual(starts, wantStart) {
		t.Fatalf("Start() order = %v, want %v", starts, wantStart)
	}

	wantStops := slices.Clone(wantStart)
	slices.Reverse(wantStops)

	if !reflect.DeepEqual(stops, wantStops) {
		t.Fatalf("Shutdown() order = %v, want %v", stops, wantStops)
	}
}

func TestRuntimeCloseIsIdempotentAndOrdered(t *testing.T) {
	var calls []string

	runtime := &Runtime{closeSteps: []func(context.Context) error{
		func(context.Context) error { calls = append(calls, lifecycleBotName); return nil },
		func(context.Context) error { calls = append(calls, lifecycleAdminName); return nil },
		func(context.Context) error { calls = append(calls, lifecycleYouTubeName); return nil },
	}}

	runtime.Close()
	runtime.Close()

	if want := []string{lifecycleBotName, lifecycleAdminName, lifecycleYouTubeName}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("Close() calls = %v, want %v", calls, want)
	}
}

type runtimeGroupStub struct {
	start    func(context.Context, chan<- error)
	shutdown func(context.Context) error
}

func (g *runtimeGroupStub) Start(ctx context.Context, errCh chan<- error) {
	if g.start != nil {
		g.start(ctx, errCh)
	}
}

func (g *runtimeGroupStub) Shutdown(ctx context.Context) error {
	if g.shutdown != nil {
		return g.shutdown(ctx)
	}

	return nil
}

func lifecycleTestComponents(starts, stops *[]string) []planeComponent {
	names := []string{lifecycleYouTubeName, lifecycleAdminName, lifecycleBotName}

	components := make([]planeComponent, 0, len(names))
	for _, name := range names {
		components = append(components, planeComponent{
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
