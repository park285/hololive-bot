package json

import (
	"strings"
	"testing"
)

func TestRawMessageMarshalJSON_NilBecomesNull(t *testing.T) {
	t.Parallel()

	var msg RawMessage
	got, err := msg.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	if string(got) != "null" {
		t.Fatalf("MarshalJSON() = %s, want null", string(got))
	}
}

func TestRawMessageUnmarshalJSON_NilPointer(t *testing.T) {
	t.Parallel()

	var msg *RawMessage
	err := msg.UnmarshalJSON([]byte(`{"name":"kapu"}`))
	if err == nil {
		t.Fatal("UnmarshalJSON() expected error")
	}
	if !strings.Contains(err.Error(), "nil pointer") {
		t.Fatalf("UnmarshalJSON() error = %q, expected nil pointer", err.Error())
	}
}

func TestNumberParsers_InvalidInput(t *testing.T) {
	t.Parallel()

	if _, err := Number("not-a-number").Float64(); err == nil {
		t.Fatal("Float64() expected error for invalid input")
	}
	if _, err := Number("3.14").Int64(); err == nil {
		t.Fatal("Int64() expected error for non-integer input")
	}
}
