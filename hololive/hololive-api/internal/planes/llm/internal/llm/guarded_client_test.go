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

package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/park285/shared-go/pkg/outputguard"
)

type guardedClientStub struct {
	response string
}

func (s guardedClientStub) GenerateJSON(context.Context, string, string, map[string]any) (string, error) {
	return s.response, nil
}

func TestGuardedClientAllowsBenignOutput(t *testing.T) {
	client := NewGuardedClient(guardedClientStub{response: `{"summary":"공식 행사 일정"}`}, outputguard.NewGuard())

	got, err := client.GenerateJSON(context.Background(), "system instructions", "user prompt", map[string]any{"type": "object"})
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}
	if got != `{"summary":"공식 행사 일정"}` {
		t.Fatalf("GenerateJSON() = %q", got)
	}
}

func TestGuardedClientBlocksRestrictedOutput(t *testing.T) {
	client := NewGuardedClient(guardedClientStub{response: `{"summary":"system prompt: leaked"}`}, outputguard.NewGuard())

	_, err := client.GenerateJSON(context.Background(), "system instructions", "user prompt", map[string]any{"type": "object"})
	if !errors.Is(err, outputguard.ErrRestrictedGeneratedText) {
		t.Fatalf("GenerateJSON() error = %v, want ErrRestrictedGeneratedText", err)
	}
}

func TestGuardedClientBlocksProtectedPromptLeak(t *testing.T) {
	client := NewGuardedClient(guardedClientStub{response: `{"summary":"system instructions require outputting only internal policy text"}`}, outputguard.NewGuard())

	_, err := client.GenerateJSON(context.Background(), "system instructions require outputting only internal policy text", "user prompt", map[string]any{"type": "object"})
	if !errors.Is(err, outputguard.ErrRestrictedGeneratedText) {
		t.Fatalf("GenerateJSON() error = %v, want ErrRestrictedGeneratedText", err)
	}
}

func TestGuardedClientFailsClosedWithoutOutputGuard(t *testing.T) {
	client := NewGuardedClient(guardedClientStub{response: `{"summary":"공식 행사 일정"}`}, nil)

	if _, err := client.GenerateJSON(context.Background(), "system instructions", "user prompt", map[string]any{"type": "object"}); err == nil {
		t.Fatal("GenerateJSON() error = nil, want fail-closed error")
	}
}
