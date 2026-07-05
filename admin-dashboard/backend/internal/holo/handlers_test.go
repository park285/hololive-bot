package holo

import (
	"testing"

	"github.com/park285/shared-go/pkg/json"
)

func TestTrimChannelStats(t *testing.T) {
	body := []byte(`{"status":"ok","stats":{"b":{"SubscriberCount":30},"a":{"SubscriberCount":20},"c":{"SubscriberCount":10}}}`)
	trimmed, err := TrimChannelStats(body, 2)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &got); err != nil {
		t.Fatal(err)
	}
	var stats map[string]json.RawMessage
	if err := json.Unmarshal(got["stats"], &stats); err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats["a"] == nil || stats["b"] == nil {
		t.Fatalf("top-N by subscriber count must keep b and a: %s", trimmed)
	}
	if stats["c"] != nil {
		t.Fatalf("lowest subscriber channel must be trimmed: %s", trimmed)
	}
}

func TestValidateChannelStatsLimit(t *testing.T) {
	if _, err := validateChannelStatsLimit("501"); err == nil {
		t.Fatal("limit above max must fail")
	}
	if limit, err := validateChannelStatsLimit("500"); err != nil || limit != 500 {
		t.Fatalf("max limit should pass: %d %v", limit, err)
	}
}
