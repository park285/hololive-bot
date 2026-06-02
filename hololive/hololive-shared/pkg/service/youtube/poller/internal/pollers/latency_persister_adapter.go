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

package pollers

import (
	"context"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"
)

// deliveryTelemetryLatencyPersisterAdapter는 outbox.DeliveryTelemetryRepository를
// batchrepo.PostLatencyClassificationPersister 인터페이스에 맞게 연결하는 composition-root 어댑터입니다.
type deliveryTelemetryLatencyPersisterAdapter struct {
	db dbx.Querier
}

func newDeliveryTelemetryLatencyPersisterAdapter(db dbx.Querier) batchrepo.PostLatencyClassificationPersister {
	return &deliveryTelemetryLatencyPersisterAdapter{db: db}
}

func (a *deliveryTelemetryLatencyPersisterAdapter) PersistPostLatencyClassificationsByIdentities(
	ctx context.Context,
	identities []batchrepo.LatencyClassificationIdentity,
) error {
	outboxIdentities := make([]outbox.PostTrackingIdentity, 0, len(identities))
	for i := range identities {
		outboxIdentities = append(outboxIdentities, outbox.PostTrackingIdentity{
			Kind:      identities[i].Kind,
			ContentID: identities[i].ContentID,
		})
	}
	return outbox.NewDeliveryTelemetryRepository(a.db).PersistPostLatencyClassificationsByIdentities(ctx, outboxIdentities)
}
