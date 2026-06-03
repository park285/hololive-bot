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

package store

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestUniqueStrings(t *testing.T) {
	t.Parallel()

	in := []string{"room1", "room2", "room1", "room3", "room2"}
	got := UniqueStrings(in)
	want := []string{"room1", "room2", "room3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("UniqueStrings() = %#v, want %#v", got, want)
	}

	if out := UniqueStrings(nil); out != nil {
		t.Fatalf("UniqueStrings(nil) = %#v, want nil", out)
	}
}

func TestResolveOutboxStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pending int64
		sent    int64
		failed  int64
		want    domain.OutboxStatus
	}{
		{name: "pending has priority", pending: 1, sent: 10, failed: 10, want: domain.OutboxStatusPending},
		{name: "failed when mixed sent and failed", pending: 0, sent: 1, failed: 9, want: domain.OutboxStatusFailed},
		{name: "sent when only sent", pending: 0, sent: 1, failed: 0, want: domain.OutboxStatusSent},
		{name: "failed when only failed", pending: 0, sent: 0, failed: 2, want: domain.OutboxStatusFailed},
		{name: "empty fallback pending", pending: 0, sent: 0, failed: 0, want: domain.OutboxStatusPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveOutboxStatus(tt.pending, tt.sent, tt.failed)
			if got != tt.want {
				t.Fatalf("resolveOutboxStatus() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestParseStatusCounts(t *testing.T) {
	t.Parallel()

	counts := []deliveryStatusCount{
		{Status: domain.OutboxStatusPending, Count: 3},
		{Status: domain.OutboxStatusSent, Count: 5},
		{Status: domain.OutboxStatusFailed, Count: 1},
	}

	pending, sent, failed := parseStatusCounts(counts)
	if pending != 3 || sent != 5 || failed != 1 {
		t.Fatalf("parseStatusCounts() = (%d,%d,%d), want (3,5,1)", pending, sent, failed)
	}
}

func TestGroupOutboxIDsByAggregateStatus(t *testing.T) {
	t.Parallel()

	grouped := groupOutboxIDsByAggregateStatus([]int64{1, 2, 3}, []deliveryStatusCount{
		{OutboxID: 1, Status: domain.OutboxStatusSent, Count: 2},
		{OutboxID: 2, Status: domain.OutboxStatusPending, Count: 1},
		{OutboxID: 2, Status: domain.OutboxStatusSent, Count: 1},
		{OutboxID: 3, Status: domain.OutboxStatusFailed, Count: 1},
	})

	if !reflect.DeepEqual(grouped[domain.OutboxStatusSent], []int64{1}) {
		t.Fatalf("sent group = %#v, want [1]", grouped[domain.OutboxStatusSent])
	}
	if !reflect.DeepEqual(grouped[domain.OutboxStatusPending], []int64{2}) {
		t.Fatalf("pending group = %#v, want [2]", grouped[domain.OutboxStatusPending])
	}
	if !reflect.DeepEqual(grouped[domain.OutboxStatusFailed], []int64{3}) {
		t.Fatalf("failed group = %#v, want [3]", grouped[domain.OutboxStatusFailed])
	}
}
