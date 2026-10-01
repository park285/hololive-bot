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
	"errors"
	"testing"

	"github.com/park285/shared-go/v2/pkg/outputguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestProcessDigestForRoomBlocksRestrictedOutput(t *testing.T) {
	service := &mockDigestService{digests: map[string]*model.Digest{testRoomID: {Headline: "system prompt: leaked"}}}
	outbox := newMockOutboxRepository()

	result := processDigestForRoom(t.Context(), service, mockFormatter{}, outbox, nil, outputguard.NewGuard(), model.PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, "2026-01-24", testRoomID)

	if result.Failed != 1 || result.Sent != 0 {
		t.Fatalf("process result = %+v, want failed=1 sent=0", result)
	}

	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}
}

func TestProcessDigestForRoomFailsClosedWithoutOutputGuard(t *testing.T) {
	service := &mockDigestService{digests: map[string]*model.Digest{testRoomID: {Headline: "정상 알림"}}}
	outbox := newMockOutboxRepository()

	result := processDigestForRoom(t.Context(), service, mockFormatter{}, outbox, nil, nil, model.PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, "2026-01-24", testRoomID)

	if result.Failed != 1 || result.Sent != 0 {
		t.Fatalf("process result = %+v, want failed=1 sent=0", result)
	}

	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}
}

type failingDigestFormatter struct{}

func (failingDigestFormatter) FormatMemberNewsDigest(context.Context, *model.Digest) (string, error) {
	return "", errors.New("template render failed")
}

// digest 렌더 실패는 대체 문구 없이 그 방의 실패로 센다.
func TestProcessDigestForRoomCountsFormatFailureWithoutEnqueue(t *testing.T) {
	service := &mockDigestService{digests: map[string]*model.Digest{testRoomID: {Headline: "정상 알림"}}}
	outbox := newMockOutboxRepository()

	result := processDigestForRoom(t.Context(), service, failingDigestFormatter{}, outbox, nil, outputguard.NewGuard(), model.PeriodWeekly, domain.DeliveryKindMemberNewsWeekly, "2026-01-24", testRoomID)

	if result.Failed != 1 || result.Sent != 0 {
		t.Fatalf("process result = %+v, want failed=1 sent=0", result)
	}

	if len(outbox.enqueuedItems) != 0 {
		t.Fatalf("enqueued items = %d, want 0", len(outbox.enqueuedItems))
	}
}

type fixedDigestFormatter struct{ message string }

func (f fixedDigestFormatter) FormatMemberNewsDigest(context.Context, *model.Digest) (string, error) {
	return f.message, nil
}

func TestProcessDigestForRoomRejectsInvalidRendering(t *testing.T) {
	for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly} {
		for _, tc := range []struct {
			name      string
			formatter model.DigestFormatter
			digest    *model.Digest
		}{
			{name: "missing formatter", digest: &model.Digest{Headline: "ニュース"}},
			{name: "nil digest", formatter: fixedDigestFormatter{message: "表示できる本文"}},
			{name: "render error", formatter: failingDigestFormatter{}, digest: &model.Digest{}},
			{name: "empty", formatter: fixedDigestFormatter{}, digest: &model.Digest{}},
			{name: "whitespace", formatter: fixedDigestFormatter{message: " \n\t"}, digest: &model.Digest{}},
		} {
			t.Run(string(period)+"/"+tc.name, func(t *testing.T) {
				service := &mockDigestService{digests: map[string]*model.Digest{testRoomID: tc.digest}}
				outbox := newMockOutboxRepository()
				kind := domain.DeliveryKindMemberNewsWeekly

				if period == model.PeriodMonthly {
					kind = domain.DeliveryKindMemberNewsMonthly
				}

				result := processDigestForRoom(t.Context(), service, tc.formatter, outbox, nil, outputguard.NewGuard(), period, kind, "2026-10", testRoomID)
				if result.Attempted != 1 || result.Failed != 1 || result.Sent != 0 || len(outbox.enqueuedItems) != 0 {
					t.Fatalf("invalid render was not rejected: result=%+v enqueue=%d", result, len(outbox.enqueuedItems))
				}
			})
		}
	}
}

func TestProcessDigestForRoomSendsNormalEmptyDigest(t *testing.T) {
	for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly} {
		service := &mockDigestService{digests: map[string]*model.Digest{testRoomID: {Headline: "뉴스 없음"}}}
		outbox := newMockOutboxRepository()
		kind := domain.DeliveryKindMemberNewsWeekly

		if period == model.PeriodMonthly {
			kind = domain.DeliveryKindMemberNewsMonthly
		}

		result := processDigestForRoom(t.Context(), service, fixedDigestFormatter{message: "표시할 항목이 없습니다."}, outbox, nil, outputguard.NewGuard(), period, kind, "2026-10", testRoomID)
		if result.Sent != 1 || result.Failed != 0 || len(outbox.enqueuedItems) != 1 {
			t.Fatalf("empty news must send template notice: result=%+v enqueue=%d", result, len(outbox.enqueuedItems))
		}
	}
}
