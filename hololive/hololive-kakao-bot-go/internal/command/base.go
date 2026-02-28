package command

import (
	"fmt"
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
		return fmt.Errorf("command dependencies not configured")
	}

	if b.deps.SendMessage == nil || b.deps.SendError == nil {
		return fmt.Errorf("message callbacks not configured")
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
