package dbtest

import (
	"io/fs"
	"os"
	"regexp"
	"testing"
)

type templateMigrationBody struct{ oldBody, newBody string }

// 각 migration 자체의 전후 본문을 사용해 이후 migration의 변경과 검증을 분리합니다.
func loadTemplateMigrationBodies(t *testing.T, file string) map[string]templateMigrationBody {
	t.Helper()

	dir, err := resolveMigrationsDir()
	if err != nil {
		t.Fatal(err)
	}

	raw, err := fs.ReadFile(os.DirFS(dir), file)
	if err != nil {
		t.Fatal(err)
	}

	rows := regexp.MustCompile(`(?s)\('([^']+)', \$old\$(.*?)\$old\$, \$new\$(.*?)\$new\$\)`).FindAllStringSubmatch(string(raw), -1)
	if len(rows) == 0 {
		t.Fatal("template migration contains no body pairs")
	}

	bodies := make(map[string]templateMigrationBody, len(rows))
	for _, row := range rows {
		bodies[row[1]] = templateMigrationBody{row[2], row[3]}
	}

	return bodies
}
