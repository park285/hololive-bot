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

func TestProductionComposePinsTheTestPoolQueryExecMode(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../deploy/compose/docker-compose.prod.yml")
	if err != nil {
		t.Fatalf("read production compose: %v", err)
	}

	match := regexp.MustCompile(
		`POSTGRES_QUERY_EXEC_MODE:\s*\$\{POSTGRES_QUERY_EXEC_MODE:-([a-z_]+)\}`,
	).FindSubmatch(raw)
	if match == nil {
		t.Fatal("production compose no longer declares a POSTGRES_QUERY_EXEC_MODE default; update the test pool contract together")
	}

	composeDefault := string(match[1])

	if composeDefault != productionComposeQueryExecModeName {
		t.Fatalf("production compose default query exec mode = %q, test pools run %q — keep both sides in one commit",
			composeDefault, productionComposeQueryExecModeName)
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
