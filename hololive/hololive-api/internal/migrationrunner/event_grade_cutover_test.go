package migrationrunner

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/scripts/migrations"
	dbtest "github.com/kapu/hololive-dbtest"
)

func migrationsThrough(t *testing.T, head string) fstest.MapFS {
	t.Helper()

	through := fstest.MapFS{}

	require.NoError(t, fs.WalkDir(migrations.FS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}

		body, readErr := fs.ReadFile(migrations.FS, path)
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", path, readErr)
		}

		if path == "manifest.txt" {
			lines := strings.SplitAfter(string(body), "\n")
			kept := lines[:0]

			for _, line := range lines {
				if version, _, _ := strings.Cut(line, " "); version <= head {
					kept = append(kept, line)
				}
			}

			body = []byte(strings.Join(kept, ""))
		}

		through[path] = &fstest.MapFile{Data: body}

		return nil
	}))

	return through
}

func TestShadowStateRetirementRequiresConsumedEventGradeObservations(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	_, err := Run(t.Context(), pool, migrationsThrough(t, "004"), Config{})
	require.NoError(t, err)

	_, err = pool.Exec(t.Context(), "UPDATE public.event_grade_regions SET published_seq = published_seq + 1 WHERE region = 'jp'")
	require.NoError(t, err)

	_, err = Run(t.Context(), pool, migrations.FS, Config{})
	require.ErrorContains(t, err, "not fully consumed")

	var retained bool

	require.NoError(t, pool.QueryRow(t.Context(), "SELECT to_regclass('public.event_grade_collection_targets') IS NOT NULL").Scan(&retained))
	require.True(t, retained)

	_, err = pool.Exec(t.Context(), `UPDATE public.event_grade_consumer_offsets o SET applied_seq = r.published_seq
		FROM public.event_grade_regions r WHERE r.region = o.region`)
	require.NoError(t, err)

	result, err := Run(t.Context(), pool, migrations.FS, Config{})
	require.NoError(t, err)
	require.Equal(t, 10, result.Applied)
}
