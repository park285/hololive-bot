// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package dbtest

import (
	"os"
	"regexp"
	"testing"

	"github.com/jackc/pgx/v5"
)

// 중앙 Quadlet unit은 [Service] Environment=로 compose.env가 비었을 때의 기본값을 둔다.
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

	for _, unit := range []string{"hololive-api", "hololive-alarm-worker", "hololive-youtube-collector-c"} {
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
