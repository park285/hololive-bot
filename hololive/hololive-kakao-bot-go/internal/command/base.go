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
	"errors"
	"log/slog"
)

// BaseCommand: 모든 커맨드가 공통으로 가지는 기본 의존성과 검증 로직을 제공합니다.
type BaseCommand struct {
	deps *Dependencies
}

// NewBaseCommand: 새로운 BaseCommand 인스턴스를 생성합니다.
func NewBaseCommand(deps *Dependencies) BaseCommand {
	return BaseCommand{deps: deps}
}

// EnsureBaseDeps: 기본 의존성이 올바르게 설정되었는지 검증합니다.
// 모든 커맨드에서 공통으로 필요한 SendMessage, SendError, Logger를 확인한다.
func (b *BaseCommand) EnsureBaseDeps() error {
	if b == nil || b.deps == nil {
		return errors.New("command dependencies not configured")
	}

	if b.deps.SendMessage == nil || b.deps.SendError == nil {
		return errors.New("message callbacks not configured")
	}

	if b.deps.Logger == nil {
		b.deps.Logger = slog.Default()
	}

	return nil
}

// Deps: 의존성 객체를 반환합니다.
func (b *BaseCommand) Deps() *Dependencies {
	if b == nil {
		return nil
	}

	return b.deps
}
