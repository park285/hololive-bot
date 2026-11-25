package sqlassets

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/park285/shared-go/postgres/pgxdb"
)

func MustReader(assets fs.FS, directory string) func(string) string {
	directory = strings.TrimSuffix(directory, "/")
	if !fs.ValidPath(directory) || directory == "." {
		panic(fmt.Errorf("invalid embedded SQL directory %q", directory))
	}

	var cache sync.Map

	return func(name string) string {
		if cached, ok := cache.Load(name); ok {
			return cachedSQL(cached)
		}

		queryPath := directory + "/" + name
		if !fs.ValidPath(queryPath) {
			panic(fmt.Errorf("invalid embedded SQL path %q", queryPath))
		}

		query, err := fs.ReadFile(assets, queryPath)
		if err != nil {
			panic(fmt.Errorf("read embedded SQL %q: %w", queryPath, err))
		}

		if strings.TrimSpace(string(query)) == "" {
			panic(fmt.Errorf("empty embedded SQL %q", queryPath))
		}

		actual, _ := cache.LoadOrStore(name, string(query))

		return cachedSQL(actual)
	}
}

func MustStatementReader(assets embed.FS, directory string) func(string) pgxdb.Statement {
	directory = strings.TrimSuffix(directory, "/")
	if !fs.ValidPath(directory) || directory == "." {
		panic(fmt.Errorf("invalid embedded SQL directory %q", directory))
	}

	return func(name string) pgxdb.Statement {
		return pgxdb.MustStatement(assets, directory+"/"+name)
	}
}

func Fragment(name string) string {
	return "/* fragment:" + name + " */"
}

func cachedSQL(value any) string {
	query, ok := value.(string)
	if !ok {
		panic(fmt.Errorf("embedded SQL cache stored %T, want string", value))
	}

	return query
}
