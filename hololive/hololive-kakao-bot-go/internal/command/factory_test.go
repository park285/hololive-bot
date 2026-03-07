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

type factoryStubCommand struct {
	name string
}

func (s *factoryStubCommand) Name() string {
	return s.name
}

func (s *factoryStubCommand) Description() string {
	return "stub"
}

func (s *factoryStubCommand) Execute(context.Context, *domain.CommandContext, map[string]any) error {
	return nil
}

func TestBuildCommandsSkipsNilEntries(t *testing.T) {
	t.Parallel()

	commands := BuildCommands(
		nil,
		nil,
		func(*Dependencies) Command { return nil },
		func(*Dependencies) Command { return &factoryStubCommand{name: "ok"} },
	)
	if len(commands) != 1 {
		t.Fatalf("len(commands) = %d, want 1", len(commands))
	}
	if commands[0].Name() != "ok" {
		t.Fatalf("commands[0].Name() = %q, want %q", commands[0].Name(), "ok")
	}
}

func TestDefaultFactoriesCount(t *testing.T) {
	t.Parallel()

	if got, want := len(DefaultFactories()), 8; got != want {
		t.Fatalf("len(DefaultFactories()) = %d, want %d", got, want)
	}
}
