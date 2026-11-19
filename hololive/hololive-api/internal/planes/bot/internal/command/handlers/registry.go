package handlers

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/park285/shared-go/core/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
)

var ErrUnknownCommand = errors.New("unknown command")

type Registry struct {
	mu       sync.RWMutex
	handlers map[string]handlercore.Command
}

func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]handlercore.Command),
	}
}

func (r *Registry) Register(handler handlercore.Command) {
	if handler == nil {
		return
	}

	name := stringutil.Normalize(handler.Name())

	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[name] = handler
}

func (r *Registry) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, key string, params map[string]any) error {
	if r == nil {
		return errors.New("command registry is nil")
	}

	handler := r.getHandler(key)
	if handler == nil {
		return fmt.Errorf("%w: %s", ErrUnknownCommand, key)
	}

	if err := handler.Execute(ctx, cmdCtx, params); err != nil {
		return fmt.Errorf("command %s: %w", key, err)
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

func (r *Registry) getHandler(key string) handlercore.Command {
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
