package pocentral

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

func digest(t *testing.T, path string) string {
	t.Helper()

	sum := sha256.Sum256([]byte(harness.Read(t, path)))

	return hex.EncodeToString(sum[:])
}

func sums(t *testing.T, dir string, names ...string) string {
	t.Helper()

	var b strings.Builder

	for _, name := range names {
		b.WriteString(digest(t, filepath.Join(dir, name)) + "  " + name + "\n")
	}

	return b.String()
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()

	harness.Write(t, to, harness.Read(t, from))
}

func nonEmptyLines(text string) []string {
	out := make([]string, 0, strings.Count(text, "\n")+1)

	for line := range strings.Lines(text) {
		if line = strings.TrimRight(line, "\n"); strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}

	return out
}

func centralPayload(t *testing.T) []string {
	t.Helper()

	payload := strings.Split(strings.TrimRight(harness.Read(t, harness.Path(t, "scripts/deploy/po-central-files.txt")), "\n"), "\n")
	migrations := nonEmptyLines(harness.Read(t, harness.Path(t, "hololive/hololive-api/scripts/migrations/manifest.txt")))

	for _, line := range migrations[1:] {
		name := strings.Fields(line)[1]

		payload = append(payload, "hololive/hololive-api/scripts/migrations/"+name, "hololive/hololive-api/scripts/migrations/contracts/"+name)
	}

	return payload
}

func filesUnder(t *testing.T, dir string) []string {
	t.Helper()

	var files []string

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("relative path: %w", err)
		}

		files = append(files, rel)

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}

	slices.Sort(files)

	return files
}

func TestVerifiedPairTransferStagesExactlyThePayloadAndSealedPair(t *testing.T) {
	t.Parallel()

	state := t.TempDir()
	tool, pair, remote, bin := filepath.Join(state, "tool"), filepath.Join(state, "pair"), filepath.Join(state, "remote"), filepath.Join(state, "bin")
	payload := centralPayload(t)

	for _, name := range append(slices.Clone(payload), "scripts/deploy/po-central-cutover.sh", "scripts/deploy/lib/source-revision.sh",
		"scripts/deploy/lib/po-central-remote.sh", "scripts/deploy/po-central-files.txt", "hololive/hololive-api/VERSION") {
		copyFile(t, harness.Path(t, name), filepath.Join(tool, name))
	}

	for _, dir := range []string{pair, remote} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	for _, p := range [][2]string{{"", rootfsManifest}, {"collector-", collectorManifest}} {
		harness.Write(t, filepath.Join(pair, p[0]+"image.tar"), p[0]+"image")
		harness.Write(t, filepath.Join(pair, p[0]+"image.tar.sha256"), sums(t, pair, p[0]+"image.tar"))
		harness.Write(t, filepath.Join(pair, p[0]+"image-id"), newID+"\n")
		harness.Write(t, filepath.Join(pair, p[1]), `{"source_revision": "`+newRevision+`", "architecture": "arm64", "go": {"goarch": "arm64"}}`)
	}

	harness.Write(t, filepath.Join(pair, "source-files.sha256"), sums(t, tool, payload...))
	harness.Write(t, filepath.Join(pair, "transport-files.sha256"), sums(t, pair, "source-files.sha256", rootfsManifest, collectorManifest,
		"image-id", "collector-image-id", "image.tar.sha256", "collector-image.tar.sha256"))
	harness.Executable(t, filepath.Join(bin, "ssh"), "shift\ncommand=\"$*\"\nexec bash -c \"${command//\"$REMOTE_ROOT\"/\"$FAKE_REMOTE\"}\"\n")

	r := run(t, transferEntry, state, "transfer", digest(t, filepath.Join(pair, "transport-files.sha256")))
	staged, err := filepath.Glob(filepath.Join(remote, "po-staging-*"))
	require(t, err == nil && r.ExitCode == 0 && len(staged) == 1 && exists(filepath.Join(state, "remote-body")),
		"verified pair transfer reaches the remote quiesce with the installed rsync", r)

	pairFiles := []string{
		"image.tar", "image.tar.sha256", "image-id", rootfsManifest, "collector-image.tar", "collector-image.tar.sha256",
		"collector-image-id", collectorManifest, "source-files.sha256", "transport-files.sha256",
	}
	want := slices.Sorted(slices.Values(append(slices.Clone(payload), pairFiles...)))
	require(t, slices.Equal(filesUnder(t, staged[0]), want), "staging holds exactly the payload and the sealed pair", r)

	for _, name := range payload {
		require(t, harness.Read(t, filepath.Join(staged[0], name)) == harness.Read(t, filepath.Join(tool, name)), "payload copied byte for byte: "+name, r)
	}

	for _, name := range pairFiles {
		require(t, harness.Read(t, filepath.Join(staged[0], name)) == harness.Read(t, filepath.Join(pair, name)), "pair copied byte for byte: "+name, r)
	}
}
