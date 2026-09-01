package batchrepo

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPollerBatchRepositoryDoesNotMutateDeliveryLifecycle(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	packageDir := filepath.Dir(file)

	violations, err := findPollerLifecycleMutations(packageDir)
	if err != nil {
		t.Fatalf("scan poller lifecycle mutations: %v", err)
	}

	if len(violations) == 0 {
		return
	}

	slices.Sort(violations)
	t.Fatalf("poller batch repository must not mutate delivery lifecycle:\n%s", strings.Join(violations, "\n"))
}

func findPollerLifecycleMutations(packageDir string) ([]string, error) {
	mutationPattern := regexp.MustCompile(`(?is)\b(?:UPDATE|DELETE\s+FROM)\s+(?:public\.)?youtube_notification_(?:outbox|delivery)\b`)
	conflictRewritePattern := regexp.MustCompile(`(?is)\bON\s+CONFLICT\b[\s\S]*\bDO\s+UPDATE\b`)
	violations := make([]string, 0)

	err := filepath.WalkDir(packageDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() {
			return nil
		}

		if filepath.Ext(path) != ".sql" {
			return nil
		}

		// The path is yielded by WalkDir from the fixed test package root.
		//nolint:gosec // This test neither accepts external paths nor follows a mutable production tree.
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}

		relPath, err := filepath.Rel(packageDir, path)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}

		if mutationPattern.Match(body) {
			violations = append(violations, relPath+": direct outbox/delivery mutation")
		}

		if filepath.Base(path) == "repository_batch_writes_0244_06.sql" && conflictRewritePattern.Match(body) {
			violations = append(violations, relPath+": existing outbox conflict rewrite")
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk poller batch package: %w", err)
	}

	return violations, nil
}
