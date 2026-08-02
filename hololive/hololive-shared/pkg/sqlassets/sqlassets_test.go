package sqlassets

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMustReaderReadsAsset(t *testing.T) {
	t.Parallel()

	read := MustReader(
		fstest.MapFS{
			"queries/select.sql": &fstest.MapFile{Data: []byte("SELECT 1;\n")},
		},
		"queries",
	)

	if got := read("select.sql"); got != "SELECT 1;\n" {
		t.Fatalf("read(select.sql) = %q, want %q", got, "SELECT 1;\n")
	}
}

func TestMustReaderPreservesTrailingWhitespace(t *testing.T) {
	t.Parallel()

	read := MustReader(
		fstest.MapFS{
			"queries/prefix.sql": &fstest.MapFile{Data: []byte("\n\t\tSELECT id FROM t WHERE ")},
		},
		"queries",
	)

	const want = "\n\t\tSELECT id FROM t WHERE "
	if got := read("prefix.sql"); got != want {
		t.Fatalf("read(prefix.sql) = %q, want %q", got, want)
	}
}

func TestMustReaderPanicsWithBlankAsset(t *testing.T) {
	t.Parallel()

	read := MustReader(
		fstest.MapFS{
			"queries/blank.sql": &fstest.MapFile{Data: []byte(" \n\t")},
		},
		"queries",
	)
	assertPanicContains(t, func() { read("blank.sql") }, `empty embedded SQL "queries/blank.sql"`)
}

func TestMustReaderPanicsWithMissingAssetPath(t *testing.T) {
	t.Parallel()

	read := MustReader(fstest.MapFS{}, "queries")
	assertPanicContains(t, func() { read("missing.sql") }, `read embedded SQL "queries/missing.sql"`)
}

func TestMustReaderRejectsTraversal(t *testing.T) {
	t.Parallel()

	read := MustReader(
		fstest.MapFS{
			"secret.sql": &fstest.MapFile{Data: []byte("secret")},
		},
		"queries",
	)
	assertPanicContains(t, func() { read("../secret.sql") }, `invalid embedded SQL path "queries/../secret.sql"`)
}

func assertPanicContains(t *testing.T, fn func(), want string) {
	t.Helper()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic containing %q", want)
		}
		if got := fmt.Sprint(recovered); !strings.Contains(got, want) {
			t.Fatalf("panic = %q, want substring %q", got, want)
		}
	}()

	fn()
}
