package main

import (
	"strings"
	"testing"
)

func TestEnvBool(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		want    bool
		wantErr string
	}{
		{name: "unset", value: "", want: false},
		{name: "true", value: "true", want: true},
		{name: "false", value: "false", want: false},
		{name: "trimmed", value: " 1 ", want: true},
		{name: "invalid", value: "enabled", wantErr: "must be a boolean"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MIGRATION_ALLOW_BLOCKING_INDEX_DROP", test.value)

			got, err := envBool("MIGRATION_ALLOW_BLOCKING_INDEX_DROP")

			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("envBool() error = %v, want containing %q", err, test.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("envBool() error = %v", err)
			}

			if got != test.want {
				t.Fatalf("envBool() = %t, want %t", got, test.want)
			}
		})
	}
}
