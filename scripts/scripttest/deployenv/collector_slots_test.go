package deployenv

import (
	"path/filepath"
	"strings"
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

func TestCollectorSlotsFileFollowsTheSourcedLibrary(t *testing.T) {
	t.Parallel()

	lib := harness.Path(t, "scripts/deploy/lib")

	want, err := filepath.EvalSymlinks(filepath.Join(lib, "collector-slots.tsv"))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]harness.Cmd{
		"absolute":  {Script: `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.sh"; collector_slots_file`},
		"relative":  {Script: `. scripts/deploy/lib/collector-slots.sh; collector_slots_file`, Dir: harness.Root(t)},
		"bare name": {Script: `. collector-slots.sh; collector_slots_file`, Dir: lib},
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := harness.Bash(t, cmd)
			harness.Succeeds(t, name, r)

			if got := strings.TrimSuffix(r.Stdout, "\n"); got != want {
				t.Fatalf("%s: got %q, want %q", name, got, want)
			}
		})
	}
}
