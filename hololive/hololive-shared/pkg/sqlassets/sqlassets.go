package sqlassets

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
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

func WithFragment(query, name, fragment string) string {
	directive := "/* fragment:" + name + " */"
	if count := strings.Count(query, directive); count != 1 {
		panic(fmt.Errorf("embedded SQL fragment %q appears %d times, want exactly 1", directive, count))
	}

	return strings.Replace(query, directive, fragment, 1)
}

func cachedSQL(value any) string {
	query, ok := value.(string)
	if !ok {
		panic(fmt.Errorf("embedded SQL cache stored %T, want string", value))
	}

	return query
}
