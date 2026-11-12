package app

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestPlaneGroupStartUsesDeclarationOrder(t *testing.T) {
	t.Parallel()

	var got []string

	group := newPlaneGroup(slog.New(slog.DiscardHandler),
		planeComponent{Name: "infra", Start: func(context.Context, chan<- error) { got = append(got, "infra") }},
		planeComponent{Name: lifecycleBotName, Start: func(context.Context, chan<- error) { got = append(got, lifecycleBotName) }},
		planeComponent{Name: "nil-start"},
		planeComponent{Name: lifecycleAdminName, Start: func(context.Context, chan<- error) { got = append(got, lifecycleAdminName) }},
	)

	group.Start(t.Context(), nil)

	want := []string{"infra", lifecycleBotName, lifecycleAdminName}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %v, want %v", got, want)
	}
}

func TestPlaneGroupShutdownUsesReverseOrderAndJoinsErrors(t *testing.T) {
	t.Parallel()

	botErr := errors.New("bot stopped dirty")
	youtubeErr := errors.New("youtube stopped dirty")

	var got []string

	group := newPlaneGroup(slog.New(slog.DiscardHandler),
		planeComponent{Name: lifecycleBotName, Shutdown: func(context.Context) error {
			got = append(got, lifecycleBotName)
			return botErr
		}},
		planeComponent{Name: lifecycleAdminName, Shutdown: func(context.Context) error {
			got = append(got, lifecycleAdminName)
			return nil
		}},
		planeComponent{Name: lifecycleYouTubeName, Shutdown: func(context.Context) error {
			got = append(got, lifecycleYouTubeName)
			return youtubeErr
		}},
	)

	err := group.Shutdown(t.Context())
	if err == nil {
		t.Fatal("shutdown returned nil error, want joined component errors")
	}

	wantOrder := []string{lifecycleYouTubeName, lifecycleAdminName, lifecycleBotName}
	if !reflect.DeepEqual(got, wantOrder) {
		t.Fatalf("shutdown order = %v, want %v", got, wantOrder)
	}

	if !errors.Is(err, botErr) {
		t.Fatalf("joined error does not contain bot error: %v", err)
	}

	if !errors.Is(err, youtubeErr) {
		t.Fatalf("joined error does not contain youtube error: %v", err)
	}

	if !strings.Contains(err.Error(), "shutdown bot") || !strings.Contains(err.Error(), "shutdown youtube") {
		t.Fatalf("joined error lacks component context: %v", err)
	}
}

func TestPlaneGroupShutdownIsolatesComponentPanic(t *testing.T) {
	t.Parallel()

	var got []string

	group := newPlaneGroup(slog.New(slog.DiscardHandler),
		planeComponent{Name: "deps", Shutdown: func(context.Context) error {
			got = append(got, "deps")
			return nil
		}},
		planeComponent{Name: "web", Shutdown: func(context.Context) error {
			panic("web shutdown exploded")
		}},
	)

	err := group.Shutdown(t.Context())
	if err == nil || !strings.Contains(err.Error(), "shutdown web") || !strings.Contains(err.Error(), "web shutdown exploded") {
		t.Fatalf("shutdown error = %v, want recovered web panic", err)
	}

	if !reflect.DeepEqual(got, []string{"deps"}) {
		t.Fatalf("shutdown continued = %v, want deps after web panic", got)
	}
}

func TestPlaneGroupNilIsNoop(t *testing.T) {
	t.Parallel()

	var group *planeGroup

	group.Start(t.Context(), nil)

	if err := group.Shutdown(t.Context()); err != nil {
		t.Fatalf("nil group shutdown returned error: %v", err)
	}
}
