package main

import "testing"

func TestQuoteSQLLiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "''"},
		{name: "plain", in: "secret", want: "'secret'"},
		{name: "single quote", in: "pa'ss", want: "'pa''ss'"},
		{name: "multiple quotes", in: "a'b'c", want: "'a''b''c'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := quoteSQLLiteral(tt.in); got != tt.want {
				t.Fatalf("quoteSQLLiteral(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
