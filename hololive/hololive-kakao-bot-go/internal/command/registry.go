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
	"errors"
	"fmt"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// ErrUnknownCommand: 등록되지 않은 명령어를 호출했을 때 발생하는 오류.
var ErrUnknownCommand = errors.New("unknown command")

// Registry: 봇의 모든 명령어 핸들러를 등록하고 관리하며, 이름 기반 조회 및 실행을 담당하는 레지스트리.
type Registry struct {
	mu        sync.RWMutex
	handlers  map[string]Command
	aliasKeys map[string]string
}

// NewRegistry: 새로운 명령어 레지스트리 인스턴스를 생성합니다.
func NewRegistry() *Registry {
	return &Registry{
		handlers:  make(map[string]Command),
		aliasKeys: make(map[string]string),
	}
}

// Register: 새로운 명령어 핸들러를 레지스트리에 등록한다. (이름 정규화 적용).
func (r *Registry) Register(handler Command) {
	if handler == nil {
		return
	}

	name := stringutil.Normalize(handler.Name())

	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[name] = handler
}

// Execute: 주어진 키(명령어 이름)에 해당하는 핸들러를 찾아 명령을 실행한다. (스레드 안전).
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

// Count: 현재 등록된 명령어의 총 개수를 반환합니다.
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
