package communityshortscli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunListsConsolidatedCommands(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run() exit=%d want=0", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q want empty", stdout.String())
	}
	for _, command := range []string{"send-counts", "delivery-logs", "channel-summary", "target-baseline"} {
		if !strings.Contains(stderr.String(), command) {
			t.Fatalf("usage missing %q:\n%s", command, stderr.String())
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	code := Run([]string{"missing"}, nil, &stderr)
	if code != 2 {
		t.Fatalf("Run() exit=%d want=2", code)
	}
	if !strings.Contains(stderr.String(), `unknown command "missing"`) {
		t.Fatalf("stderr=%q", stderr.String())
	}
}
