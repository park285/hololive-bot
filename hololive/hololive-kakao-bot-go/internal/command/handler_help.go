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
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type HelpCommand struct {
	deps *Dependencies
}

func NewHelpCommand(deps *Dependencies) *HelpCommand {
	return &HelpCommand{deps: deps}
}

func (c *HelpCommand) Name() string {
	return "help"
}

func (c *HelpCommand) Description() string {
	return "도움말을 표시합니다"
}

func (c *HelpCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	message := c.deps.Formatter.FormatHelp(ctx)

	return c.deps.SendMessage(ctx, cmdCtx.Room, message)
}

func (c *HelpCommand) ensureDeps() error {
	if c == nil || c.deps == nil {
		return errors.New("help command dependencies not configured")
	}

	if c.deps.SendMessage == nil {
		return errors.New("message callback not configured")
	}

	if c.deps.Formatter == nil {
		return errors.New("formatter not configured")
	}

	return nil
}
