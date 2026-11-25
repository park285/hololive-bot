package sqlassets

import (
	"embed"
	"testing"
)

//go:embed testdata/queries/select.sql
var testAssets embed.FS

func TestMustStatementReaderReadsTrimmedAsset(t *testing.T) {
	t.Parallel()

	read := MustStatementReader(testAssets, "testdata/queries/")

	if got := read("select.sql").SQL(); got != "SELECT 1;" {
		t.Fatalf("read(select.sql).SQL() = %q, want %q", got, "SELECT 1;")
	}
}

func TestMustStatementReaderRejectsInvalidDirectory(t *testing.T) {
	t.Parallel()

	for _, directory := range []string{".", "", "../queries", "/queries"} {
		t.Run(directory, func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatalf("MustStatementReader(%q) did not panic", directory)
				}
			}()

			MustStatementReader(testAssets, directory)
		})
	}
}

func TestMustStatementReaderPanicsOnMissingAsset(t *testing.T) {
	t.Parallel()

	read := MustStatementReader(testAssets, "testdata/queries")

	defer func() {
		if recover() == nil {
			t.Fatal("read(missing.sql) did not panic")
		}
	}()

	read("missing.sql")
}
