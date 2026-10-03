package fxapp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

type resourceOwner struct {
	mu        sync.Mutex
	steps     []*resourceCloseStep
	closeInit sync.Once
	closeGate chan struct{}
	closeErr  error
}

type resourceCloseStep struct {
	run       func(context.Context) error
	resumable bool
	finished  bool
}

func newResourceOwner() *resourceOwner {
	return &resourceOwner{}
}

func (o *resourceOwner) Add(step func(context.Context) error) {
	o.add(step, false)
}

// AddResumable은 미완료 join을 같은 멱등 owner에서 이어갈 수 있는 runtime 자원을 등록한다.
func (o *resourceOwner) AddResumable(step func(context.Context) error) {
	o.add(step, true)
}

func (o *resourceOwner) add(step func(context.Context) error, resumable bool) {
	if o == nil || step == nil {
		return
	}

	o.mu.Lock()

	o.steps = append(o.steps, &resourceCloseStep{run: step, resumable: resumable})
	o.mu.Unlock()
}

func (o *resourceOwner) Close(ctx context.Context) error {
	if o == nil {
		return nil
	}

	o.closeInit.Do(func() { o.closeGate = make(chan struct{}, 1) })

	select {
	case o.closeGate <- struct{}{}:
	case <-ctx.Done():
		o.recordCloseError(fmt.Errorf("wait for process resource cleanup: %w", ctx.Err()))

		return o.closeError()
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		defer func() { <-o.closeGate }()

		o.mu.Lock()

		steps := slices.Clone(o.steps)
		o.mu.Unlock()

		for _, step := range slices.Backward(steps) {
			if step.finished {
				continue
			}

			err := step.run(ctx)
			// 단일 호출 자원은 실패도 최종 결과다. runtime은 같은 owner에 후속 join을 요청할 수 있다.
			step.finished = !step.resumable || err == nil
			o.recordCloseError(err)
		}
	}()

	select {
	case <-done:
		return o.closeError()
	case <-ctx.Done():
		o.recordCloseError(fmt.Errorf("join process resource cleanup: %w", ctx.Err()))

		return o.closeError()
	}
}

func (o *resourceOwner) recordCloseError(err error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if err != nil {
		o.closeErr = errors.Join(o.closeErr, err)
	}
}

func (o *resourceOwner) closeError() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.closeErr
}
