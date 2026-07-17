package sqlassets

import (
	"fmt"
	"io/fs"
	"strings"
)

// MustReader returns a package-local embedded SQL loader rooted at directory.
// Missing or invalid assets panic, matching the existing mustSQL contract while
// preserving the failing asset path in the diagnostic.
func MustReader(assets fs.FS, directory string) func(string) string {
	directory = strings.TrimSuffix(directory, "/")
	if !fs.ValidPath(directory) || directory == "." {
		panic(fmt.Errorf("invalid embedded SQL directory %q", directory))
	}

	return func(name string) string {
		queryPath := directory + "/" + name
		if !fs.ValidPath(queryPath) {
			panic(fmt.Errorf("invalid embedded SQL path %q", queryPath))
		}

		query, err := fs.ReadFile(assets, queryPath)
		if err != nil {
			panic(fmt.Errorf("read embedded SQL %q: %w", queryPath, err))
		}
		return string(query)
	}
}
