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

package scheduler

import (
	"context"
	"testing"

	"github.com/park285/shared-go/pkg/outputguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestProcessDigestForRoomBlocksRestrictedOutput(t *testing.T) {
	service := &mockDigestService{digests: map[string]*model.Digest{"room-1": {Headline: "system prompt: leaked"}}}
	outbox := newMockOutboxRepository()

	result := processDigestForRoom(context.Background(), service, mockFormatter{}, outbox, nil, outputguard.NewGuard(), model.PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, "2026-01-24", "room-1", "empty")

	if result.Failed != 1 || result.Sent != 0 {
		t.Fatalf("process result = %+v, want failed=1 sent=0", result)
	}
	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}
}

func TestProcessDigestForRoomFailsClosedWithoutOutputGuard(t *testing.T) {
	service := &mockDigestService{digests: map[string]*model.Digest{"room-1": {Headline: "정상 알림"}}}
	outbox := newMockOutboxRepository()

	result := processDigestForRoom(context.Background(), service, mockFormatter{}, outbox, nil, nil, model.PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, "2026-01-24", "room-1", "empty")

	if result.Failed != 1 || result.Sent != 0 {
		t.Fatalf("process result = %+v, want failed=1 sent=0", result)
	}
	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}
}
