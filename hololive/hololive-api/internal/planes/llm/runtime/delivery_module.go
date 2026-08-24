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

package runtime

import (
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/envutil"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

const deliveryOutboxV3HandoffModeEnv = "DELIVERY_OUTBOX_V3_HANDOFF_MODE"

type DeliveryModule struct {
	Locker     delivery.NotificationLocker
	Repository *delivery.OutboxRepository
}

func BuildDeliveryModule(
	cacheClient cache.Client,
	postgres database.Client,
	logger *slog.Logger,
) (*DeliveryModule, error) {
	locker := delivery.NewLocker(cacheClient, logger)

	mode, err := handoff.ParseMode(envutil.String(deliveryOutboxV3HandoffModeEnv, "off"))
	if err != nil {
		return nil, fmt.Errorf("parse mode: %w", err)
	}

	var options []delivery.RepositoryOption

	if mode != handoff.ModeOff {
		publisher := queue.NewPublisher(
			cacheClient,
			logger,
			queue.WithOutbox(dispatchoutbox.NewPgxRepository(postgres, logger)),
		)

		options = append(options, delivery.WithDispatchHandoff(mode, deliveryDispatchPublisher{publisher: publisher}))
	}

	repository := delivery.NewOutboxRepository(postgres, logger, options...)

	return &DeliveryModule{
		Locker:     locker,
		Repository: repository,
	}, nil
}
