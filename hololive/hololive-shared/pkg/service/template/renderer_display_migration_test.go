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

	pairs := make(map[domain.TemplateKey]displayLineSeedPair, len(rows))
	for _, row := range rows {
		pairs[domain.TemplateKey(row[1])] = displayLineSeedPair{row[2], row[3]}
	}

	return pairs
}

func TestDisplayLineMigrationPreservesAllSeedOutput(t *testing.T) {
	pool := dbtest.NewPool(t)
	pairs := loadDisplayLineSeeds(t)
	previousFuncs := legacyTemplateFunctions()

	for _, key := range sampledata.GetAllTemplateKeys() {
		t.Run(string(key), func(t *testing.T) {
			current := seedBody(t, pool, key)
			previous := current

			data := sampledata.GetTemplateSampleData(key)

			if pair, changed := pairs[key]; changed {
				previous = pair.oldBody
				current = pair.newBody

				// 208 본문의 알람 추가·목록은 233 이전 계약대로 NextStream 키를 요구한다.
				if sample, ok := data.(map[string]any); ok {
					data = withNilNextStream(sample)
				}
			}

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
