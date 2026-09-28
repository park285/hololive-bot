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

package messagestrings

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// 시드 SQL 기반 계약 테스트(DEC-20260926-hololive-message-strings-startup-validation).
// DB 없이 migration manifest 순서대로 message_strings INSERT를 읽어 운영 시드 값을 재구성하고,
// 타입 있는 key 전체가 비어 있지 않은 값을 가지며 format verb 수가 Key.Args와 같은지 확인한다.

const seedMigrationsDir = "../../../../hololive-api/scripts/migrations"

var errUnterminatedTuple = errors.New("unterminated value tuple")

var messageStringsInsertPattern = regexp.MustCompile(`(?i)INSERT\s+INTO\s+(?:public\.)?message_strings\s*\(([^)]*)\)\s*VALUES`)

// message_strings를 바꾸는 다른 형태의 문장이 생기면 이 parser가 시드를 잘못 재구성하므로 테스트를 실패시킨다.
var messageStringsMutationPattern = regexp.MustCompile(`(?i)(UPDATE\s+(?:public\.)?message_strings\b|DELETE\s+FROM\s+(?:public\.)?message_strings\b|COPY\s+(?:public\.)?message_strings\b)`)

// INSERT 뒤 충돌 절은 없음(중복이면 ux_message_strings 위반으로 migration이 실패) 또는 ON CONFLICT DO NOTHING(먼저
// 들어간 값 유지)만 지원한다. DO UPDATE처럼 기존 값을 바꾸는 절은 parser가 재구성하지 못하므로 거절한다.
var messageStringsDoNothingPattern = regexp.MustCompile(`(?is)^ON\s+CONFLICT(?:\s*\([^)]*\))?\s+DO\s+NOTHING$`)

type seedConflictMode int

const (
	seedConflictNone seedConflictMode = iota
	seedConflictDoNothing
)

func seedMessageStrings(t *testing.T) map[string]map[string]string {
	t.Helper()

	migrations := os.DirFS(seedMigrationsDir)

	manifest, err := fs.ReadFile(migrations, "manifest.txt")
	if err != nil {
		t.Fatalf("read migration manifest: %v", err)
	}

	seed := make(map[string]map[string]string)

	for line := range strings.Lines(string(manifest)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		migration := fields[len(fields)-1]

		content, readErr := fs.ReadFile(migrations, migration)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", migration, readErr)
		}

		if err := applyMessageStringInserts(seed, string(content)); err != nil {
			t.Fatalf("parse message_strings seed in %s: %v", migration, err)
		}
	}

	return seed
}

func applyMessageStringInserts(seed map[string]map[string]string, sql string) error {
	if mutation := messageStringsMutationPattern.FindString(sql); mutation != "" {
		return fmt.Errorf("unsupported message_strings statement %q: extend the seed parser", mutation)
	}

	for _, match := range messageStringsInsertPattern.FindAllStringSubmatchIndex(sql, -1) {
		columns := splitColumns(sql[match[2]:match[3]])
		rest := sql[match[1]:]

		tuples, next, err := parseValueTuples(rest)
		if err != nil {
			return err
		}

		mode, err := parseSeedConflictClause(rest[next:])
		if err != nil {
			return err
		}

		for _, tuple := range tuples {
			if err := applySeedTuple(seed, columns, tuple, mode); err != nil {
				return err
			}
		}
	}

	return nil
}

// parseSeedConflictClause는 VALUES 목록 뒤부터 문장 끝(;)까지의 절을 읽는다.
func parseSeedConflictClause(rest string) (seedConflictMode, error) {
	statement, _, _ := strings.Cut(rest, ";")
	clause := strings.TrimSpace(statement)

	switch {
	case clause == "":
		return seedConflictNone, nil
	case messageStringsDoNothingPattern.MatchString(clause):
		return seedConflictDoNothing, nil
	default:
		return seedConflictNone, fmt.Errorf("unsupported message_strings INSERT clause %q: only no conflict clause or ON CONFLICT DO NOTHING is reconstructed; extend the seed parser", clause)
	}
}

