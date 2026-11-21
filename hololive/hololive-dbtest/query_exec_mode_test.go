package dbtest

import (
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5"
)

var quadletQueryExecModePattern = regexp.MustCompile(`(?m)^Environment=POSTGRES_QUERY_EXEC_MODE=([a-z_]+)$`)

func TestProductionComposePinsTheTestPoolQueryExecMode(t *testing.T) {
	t.Parallel()

	unitDir, err := os.OpenRoot("../../deploy/quadlet/hololive-bot")
	if err != nil {
		t.Fatalf("open production Quadlet unit directory: %v", err)
	}

	defer func() {
		if closeErr := unitDir.Close(); closeErr != nil {
			t.Fatalf("close production Quadlet unit directory: %v", closeErr)
		}
	}()

	for _, unit := range []string{"hololive-api", "hololive-alarm-worker", "hololive-collector-c"} {
		raw, err := unitDir.ReadFile(unit + ".container")
		if err != nil {
			t.Fatalf("read production Quadlet unit %s: %v", unit, err)
		}

		match := quadletQueryExecModePattern.FindSubmatch(raw)
		if match == nil {
			t.Fatalf("%s no longer declares a POSTGRES_QUERY_EXEC_MODE default; update the test pool contract together", unit)
		}

		if unitDefault := string(match[1]); unitDefault != productionComposeQueryExecModeName {
			t.Fatalf("%s default query exec mode = %q, test pools run %q — keep both sides in one commit",
				unit, unitDefault, productionComposeQueryExecModeName)
		}
	}

	modesByName := map[string]pgx.QueryExecMode{
		"cache_statement": pgx.QueryExecModeCacheStatement,
		"cache_describe":  pgx.QueryExecModeCacheDescribe,
		"describe_exec":   pgx.QueryExecModeDescribeExec,
		"exec":            pgx.QueryExecModeExec,
		"simple_protocol": pgx.QueryExecModeSimpleProtocol,
	}
	want, ok := modesByName[productionComposeQueryExecModeName]

	if !ok {
		t.Fatalf("unknown query exec mode name %q", productionComposeQueryExecModeName)
	}

	if productionQueryExecMode != want {
		t.Fatalf("test pool query exec mode = %v, production compose default resolves to %v", productionQueryExecMode, want)
	}
}
