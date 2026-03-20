// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package command

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type stubCommand struct {
	name     string
	executed int
}

func (s *stubCommand) Name() string { return s.name }

func (s *stubCommand) Description() string { return "stub" }

func (s *stubCommand) Execute(_ context.Context, _ *domain.CommandContext, _ map[string]any) error {
	s.executed++
	return nil
}

func TestSequentialDispatcherPublishRequiresConfiguration(t *testing.T) {
	ctx := t.Context()

	var nilDispatcher *sequentialDispatcher
	if _, err := nilDispatcher.Publish(ctx, nil); err == nil {
		t.Fatal("expected error for nil dispatcher")
	}

	noRegistry := &sequentialDispatcher{normalize: func(_ domain.CommandType, p map[string]any) (string, map[string]any) {
		return "help", p
	}}
	if _, err := noRegistry.Publish(ctx, nil); err == nil {
		t.Fatal("expected error when registry is nil")
	}

	noNormalize := &sequentialDispatcher{registry: NewRegistry()}
	if _, err := noNormalize.Publish(ctx, nil); err == nil {
		t.Fatal("expected error when normalize func is nil")
	}
}

func TestSequentialDispatcherPublishExecutesEvents(t *testing.T) {
	registry := NewRegistry()
	cmd := &stubCommand{name: "help"}
	registry.Register(cmd)

	dispatcher := NewSequentialDispatcher(
		registry,
		func(_ domain.CommandType, p map[string]any) (string, map[string]any) {
			return "help", p
		},
	)

	executed, err := dispatcher.Publish(t.Context(), &domain.CommandContext{}, Event{
		Type:   domain.CommandHelp,
		Params: map[string]any{"foo": "bar"},
	})
	if err != nil {
		t.Fatalf("publish failed: %v", err)
	}

	if executed != 1 {
		t.Fatalf("executed = %d, want 1", executed)
	}

	if cmd.executed != 1 {
		t.Fatalf("command executed = %d, want 1", cmd.executed)
	}
}