func splitColumns(raw string) []string {
	parts := strings.Split(raw, ",")
	columns := make([]string, 0, len(parts))

	for _, part := range parts {
		columns = append(columns, strings.TrimSpace(part))
	}

	return columns
}

func applySeedTuple(seed map[string]map[string]string, columns, tuple []string, mode seedConflictMode) error {
	if len(columns) != len(tuple) {
		return fmt.Errorf("column count %d != value count %d", len(columns), len(tuple))
	}

	row := make(map[string]string, len(columns))
	for i, column := range columns {
		row[column] = tuple[i]
	}

	namespace, key, value := row["namespace"], row["key"], row["value"]
	if namespace == "" || key == "" {
		return fmt.Errorf("message_strings row without namespace/key: %v", row)
	}

	values, ok := seed[namespace]
	if !ok {
		values = make(map[string]string)
		seed[namespace] = values
	}

	if _, exists := values[key]; exists {
		// ON CONFLICT DO NOTHING은 먼저 들어간 값을 유지한다. 충돌 절 없는 중복은 unique 위반으로 migration이 실패한다.
		if mode == seedConflictDoNothing {
			return nil
		}

		return fmt.Errorf("duplicate message_strings %s/%s without ON CONFLICT DO NOTHING", namespace, key)
	}

	values[key] = value

	return nil
}

// parseValueTuples는 VALUES 뒤의 (…), (…) 목록을 읽고 목록 다음 위치를 돌려준다. 문자열 literal 안의 ”와 줄바꿈을
// 처리한다.
func parseValueTuples(rest string) ([][]string, int, error) {
	var tuples [][]string

	i := skipSpace(rest, 0)
	for i < len(rest) && rest[i] == '(' {
		tuple, next, err := parseTuple(rest, i+1)
		if err != nil {
			return nil, i, err
		}

		tuples = append(tuples, tuple)

		i = skipSpace(rest, next)
		if i < len(rest) && rest[i] == ',' {
			i = skipSpace(rest, i+1)

			continue
		}

		break
	}

	if len(tuples) == 0 {
		return nil, i, errors.New("no value tuples after VALUES")
	}

	return tuples, i, nil
}

func parseTuple(sql string, i int) ([]string, int, error) {
	var values []string

	for {
		i = skipSpace(sql, i)
		if i >= len(sql) {
			return nil, i, errUnterminatedTuple
		}

		value, next, err := parseScalar(sql, i)
		if err != nil {
			return nil, i, err
		}

		values = append(values, value)

		i = skipSpace(sql, next)
		if i >= len(sql) {
			return nil, i, errUnterminatedTuple
		}

		switch sql[i] {
		case ',':
			i++
		case ')':
			return values, i + 1, nil
		default:
			return nil, i, fmt.Errorf("unexpected %q in value tuple", sql[i])
		}
	}
}

func parseScalar(sql string, i int) (string, int, error) {
	if sql[i] != '\'' {
		start := i
		for i < len(sql) && sql[i] != ',' && sql[i] != ')' {
			i++
		}

		return strings.TrimSpace(sql[start:i]), i, nil
	}

	var value strings.Builder

	for i++; i < len(sql); i++ {
		if sql[i] != '\'' {
			value.WriteByte(sql[i])

			continue
		}

		if i+1 < len(sql) && sql[i+1] == '\'' {
			value.WriteByte('\'')

			i++

			continue
		}

		return value.String(), i + 1, nil
	}

	return "", i, errors.New("unterminated string literal")
}

func skipSpace(s string, i int) int {
	for i < len(s) && unicode.IsSpace(rune(s[i])) {
		i++
	}

	return i
}

// countFormatVerbs는 fmt verb 수를 센다. "%%"는 literal이라 세지 않는다.
func countFormatVerbs(value string) int {
	count := 0

	for i := 0; i < len(value); i++ {
		if value[i] != '%' {
			continue
		}

		if i+1 < len(value) && value[i+1] == '%' {
			i++

			continue
		}

		count++
	}

	return count
}

func TestSeedSQLCoversEveryTypedKeyWithMatchingFormatArgs(t *testing.T) {
	seed := seedMessageStrings(t)

	for _, key := range AllKeys() {
		value := seed[key.Namespace][key.Name]
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s has no seeded value", key)

			continue
		}

		if got := countFormatVerbs(value); got != key.Args {
			t.Errorf("%s seed %q has %d format verbs, Key.Args = %d", key, value, got, key.Args)
		}
	}
}

