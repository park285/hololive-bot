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

package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

var ErrUnknownCommand = errors.New("unknown command")

type Registry struct {
	mu        sync.RWMutex
	handlers  map[string]Command
	aliasKeys map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		handlers:  make(map[string]Command),
		aliasKeys: make(map[string]string),
	}
}

func (r *Registry) Register(handler Command) {
	if handler == nil {
		return
	}

	name := stringutil.Normalize(handler.Name())

	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[name] = handler
}

func (r *Registry) Execute(ctx context.Context, cmdCtx *domain.CommandContext, key string, params map[string]any) error {
	if r == nil {
		return errors.New("command registry is nil")
	}

	handler := r.getHandler(key)
	if handler == nil {
		return fmt.Errorf("%w: %s", ErrUnknownCommand, key)
	}

	if err := handler.Execute(ctx, cmdCtx, params); err != nil {
		return fmt.Errorf("failed to execute command %s: %w", key, err)
	}

	return nil
}

func (r *Registry) Count() int {
	if r == nil {
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.handlers)
}

func (r *Registry) getHandler(key string) Command {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if key == "" {
		return nil
	}

	if handler, ok := r.handlers[stringutil.Normalize(key)]; ok {
		return handler
	}

	return nil
}
