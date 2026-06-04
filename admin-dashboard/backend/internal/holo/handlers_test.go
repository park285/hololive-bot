package holo

import (
	"testing"

	"github.com/park285/shared-go/pkg/json"
)

func TestTrimChannelStats(t *testing.T) {
	body := []byte(`{"status":"ok","stats":{"b":{"subscriberCount":20},"a":{"subscriberCount":20},"c":{"subscriberCount":10}}}`)
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
		t.Fatalf("unexpected trim result: %s", trimmed)
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
