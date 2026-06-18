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

package handlercore

import "context"

type SubscriptionPort interface {
	IsSubscribed(ctx context.Context, roomID string) (bool, error)
	Subscribe(ctx context.Context, roomID, roomName string) error
	Unsubscribe(ctx context.Context, roomID string) error
}

type SubscriptionFlowConfig struct {
	Port SubscriptionPort

	OnCheckError        func(ctx context.Context, err error) error
	OnAlreadySubscribed func(ctx context.Context) error
	OnSubscribeError    func(ctx context.Context, err error) error
	OnSubscribed        func(ctx context.Context) error
	OnNotSubscribed     func(ctx context.Context) error
	OnUnsubscribeError  func(ctx context.Context, err error) error
	OnUnsubscribed      func(ctx context.Context) error
	OnStatus            func(ctx context.Context, subscribed bool) error
}

type SubscriptionFlow struct {
	cfg *SubscriptionFlowConfig
}

func NewSubscriptionFlow(cfg *SubscriptionFlowConfig) SubscriptionFlow {
	return SubscriptionFlow{cfg: cfg}
}

func (f SubscriptionFlow) Subscribe(ctx context.Context, roomID, roomName string) error {
	isSubscribed, err := f.cfg.Port.IsSubscribed(ctx, roomID)
	if err != nil {
		return f.cfg.OnCheckError(ctx, err)
	}

	if isSubscribed {
		return f.cfg.OnAlreadySubscribed(ctx)
	}

	if err := f.cfg.Port.Subscribe(ctx, roomID, roomName); err != nil {
		return f.cfg.OnSubscribeError(ctx, err)
	}

	return f.cfg.OnSubscribed(ctx)
}

func (f SubscriptionFlow) Unsubscribe(ctx context.Context, roomID string) error {
	isSubscribed, err := f.cfg.Port.IsSubscribed(ctx, roomID)
	if err != nil {
		return f.cfg.OnCheckError(ctx, err)
	}

	if !isSubscribed {
		return f.cfg.OnNotSubscribed(ctx)
	}

	if err := f.cfg.Port.Unsubscribe(ctx, roomID); err != nil {
		return f.cfg.OnUnsubscribeError(ctx, err)
	}

	return f.cfg.OnUnsubscribed(ctx)
}

func (f SubscriptionFlow) Status(ctx context.Context, roomID string) error {
	isSubscribed, err := f.cfg.Port.IsSubscribed(ctx, roomID)
	if err != nil {
		return f.cfg.OnCheckError(ctx, err)
	}

	return f.cfg.OnStatus(ctx, isSubscribed)
}
