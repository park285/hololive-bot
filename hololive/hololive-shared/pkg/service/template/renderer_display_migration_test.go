package template

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	texttemplate "text/template"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/internal/service/template/sampledata"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type displayLineSeedPair struct{ oldBody, newBody string }

func loadDisplayLineSeeds(tb testing.TB) map[domain.TemplateKey]displayLineSeedPair {
	tb.Helper()

	dir := filepath.Join("..", "..", "..", "..", "hololive-api", "scripts", "migrations")

	raw, err := fs.ReadFile(os.DirFS(dir), "208_template_displayline.sql")
	if err != nil {
		tb.Fatal(err)
	}

	rows := regexp.MustCompile(`(?s)\('([^']+)', \$old\$(.*?)\$old\$, \$new\$(.*?)\$new\$\)`).FindAllStringSubmatch(string(raw), -1)
	if len(rows) != 36 {
		tb.Fatalf("expected 36 updated template keys, got %d", len(rows))
	}

	pairs := make(map[domain.TemplateKey]displayLineSeedPair, len(rows))
	for _, row := range rows {
		pairs[domain.TemplateKey(row[1])] = displayLineSeedPair{row[2], row[3]}
	}

	return pairs
}

func TestDisplayLineMigrationPreservesAllSeedOutput(t *testing.T) {
	pool := dbtest.NewPool(t)
	pairs := loadDisplayLineSeeds(t)
	compacted := loadCompactSeparatorSeeds(t)
	previousFuncs := legacyTemplateFunctions()

	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			current := seedBody(t, pool, key)
			previous := current

			if pair, changed := pairs[key]; changed {
				want := pair.newBody
				// 217이 이어서 바꾼 키는 217 결과를 확인하고, 208 자체의 표시 보존은 208 본문으로 비교한다.
				if next, rewritten := compacted[key]; rewritten {
					want = next.newBody
				}

				if current != want {
					t.Fatal("standard default was not migrated")
				}

				previous = pair.oldBody
				current = pair.newBody
			} else if _, rewritten := compacted[key]; rewritten {
				t.Fatal("217 rewrote a key that 208 did not standardize")
			}

			data := sampledata.GetTemplateSampleData(key)
			before := renderOptimizationTemplate(t, previous, previousFuncs, data)
			after := renderOptimizationTemplate(t, current, templateFuncs, data)

			if after != before {
				t.Errorf("display changed: got=%q want=%q", after, before)
			}
		})
	}
}

func legacyTemplateFunctions() texttemplate.FuncMap {
	funcs := make(texttemplate.FuncMap, len(templateFuncs))
	maps.Copy(funcs, templateFuncs)

	funcs["truncate"] = legacyTemplateTruncate

	return funcs
}

func renderOptimizationTemplate(tb testing.TB, body string, funcs texttemplate.FuncMap, data any) string {
	tb.Helper()

	tmpl, err := texttemplate.New("optimization").Funcs(funcs).Option("missingkey=error").Parse(body)
	if err != nil {
		tb.Fatal(err)
	}

	var buf bytes.Buffer

	if err := tmpl.Execute(&buf, data); err != nil {
		tb.Fatal(err)
	}

	return buf.String()
}
