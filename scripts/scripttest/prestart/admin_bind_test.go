package prestart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const bindLibs = `. "$SCRIPTTEST_ROOT/scripts/deploy/lib/admin-bind.sh"
. "$SCRIPTTEST_ROOT/scripts/deploy/lib/postgres-capacity.sh"
tailscale() {
  [[ "${TAILSCALE_DOWN:-}" != 1 && "$1" == ip && "$2" == --assert && ( "$3" == 100.100.1.8 || "$3" == fd7a:115c:a1e0::8 ) ]]
}
`

func bindCheck(t *testing.T, call, bindIP string, env ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{Script: bindLibs + call, Args: []string{bindIP}, Env: env})
}

func TestAdminBindAcceptsOnlyTheTailnetIdentity(t *testing.T) {
	t.Parallel()

	for _, ip := range []string{"100.100.1.8", "fd7a:115c:a1e0::8"} {
		harness.Succeeds(t, ip, bindCheck(t, `admin_bind_assert_host "$1"`, ip))
	}

	for _, ip := range []string{
		"", "0.0.0.0", "::", "127.0.0.1", "224.0.0.1", "ff02::1", "example.invalid", "100.100.1.9",
		"100.100.1.8.example.invalid", "100.100.1.256", "fd7a:115c:a1e0::9", "fd7a:115c:a1e0::8%eth0",
	} {
		harness.Fails(t, "admin bind "+ip, bindCheck(t, `admin_bind_assert_host "$1"`, ip))
	}

	harness.Fails(t, "unknown host identity", bindCheck(t, `admin_bind_assert_host "$1"`, "100.100.1.8", "TAILSCALE_DOWN=1"))
}

func TestPostgresBindRequiresThePrimary(t *testing.T) {
	t.Parallel()

	call := `admin_postgres_bind_assert "$SCRIPTTEST_ROOT" "$1"`
	harness.Succeeds(t, "primary", bindCheck(t, call, "100.100.1.8"))

	for _, ip := range []string{"", "100.100.1.3", "0.0.0.0"} {
		harness.Fails(t, "postgres bind "+ip, bindCheck(t, call, ip))
	}

	harness.Fails(t, "no host identity", bindCheck(t, call, "100.100.1.8", "TAILSCALE_DOWN=1"))
}

var firewallMutations = []string{
	`.nftables[0].set.elem = []`,
	`.nftables[0].set.elem += ["0.0.0.0/0"]`,
	`del(.nftables[1])`,
	`del(.nftables[2])`,
	`del(.nftables[4])`,
	`.nftables[4].rule.expr[1].match.right = 6433`,
	`del(.nftables[4].rule.expr[4])`,
	`del(.nftables[5])`,
	`.nftables[6].chain.prio = -10`,
	`del(.nftables[7])`,
	`.nftables[7].rule.expr[-1].mangle.value = 0`,
	`del(.nftables[7].rule.expr[4])`,
	`del(.nftables[] | select(.chain.name == "input"))`,
	`del(.nftables[] | select(.rule.chain == "input" and .rule.expr[-1].reject.type == "tcp reset" and .rule.expr[0].match.right == 5433))`,
	`(.nftables[] | select(.rule.chain == "input" and .rule.expr[0].match.right == "tailscale0" and .rule.expr[-2].match.right == 5433) | .rule.expr) |= .[1:]`,
	`.nftables |= (.[0:9] + [{"rule":{"chain":"input","expr":[{"accept":null}]}}] + .[9:])`,
	`(.nftables[] | select(.rule.chain == "input") | .rule.expr[] | select(.match.right == 5433) | .match.right) |= {"set":[5433,6433]}`,
	`(.nftables | map(.rule.chain == "input") | index(true)) as $i | .nftables |= (.[0:$i] + [{"rule":{"chain":"input","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":{"range":[5000,6500]}}},{"accept":null}]}}] + .[$i:])`,
	`(.nftables | map(.rule.chain == "input") | index(true)) as $i | .nftables |= (.[0:$i] + [{"rule":{"chain":"input","expr":[{"match":{"op":"==","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":"@wide_ports"}},{"accept":null}]}}] + .[$i:])`,
	`(.nftables | map(.rule.chain == "input") | index(true)) as $i | .nftables |= (.[0:$i] + [{"rule":{"chain":"input","expr":[{"match":{"op":"!=","left":{"payload":{"protocol":"tcp","field":"dport"}},"right":22}},{"accept":null}]}}] + .[$i:])`,
}

func firewallCheck(t *testing.T, ruleset, installed string, env ...string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: bindLibs + `systemctl() { [[ "${INACTIVE:-}" != 1 ]]; }
nft() { cat "$RULESET"; }
admin_network_assert_firewall "$SCRIPTTEST_ROOT/scripts/systemd/admin-dashboard-ingress.nft" "$INSTALLED"
`,
		Env: append([]string{"RULESET=" + ruleset, "INSTALLED=" + installed}, env...),
	})
}

func TestAdminFirewallPreflightRejectsIncompleteRulesets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	installed := filepath.Join(dir, "installed.nft")
	copyInto(t, "scripts/systemd/admin-dashboard-ingress.nft", installed)

	valid := harness.Path(t, "scripts/deploy/testdata/ingress-firewall.json")
	harness.Succeeds(t, "valid firewall", firewallCheck(t, valid, installed))
	harness.Fails(t, "inactive firewall", firewallCheck(t, valid, installed, "INACTIVE=1"))

	for i, filter := range firewallMutations {
		out, err := exec.CommandContext(t.Context(), "jq", filter, valid).Output() // #nosec G204 -- 고정된 jq 변형만 실행한다.
		if err != nil {
			t.Fatalf("jq %q: %v", filter, err)
		}

		mutated := filepath.Join(dir, "firewall.json")
		if err := os.WriteFile(mutated, out, 0o600); err != nil {
			t.Fatal(err)
		}

		harness.Fails(t, fmt.Sprintf("mutation %d: %s", i, filter), firewallCheck(t, mutated, installed))
	}

	harness.Write(t, installed, harness.Read(t, installed)+"# drift\n")
	harness.Fails(t, "unreviewed installed firewall", firewallCheck(t, valid, installed))
}

func copyInto(t *testing.T, rel, dst string) {
	t.Helper()

	data, err := os.ReadFile(harness.Path(t, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	harness.Write(t, dst, string(data))
}
