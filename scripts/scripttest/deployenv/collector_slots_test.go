package deployenv

import (
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const collectorSlotCaller = `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.sh"
eval "$(collector_slot_preamble a "$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.tsv")"
`

func TestCollectorSlotMatchesCallerValues(t *testing.T) {
	t.Parallel()

	table := harness.Path(t, "scripts/deploy/lib/collector-slots.tsv")
	empty := filepath.Join(t.TempDir(), "collector-slots.tsv")
	harness.Write(t, empty, "")

	cases := map[string]struct {
		body    string
		table   string
		matches bool
	}{
		"same table":      {body: `collector_slot_matches a "$1"`, table: table, matches: true},
		"different value": {body: `slot_port=1; collector_slot_matches a "$1"`, table: table, matches: false},
		"other slot":      {body: `collector_slot_matches b "$1"`, table: table, matches: false},
		"empty table":     {body: `collector_slot_matches a "$1"`, table: empty, matches: false},
		"missing table":   {body: `collector_slot_matches a "$1"`, table: empty + ".missing", matches: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := harness.Bash(t, harness.Cmd{Script: collectorSlotCaller + tc.body, Args: []string{tc.table}})
			if tc.matches {
				harness.Succeeds(t, name, r)
			} else {
				harness.Fails(t, name, r)
			}
		})
	}
}
