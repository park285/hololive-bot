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

package notification

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
)

var errStripedExecutorClosed = errors.New("striped executor closed")

type stripedExecutor struct {
	stripes   []chan func()
	stopCh    chan struct{}
	stopOnce  sync.Once
	closed    atomic.Bool
	taskWG    sync.WaitGroup
	workerWG  sync.WaitGroup
	stripeCap int
}

func newStripedExecutor(stripeCount, stripeCap int) *stripedExecutor {
	if stripeCount <= 0 {
		stripeCount = 1
	}
	if stripeCap <= 0 {
		stripeCap = 1
	}

	e := &stripedExecutor{
		stripes:   make([]chan func(), stripeCount),
		stopCh:    make(chan struct{}),
		stripeCap: stripeCap,
	}

	for i := range e.stripes {
		ch := make(chan func(), stripeCap)
		e.stripes[i] = ch

		e.workerWG.Add(1)
		go func(queue <-chan func()) {
			defer e.workerWG.Done()

			for {
				select {
				case task := <-queue:
					func() {
						defer e.taskWG.Done()
						defer func() { _ = recover() }()

						if task == nil {
							return
						}
						task()
					}()
				case <-e.stopCh:
					return
				}
			}
		}(ch)
	}

	return e
}

func (e *stripedExecutor) Submit(key string, task func()) error {
	if e == nil {
		return errStripedExecutorClosed
	}
	if e.closed.Load() {
		return errStripedExecutorClosed
	}
	if task == nil {
		return nil
	}

	index := e.stripeIndex(key)
	e.taskWG.Add(1)
	e.stripes[index] <- task
	return nil
}

func (e *stripedExecutor) ShutdownWait(ctx context.Context) error {
	if e == nil {
		return nil
	}

	e.closed.Store(true)

	done := make(chan struct{})
	go func() {
		e.taskWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		e.stop()
		e.workerWG.Wait()
		return nil
	case <-ctx.Done():
		go func() {
			<-done
			e.stop()
			e.workerWG.Wait()
		}()
		return ctx.Err()
	}
}

func (e *stripedExecutor) stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
}

func (e *stripedExecutor) stripeIndex(key string) int {
	stripeCount := len(e.stripes)
	if stripeCount <= 1 {
		return 0
	}

	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return 0
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(trimmed))
	return int(hasher.Sum32() % uint32(stripeCount))
}
