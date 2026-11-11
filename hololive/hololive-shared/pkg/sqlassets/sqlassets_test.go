package sqlassets

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
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

const (
	selectAssetName = "select.sql"
	selectQuery     = "SELECT 1;\n"
)

func selectAssets(query string) fstest.MapFS {
	return fstest.MapFS{
		"queries/" + selectAssetName: &fstest.MapFile{Data: []byte(query)},
	}
}

func TestMustReaderReusesSuccessfulRead(t *testing.T) {
	t.Parallel()

	assets := &countingFS{fsys: selectAssets(selectQuery)}
	read := MustReader(assets, "queries")

	for range 3 {
		if got := read(selectAssetName); got != selectQuery {
			t.Fatalf("read(%s) = %q, want %q", selectAssetName, got, selectQuery)
		}
	}

	if got := assets.opens.Load(); got != 1 {
		t.Fatalf("assets opened %d times, want 1", got)
	}
}

func TestMustReaderPanicsOnEveryFailedCall(t *testing.T) {
	t.Parallel()

	assets := &countingFS{fsys: fstest.MapFS{
		"queries/blank.sql": &fstest.MapFile{Data: []byte(" \n\t")},
		"secret.sql":        &fstest.MapFile{Data: []byte("secret")},
	}}
	read := MustReader(assets, "queries")

	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "missing.sql", want: `read embedded SQL "queries/missing.sql"`},
		{name: "blank.sql", want: `empty embedded SQL "queries/blank.sql"`},
		{name: "../secret.sql", want: `invalid embedded SQL path "queries/../secret.sql"`},
	} {
		for range 2 {
			assertPanicContains(t, func() { read(tc.name) }, tc.want)
		}
	}

	// 잘못된 경로는 열기 전에 거부하고, 없는 파일과 빈 자산은 호출마다 다시 연다.
	if got := assets.opens.Load(); got != 4 {
		t.Fatalf("assets opened %d times, want 4", got)
	}
}

func TestMustReaderDoesNotShareCacheAcrossReaders(t *testing.T) {
	t.Parallel()

	const otherQuery = "SELECT 2;\n"

	first := MustReader(selectAssets(selectQuery), "queries")
	second := MustReader(selectAssets(otherQuery), "queries")

	if got := first(selectAssetName); got != selectQuery {
		t.Fatalf("first(%s) = %q, want %q", selectAssetName, got, selectQuery)
	}

	if got := second(selectAssetName); got != otherQuery {
		t.Fatalf("second(%s) = %q, want %q", selectAssetName, got, otherQuery)
	}
}

func TestMustReaderConcurrentFirstCalls(t *testing.T) {
	t.Parallel()

	read := MustReader(selectAssets(selectQuery), "queries")

	const goroutines = 16

	results := make([]string, goroutines)

	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Go(func() {
			results[i] = read(selectAssetName)
		})
	}

	wg.Wait()

	for i, got := range results {
		if got != selectQuery {
			t.Fatalf("results[%d] = %q, want %q", i, got, selectQuery)
		}
	}
}

type countingFS struct {
	fsys  fs.FS
	opens atomic.Int64
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opens.Add(1)

	file, err := c.fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open counted asset: %s: %w", name, err)
	}

	return file, nil
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
