package deployenv

import (
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const composeServices = `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/compose-services.sh"
slots="$SCRIPTTEST_ROOT/scripts/deploy/lib/collector-slots.tsv"
`

func TestComposeServiceLogTargets(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"hololive-api":          `printf '%s\n' hololive-api; compose_service_resolve_log_target hololive-api`,
		"hololive-alarm-worker": `printf '%s\n' hololive-alarm-worker; compose_service_resolve_log_target hololive-alarm-worker`,
		"central collector": `slot="$(collector_slots_where host central "$slots")"
unit="$(collector_slot "$slot" collector_unit "$slots")"
printf '%s\n' "${unit%.service}"
compose_service_resolve_log_target "$(collector_slot "$slot" service "$slots")"`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := harness.Bash(t, harness.Cmd{Script: composeServices + body})
			harness.Succeeds(t, name, r)

			lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
			if len(lines) != 2 || lines[0] != lines[1] {
				t.Errorf("log target %s: want %q, got output %q", name, lines[0], r.Stdout)
			}
		})
	}
}

func TestComposeServiceLogTargetRejectsNonCentralCollectors(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"bare collector":        `compose_service_resolve_log_target collector`,
		"non-central collector": `compose_service_resolve_log_target "$(collector_slot b service "$slots")"`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			harness.Fails(t, name, harness.Bash(t, harness.Cmd{Script: composeServices + body}))
		})
	}
}
