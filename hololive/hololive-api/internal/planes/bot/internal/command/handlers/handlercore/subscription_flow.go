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

import (
	"context"
	"fmt"
)

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
	isSubscribed, handled, err := f.subscriptionState(ctx, roomID)
	if err != nil {
		return fmt.Errorf("check subscription state: %w", err)
	}

	if handled {
		return nil
	}

	if isSubscribed {
		if subscribedErr := f.alreadySubscribed(ctx); subscribedErr != nil {
			return fmt.Errorf("%w", subscribedErr)
		}

		return nil
	}

	handled, err = f.subscribePort(ctx, roomID, roomName)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	if handled {
		return nil
	}

	if err := f.cfg.OnSubscribed(ctx); err != nil {
		return fmt.Errorf("on subscribed: %w", err)
	}

	return nil
}

func (f SubscriptionFlow) Unsubscribe(ctx context.Context, roomID string) error {
	isSubscribed, handled, err := f.subscriptionState(ctx, roomID)
	if err != nil {
		return fmt.Errorf("check subscription state: %w", err)
	}

	if handled {
		return nil
	}

	if !isSubscribed {
		if notSubscribedErr := f.notSubscribed(ctx); notSubscribedErr != nil {
			return fmt.Errorf("%w", notSubscribedErr)
		}

		return nil
	}

	handled, err = f.unsubscribePort(ctx, roomID)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	if handled {
		return nil
	}

	if err := f.cfg.OnUnsubscribed(ctx); err != nil {
		return fmt.Errorf("on unsubscribed: %w", err)
	}

	return nil
}

func (f SubscriptionFlow) alreadySubscribed(ctx context.Context) error {
	if err := f.cfg.OnAlreadySubscribed(ctx); err != nil {
		return fmt.Errorf("on already subscribed: %w", err)
	}

	return nil
}

func (f SubscriptionFlow) subscribePort(ctx context.Context, roomID, roomName string) (bool, error) {
	if err := f.cfg.Port.Subscribe(ctx, roomID, roomName); err != nil {
		if replyErr := f.cfg.OnSubscribeError(ctx, err); replyErr != nil {
			return true, fmt.Errorf("on subscribe error: %w", replyErr)
		}

		return true, nil
	}

	return false, nil
}

func (f SubscriptionFlow) notSubscribed(ctx context.Context) error {
	if err := f.cfg.OnNotSubscribed(ctx); err != nil {
		return fmt.Errorf("on not subscribed: %w", err)
	}

	return nil
}

func (f SubscriptionFlow) unsubscribePort(ctx context.Context, roomID string) (bool, error) {
	if err := f.cfg.Port.Unsubscribe(ctx, roomID); err != nil {
		if replyErr := f.cfg.OnUnsubscribeError(ctx, err); replyErr != nil {
			return true, fmt.Errorf("on unsubscribe error: %w", replyErr)
		}

		return true, nil
	}

	return false, nil
}

func (f SubscriptionFlow) subscriptionState(ctx context.Context, roomID string) (bool, bool, error) {
	isSubscribed, err := f.cfg.Port.IsSubscribed(ctx, roomID)
	if err == nil {
		return isSubscribed, false, nil
	}

	if replyErr := f.cfg.OnCheckError(ctx, err); replyErr != nil {
		return false, true, fmt.Errorf("on check error: %w", replyErr)
	}

	return false, true, nil
}

func (f SubscriptionFlow) Status(ctx context.Context, roomID string) error {
	isSubscribed, err := f.cfg.Port.IsSubscribed(ctx, roomID)
	if err != nil {
		if replyErr := f.cfg.OnCheckError(ctx, err); replyErr != nil {
			return fmt.Errorf("on check error: %w", replyErr)
		}

		return nil
	}

	if err := f.cfg.OnStatus(ctx, isSubscribed); err != nil {
		return fmt.Errorf("on status: %w", err)
	}

	return nil
}
