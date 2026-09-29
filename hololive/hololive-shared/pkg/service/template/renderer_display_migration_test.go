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
