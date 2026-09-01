package main

import (
	"io"
	"strings"
	"testing"
)

func TestParseCommandOptionsRequiresExplicitAttribution(t *testing.T) {
	tests := map[string][]string{
		"empty":              {},
		"missing operator":   {"--reason=bounded replay"},
		"missing reason":     {"--activated-by=operator"},
		"surrounding spaces": {"--activated-by= operator", "--reason=bounded replay"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCommandOptions(args, io.Discard); err == nil {
				t.Fatal("parse command options error = nil")
			}
		})
	}
}

func TestParseCommandOptionsPreservesAuditFields(t *testing.T) {
	options, err := parseCommandOptions([]string{
		"--activated-by=operator-a",
		"--reason=establish bounded historical replay",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parse command options: %v", err)
	}

	if options.activatedBy != "operator-a" || options.reason != "establish bounded historical replay" {
		t.Fatalf("options = %#v", options)
	}
}

func TestParseCommandOptionsRejectsOversizedReason(t *testing.T) {
	_, err := parseCommandOptions([]string{
		"--activated-by=operator-a",
		"--reason=" + strings.Repeat("x", 1025),
	}, io.Discard)
	if err == nil {
		t.Fatal("oversized reason error = nil")
	}
}

func TestReplayEpochHelpReturnsSuccess(t *testing.T) {
	if exitCode := run(t.Context(), []string{"--help"}, io.Discard); exitCode != 0 {
		t.Fatalf("help exit code = %d", exitCode)
	}
}
