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

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type DeliveryModule struct {
	Locker     delivery.NotificationLocker
	Repository *delivery.OutboxRepository
}

// BuildDeliveryModule은 digest 발송을 v2 notification_delivery_outbox에만 적재한다. 예전에 v3 ledger로 넘기던
// DELIVERY_OUTBOX_V3_HANDOFF_MODE는 DEC-20260926-hololive-outbox-v3-convergence로 삭제했고, 키가 남아 있으면
// settings의 퇴역 가드(config_outbox_v3_handoff_retired_env.go)가 기동을 거절한다.
func BuildDeliveryModule(
	cacheClient cache.Client,
	postgres database.Client,
	logger *slog.Logger,
) (*DeliveryModule, error) {
	locker, err := delivery.NewLocker(cacheClient, logger)
	if err != nil {
		return nil, fmt.Errorf("build delivery module: %w", err)
	}

	return &DeliveryModule{
		Locker:     locker,
		Repository: delivery.NewOutboxRepository(postgres, logger),
	}, nil
}
