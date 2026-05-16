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
	"maps"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type NormalizeFunc func(domain.CommandType, map[string]any) (string, map[string]any)

type sequentialDispatcher struct {
	registry  *Registry
	normalize NormalizeFunc
}

func NewSequentialDispatcher(registry *Registry, normalize NormalizeFunc) Dispatcher {
	return &sequentialDispatcher{registry: registry, normalize: normalize}
}

func (d *sequentialDispatcher) Publish(ctx context.Context, cmdCtx *domain.CommandContext, events ...Event) (int, error) {
	if d == nil || d.registry == nil || d.normalize == nil {
		return 0, errors.New("dispatcher not configured")
	}

	executed := 0

	for _, event := range events {
		if event.Type == domain.CommandUnknown {
			continue
		}

		normalizedParams := cloneParams(event.Params)

		key, params := d.normalize(event.Type, normalizedParams)
		if err := d.registry.Execute(ctx, cmdCtx, key, params); err != nil {
			return executed, err
		}

		executed++
	}

	return executed, nil
}

func cloneParams(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}

	clone := make(map[string]any, len(src))
	maps.Copy(clone, src)

	return clone
}
