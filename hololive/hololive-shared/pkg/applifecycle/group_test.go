package applifecycle

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestGroupRuntimeStartUsesDeclarationOrder(t *testing.T) {
	t.Parallel()

	var got []string
	group := NewGroupRuntime(nil,
		GroupComponent{Name: "infra", Start: func(context.Context, chan<- error) { got = append(got, "infra") }},
		GroupComponent{Name: "bot", Start: func(context.Context, chan<- error) { got = append(got, "bot") }},
		GroupComponent{Name: "nil-start"},
		GroupComponent{Name: "admin", Start: func(context.Context, chan<- error) { got = append(got, "admin") }},
	)

	group.Start(context.Background(), nil)

	want := []string{"infra", "bot", "admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("start order = %v, want %v", got, want)
	}
}

func TestGroupRuntimeShutdownUsesReverseOrderAndJoinsErrors(t *testing.T) {
	t.Parallel()

	botErr := errors.New("bot stopped dirty")
	llmErr := errors.New("llm stopped dirty")
	var got []string
	group := NewGroupRuntime(nil,
		GroupComponent{Name: "bot", Shutdown: func(context.Context) error {
			got = append(got, "bot")
			return botErr
		}},
		GroupComponent{Name: "admin", Shutdown: func(context.Context) error {
			got = append(got, "admin")
			return nil
		}},
		GroupComponent{Name: "llm", Shutdown: func(context.Context) error {
			got = append(got, "llm")
			return llmErr
		}},
	)

	err := group.Shutdown(context.Background())

	wantOrder := []string{"llm", "admin", "bot"}
	if !reflect.DeepEqual(got, wantOrder) {
		t.Fatalf("shutdown order = %v, want %v", got, wantOrder)
	}
	if !errors.Is(err, botErr) {
		t.Fatalf("joined error does not contain bot error: %v", err)
	}
	if !errors.Is(err, llmErr) {
		t.Fatalf("joined error does not contain llm error: %v", err)
	}
	if !strings.Contains(err.Error(), "shutdown bot") || !strings.Contains(err.Error(), "shutdown llm") {
		t.Fatalf("joined error lacks component context: %v", err)
	}
}

func TestGroupRuntimeNilIsNoop(t *testing.T) {
	t.Parallel()

	var group *GroupRuntime
	group.Start(context.Background(), nil)
	if err := group.Shutdown(context.Background()); err != nil {
		t.Fatalf("nil group shutdown returned error: %v", err)
	}
}
