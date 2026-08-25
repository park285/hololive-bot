package fxapp

import (
	"context"
	"slices"
	"sync"
)

type resourceOwner struct {
	mu        sync.Mutex
	steps     []func(context.Context)
	closeOnce sync.Once
}

func newResourceOwner() *resourceOwner {
	return &resourceOwner{}
}

func (o *resourceOwner) Add(step func(context.Context)) {
	if o == nil || step == nil {
		return
	}

	o.mu.Lock()

	o.steps = append(o.steps, step)
	o.mu.Unlock()
}

func (o *resourceOwner) Close(ctx context.Context) {
	if o == nil {
		return
	}

	o.closeOnce.Do(func() {
		o.mu.Lock()

		steps := append([]func(context.Context){}, o.steps...)
		o.mu.Unlock()

		for _, step := range slices.Backward(steps) {
			step(ctx)
		}
	})
}