// 동적 key로 조회하는 namespace는 시드가 비어 있으면 기동 검증에서 실패한다.
func TestSeedSQLHasRowsForDynamicNamespaces(t *testing.T) {
	seed := seedMessageStrings(t)

	for _, namespace := range []string{NamespaceOrg, NamespaceNewsCat, NamespaceAlarmType} {
		if len(seed[namespace]) == 0 {
			t.Errorf("namespace %q has no seeded rows", namespace)
		}
	}
}

// parser가 baseline과 이후 migration을 모두 읽는지 고정한다(baseline 114행 + 188의 3행).
func TestSeedSQLParserReadsBaselineAndMigrations(t *testing.T) {
	seed := seedMessageStrings(t)

	total := 0

	for _, values := range seed {
		total += len(values)
	}

	if total < 117 {
		t.Fatalf("parsed %d message_strings rows, want at least 117 (baseline 114 + migration 188)", total)
	}

	// migration 188의 karing 행은 Karing 발송 삭제 뒤 읽는 코드가 없는 보관 데이터지만, parser가 migration을 읽는지 보는 표본으로 쓴다.
	if got := seed["karing"]["outbox_title_video_premiere"]; got != "%d분 후 공개 예정" {
		t.Fatalf("migration 188 row = %q, want %q", got, "%d분 후 공개 예정")
	}

	if got := seed[NamespaceError]["unknown_command"]; !strings.Contains(got, "\n") {
		t.Fatalf("multi-line baseline value = %q, want embedded newline preserved", got)
	}
}

// 시드 재구성은 migration의 충돌 절 의미를 따른다. DO UPDATE처럼 값을 바꾸는 절이나 충돌 절 없는 중복은 parser가
// 옛 값을 유지한 채 조용히 통과시키면 format 인자 수 검사가 실제 DB 값과 어긋나므로 실패로 드러낸다.
func TestApplyMessageStringInsertsFollowsConflictClause(t *testing.T) {
	const baseline = "INSERT INTO public.message_strings (namespace, key, value) VALUES ('karing', 'k', '%d분');\n"

	cases := []struct {
		name      string
		migration string
		wantValue string
		wantErr   string
	}{
		{
			name:      "do nothing keeps first value",
			migration: "INSERT INTO message_strings (namespace, key, value)\nVALUES\n    ('karing', 'k', '%d분 %d초')\nON CONFLICT (namespace, key) DO NOTHING;\n",
			wantValue: "%d분",
		},
		{
			name:      "do update is rejected",
			migration: "INSERT INTO message_strings (namespace, key, value) VALUES ('karing', 'k', '%d분 %d초')\nON CONFLICT (namespace, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now();\n",
			wantErr:   "unsupported message_strings INSERT clause",
		},
		{
			name:      "duplicate without conflict clause is rejected",
			migration: "INSERT INTO message_strings (namespace, key, value) VALUES ('karing', 'k', '%d분 %d초');\n",
			wantErr:   "duplicate message_strings karing/k",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed := make(map[string]map[string]string)
			if err := applyMessageStringInserts(seed, baseline); err != nil {
				t.Fatalf("baseline parse error = %v", err)
			}

			err := applyMessageStringInserts(seed, tc.migration)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("applyMessageStringInserts() error = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("applyMessageStringInserts() error = %v", err)
			}

			if got := seed["karing"]["k"]; got != tc.wantValue {
				t.Fatalf("seed value = %q, want %q", got, tc.wantValue)
			}
		})
	}
}

func TestCountFormatVerbs(t *testing.T) {
	cases := map[string]int{
		"plain":           0,
		"%d분 후":           1,
		"%s (%d시간 %d분 후)": 3,
		"100%% 완료":        0,
		"%5.2f %v":        2,
	}

	for value, want := range cases {
		if got := countFormatVerbs(value); got != want {
			t.Errorf("countFormatVerbs(%q) = %d, want %d", value, got, want)
		}
	}
}
