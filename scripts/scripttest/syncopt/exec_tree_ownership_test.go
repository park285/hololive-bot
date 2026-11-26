package syncopt

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/kapu/hololive-scripttest/internal/harness"
)

const fakeStat = `format=""
path=""
while (($# > 0)); do
  case "$1" in
    -c) format="$2"; shift 2 ;;
    *) path="$1"; shift ;;
  esac
done
case "$format" in
  %u) case "$path" in */unsafe-parent) echo 1000 ;; *) echo 0 ;; esac ;;
  %g) echo 0 ;;
  %a) case "$path" in */root-group-writable) echo 0775 ;; *) echo 0755 ;; esac ;;
  *) echo "unsupported stat format: $format" >&2; exit 2 ;;
esac
`

func verifyExec(t *testing.T, bin, path string) harness.Result {
	t.Helper()

	return harness.Bash(t, harness.Cmd{
		Script: `exec "$SCRIPTTEST_ROOT/scripts/deploy/verify-exec-tree-ownership.sh" "$1"`,
		Args:   []string{path},
		Bin:    bin,
	})
}

func execFile(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "compose.sh")
	harness.Write(t, path, "#!/bin/sh\n")

	return path
}

func rootOwnedReference(t *testing.T) string {
	t.Helper()

	for _, candidate := range []string{"/usr/bin/env", "/bin/true", "/usr/bin/true", "/bin/sh"} {
		path, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if ok && stat.Uid == 0 && info.Mode().Perm()&0o022 == 0 {
			return path
		}
	}

	t.Skip("no root-owned non-writable reference file")

	return ""
}

func TestExecTreeRejectsUnsafeFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	harness.Fails(t, "non-root-owned file", verifyExec(t, "", execFile(t, dir)))

	writable := filepath.Join(dir, "world-writable")
	harness.Write(t, writable, "x")

	if err := os.Chmod(writable, 0o666); err != nil { // #nosec G302 -- 거부되어야 하는 쓰기 가능 파일을 만든다.
		t.Fatal(err)
	}

	harness.Fails(t, "writable file", verifyExec(t, "", writable))

	link := filepath.Join(dir, "root-safe-link")
	harness.Symlink(t, "/bin/sh", link)
	harness.Fails(t, "symlinked exec path", verifyExec(t, "", link))
}

func TestExecTreeAcceptsARootOwnedFile(t *testing.T) {
	t.Parallel()

	harness.Succeeds(t, "root-owned file", verifyExec(t, "", rootOwnedReference(t)))
}

func TestExecTreeChecksEveryParentDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	harness.Executable(t, filepath.Join(bin, "stat"), fakeStat)

	harness.Fails(t, "non-root parent", verifyExec(t, bin, execFile(t, filepath.Join(dir, "unsafe-parent"))))
	harness.Succeeds(t, "root-owned chain", verifyExec(t, bin, execFile(t, filepath.Join(dir, "root-owned", "current"))))
	harness.Fails(t, "group-writable parent", verifyExec(t, bin, execFile(t, filepath.Join(dir, "root-group-writable"))))
}

func TestExecTreeRejectsAParentWithAUserWriteACL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	harness.Executable(t, filepath.Join(bin, "stat"), fakeStat)

	parent := filepath.Join(dir, "root-owned-acl")
	file := execFile(t, parent)
	entries := []struct {
		tag, perm uint16
		id        uint32
	}{{0x01, 7, 0xFFFFFFFF}, {0x02, 7, 65534}, {0x04, 5, 0xFFFFFFFF}, {0x10, 7, 0xFFFFFFFF}, {0x20, 5, 0xFFFFFFFF}}
	acl := binary.LittleEndian.AppendUint32(make([]byte, 0, 4+8*len(entries)), 2)

	for _, entry := range entries {
		acl = binary.LittleEndian.AppendUint16(acl, entry.tag)
		acl = binary.LittleEndian.AppendUint16(acl, entry.perm)
		acl = binary.LittleEndian.AppendUint32(acl, entry.id)
	}

	if err := syscall.Setxattr(parent, "system.posix_acl_access", acl, 0); err != nil {
		t.Skipf("filesystem does not support POSIX ACLs: %v", err)
	}

	harness.Fails(t, "user write ACL", verifyExec(t, bin, file))
}
