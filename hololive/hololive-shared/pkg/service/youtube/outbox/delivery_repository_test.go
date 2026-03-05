package outbox

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestUniqueStrings(t *testing.T) {
	t.Parallel()

	in := []string{"room1", "room2", "room1", "room3", "room2"}
	got := uniqueStrings(in)
	want := []string{"room1", "room2", "room3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueStrings() = %#v, want %#v", got, want)
	}

	if out := uniqueStrings(nil); out != nil {
		t.Fatalf("uniqueStrings(nil) = %#v, want nil", out)
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
		{name: "sent when no pending", pending: 0, sent: 1, failed: 9, want: domain.OutboxStatusSent},
		{name: "failed when only failed", pending: 0, sent: 0, failed: 2, want: domain.OutboxStatusFailed},
		{name: "empty fallback pending", pending: 0, sent: 0, failed: 0, want: domain.OutboxStatusPending},
	}

	for _, tt := range tests {
		tt := tt
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
