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

package applifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// GroupComponent는 더 큰 프로세스 안에 호스팅되는 하나의 논리적 runtime plane이다.
// Start는 non-blocking이어야 하며, 비동기 실패는 errCh로 보고해야 한다(기존 runtime
// Start 계약과 동일).
type GroupComponent struct {
	Name     string
	Start    func(context.Context, chan<- error)
	Shutdown func(context.Context) error
}

// GroupRuntime은 여러 runtime plane을 한 프로세스에서 조율한다. 컴포넌트는 선언 순서로
// 시작하고 역순으로 정지한다. listener가 의존성보다 먼저 drain돼야 하면 공유 의존성
// provider를 앞에, 외부 노출 listener를 뒤에 배치한다.
type GroupRuntime struct {
	Logger     *slog.Logger
	Components []GroupComponent
}

func NewGroupRuntime(logger *slog.Logger, components ...GroupComponent) *GroupRuntime {
	return &GroupRuntime{Logger: logger, Components: append([]GroupComponent(nil), components...)}
}

func (g *GroupRuntime) Start(ctx context.Context, errCh chan<- error) {
	if g == nil {
		return
	}
	for i := range g.Components {
		component := g.Components[i]
		if component.Start == nil {
			continue
		}
		name := componentName(i, component.Name)
		component.Start(ctx, errCh)
		logInfo(g.Logger, "runtime component started", slog.String("component", name))
	}
}

func (g *GroupRuntime) Shutdown(ctx context.Context) error {
	if g == nil {
		return nil
	}
	var errs []error
	for i := len(g.Components) - 1; i >= 0; i-- {
		component := g.Components[i]
		if component.Shutdown == nil {
			continue
		}
		name := componentName(i, component.Name)
		if err := component.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("shutdown %s: %w", name, err))
			logError(g.Logger, fmt.Sprintf("runtime component shutdown failed: %s", name), err)
			continue
		}
		logInfo(g.Logger, "runtime component stopped", slog.String("component", name))
	}
	return errors.Join(errs...)
}

func componentName(index int, name string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return fmt.Sprintf("component[%d]", index)
}
