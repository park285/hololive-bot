package command

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// HelpCommand: 도움말 정보를 출력하는 커맨드 핸들러
type HelpCommand struct {
	deps *Dependencies
}

// NewHelpCommand: 도움말 커맨드 핸들러를 생성합니다.
func NewHelpCommand(deps *Dependencies) *HelpCommand {
	return &HelpCommand{deps: deps}
}

// Name: 커맨드의 이름("help")을 반환합니다.
func (c *HelpCommand) Name() string {
	return "help"
}

// Description: 커맨드에 대한 설명을 반환합니다.
func (c *HelpCommand) Description() string {
	return "도움말을 표시합니다"
}

// Execute: 도움말 메시지를 생성하여 채팅방에 전송합니다.
func (c *HelpCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}
	message := c.deps.Formatter.FormatHelp(ctx)
	return c.deps.SendMessage(ctx, cmdCtx.Room, message)
}

func (c *HelpCommand) ensureDeps() error {
	if c == nil || c.deps == nil {
		return fmt.Errorf("help command dependencies not configured")
	}

	if c.deps.SendMessage == nil {
		return fmt.Errorf("message callback not configured")
	}

	if c.deps.Formatter == nil {
		return fmt.Errorf("formatter not configured")
	}

	return nil
}
